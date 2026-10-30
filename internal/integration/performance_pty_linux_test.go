//go:build linux

package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/ui"
)

type ptyTiming struct {
	mu                                         sync.Mutex
	active                                     bool
	expected                                   string
	admitted, updateStart, updateEnd, prepared time.Time
	done                                       chan object
}
type ptyTimedModel struct {
	model  tea.Model
	views  *captureViews
	timing *ptyTiming
}

func (m *ptyTimedModel) Init() tea.Cmd { return m.model.Init() }
func (m *ptyTimedModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	matched := false
	if key, ok := msg.(tea.KeyPressMsg); ok {
		m.timing.mu.Lock()
		if m.timing.active && key.String() == m.timing.expected {
			m.timing.updateStart = time.Now()
			matched = true
		}
		m.timing.mu.Unlock()
	}
	var cmd tea.Cmd
	m.model, cmd = m.model.Update(msg)
	if matched {
		m.timing.mu.Lock()
		m.timing.updateEnd = time.Now()
		m.timing.mu.Unlock()
	}
	return m, cmd
}
func (m *ptyTimedModel) View() tea.View {
	v := m.model.View()
	ready := time.Now()
	m.views.record(v.Content)
	m.timing.mu.Lock()
	if m.timing.active && !m.timing.updateEnd.IsZero() && m.timing.prepared.IsZero() {
		m.timing.prepared = ready
	}
	m.timing.mu.Unlock()
	return v
}

type ptyTimedWriter struct {
	file   *os.File
	timing *ptyTiming
}

func (w *ptyTimedWriter) Fd() uintptr { return w.file.Fd() }
func (w *ptyTimedWriter) Write(p []byte) (int, error) {
	started := time.Now()
	n, err := w.file.Write(p)
	completed := time.Now()
	w.timing.mu.Lock()
	if err == nil && w.timing.active && !w.timing.prepared.IsZero() && !started.Before(w.timing.prepared) {
		now := completed
		ms := func(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
		r := object{"admissionToUpdateMs": ms(w.timing.updateStart.Sub(w.timing.admitted)), "updateMs": ms(w.timing.updateEnd.Sub(w.timing.updateStart)), "updateEndToPreparedMs": ms(w.timing.prepared.Sub(w.timing.updateEnd)), "preparedToFirstKernelWriteMs": ms(now.Sub(w.timing.prepared)), "admissionToFirstKernelWriteMs": ms(now.Sub(w.timing.admitted)), "writeBytes": n, "preparedToKernelWriteStartMs": ms(started.Sub(w.timing.prepared)), "kernelWriteMs": ms(completed.Sub(started))}
		w.timing.active = false
		w.timing.done <- r
	}
	w.timing.mu.Unlock()
	return n, err
}

// A real Linux PTY exercises Tea's terminal input/parser and kernel writes.
// Its master is drained by this test, not a display server. The measured write
// is the first kernel write following the recorded prepared View, potentially
// including renderer housekeeping. It is never called paint/physical latency.
func openPerformancePTY(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = master.Close() })
	ioctl := func(request uintptr, value unsafe.Pointer) error {
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), request, uintptr(value))
		if errno != 0 {
			return errno
		}
		return nil
	}
	var unlock, number uint32
	if err = ioctl(syscall.TIOCSPTLCK, unsafe.Pointer(&unlock)); err != nil {
		t.Fatal(err)
	}
	if err = ioctl(syscall.TIOCGPTN, unsafe.Pointer(&number)); err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = slave.Close() })
	size := [4]uint16{35, 120, 0, 0}
	if err = ioctl(syscall.TIOCSWINSZ, unsafe.Pointer(&size[0])); err != nil {
		t.Fatal(err)
	}
	return master, slave
}

func TestPerformanceActualPTYAdmissionToWrite(t *testing.T) {
	master, slave := openPerformancePTY(t)
	f := newFixture(t)
	hosts := 1
	if raw := os.Getenv("T3OXIDE_PTY_HOSTS"); raw != "" {
		var err error
		hosts, err = strconv.Atoi(raw)
		if err != nil || hosts != 1 && hosts != 2 && hosts != 4 {
			t.Fatal("PTY hosts must be 1, 2 or 4")
		}
	}
	fixtures := []*fixture{f}
	profiles := []config.Environment{f.profile}
	for i := 1; i < hosts; i++ {
		peer := newEnvironmentFixture(t, fmt.Sprintf("synthetic-env-%d", i), fmt.Sprintf("source-%d", i), "Synthetic peer")
		fixtures = append(fixtures, peer)
		profiles = append(profiles, peer.profile)
	}
	delayed := -1
	release := func() {}
	if hosts > 1 {
		delayed = hosts - 1
		if hosts == 4 {
			delayed = 2
			fixtures[3].server.Close()
		}
		release = fixtures[delayed].holdNativeShell()
	}
	defer release()
	state, err := config.OpenState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	manager := client.NewManager(profiles, state)
	defer manager.Close()
	a, err := manager.NewSession(f.profile.ID, "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	settings, err := ui.LoadSettings(filepath.Join(t.TempDir(), "terminal.json"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	model := ui.NewModel(ctx, a, settings)
	defer model.Close()
	timing := &ptyTiming{done: make(chan object, 1)}
	views := &captureViews{}
	program := tea.NewProgram(&ptyTimedModel{model: model, views: views, timing: timing}, tea.WithContext(ctx), tea.WithInput(slave), tea.WithOutput(&ptyTimedWriter{slave, timing}), tea.WithWindowSize(120, 35), tea.WithoutSignalHandler(), tea.WithEnvironment([]string{"TERM=xterm-256color"}))
	finished := make(chan error, 1)
	go func() { _, err := program.Run(); finished <- err }()
	go func() { _, _ = io.Copy(io.Discard, master) }()
	defer func() {
		// Normal Quit waits for the Linux terminal reader before closing its
		// cancellation descriptors. Kill intentionally skips that join.
		program.Quit()
		select {
		case <-finished:
		case <-time.After(4 * time.Second):
			t.Error("PTY program did not stop")
		}
		cancel()
	}()
	deadline := time.Now().Add(8 * time.Second)
	for !strings.Contains(views.text(), "Initial Markdown for Alpha") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(views.text(), "Initial Markdown for Alpha") {
		t.Fatal("actual PTY program did not show fixture")
	}
	if delayed >= 0 {
		deadline := time.Now().Add(5 * time.Second)
		for fixtures[delayed].requestCounts()["/api/orchestration/shell"] == 0 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if fixtures[delayed].requestCounts()["/api/orchestration/shell"] == 0 {
			t.Fatal("delayed metadata read was not admitted")
		}
	}
	streamMode := os.Getenv("T3OXIDE_PTY_STREAM")
	if streamMode != "" && streamMode != "mixed-search" {
		t.Fatal("unknown PTY stream workload")
	}
	var streamEvents atomic.Int64
	streamCancel := func() {}
	if streamMode == "mixed-search" {
		fragment := "## Synthetic live body\n\nA **mixed** paragraph with a needle.\n\n| key | value |\n| --- | --- |\n| needle | 42 |\n\n```go\nfunc fixture() { return }\n```\n\n"
		base := strings.Repeat(fragment, 1+8192/len(fragment))[:8192]
		f.update("thread-a", base, 0)
		deadline := time.Now().Add(8 * time.Second)
		for !strings.Contains(views.text(), "needle") && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if !strings.Contains(views.text(), "needle") {
			t.Fatal("mixed live body did not render")
		}
		if _, err = master.Write([]byte("/search needle\x1d")); err != nil {
			t.Fatal(err)
		}
		deadline = time.Now().Add(8 * time.Second)
		for !strings.Contains(views.text(), "Find ") && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if !strings.Contains(views.text(), "Find ") {
			t.Fatal("actual transcript search was not activated")
		}
		streamCtx, stopStream := context.WithCancel(ctx)
		streamDone := make(chan struct{})
		go func() {
			defer close(streamDone)
			tick := time.NewTicker(30 * time.Millisecond)
			defer tick.Stop()
			for {
				select {
				case <-streamCtx.Done():
					return
				case <-tick.C:
					n := streamEvents.Add(1)
					f.update("thread-a", base+fmt.Sprintf("\nstream-tail %d", n), 0)
				}
			}
		}()
		var once sync.Once
		streamCancel = func() { once.Do(func() { stopStream(); <-streamDone }) }
		defer streamCancel()
	}
	samples := 40
	if raw := os.Getenv("T3OXIDE_PTY_SAMPLES"); raw != "" {
		samples, err = strconv.Atoi(raw)
		if err != nil || samples < 2 || samples > 2000 {
			t.Fatal("invalid PTY sample count")
		}
	}
	var memoryBefore runtime.MemStats
	runtime.ReadMemStats(&memoryBefore)
	values := make([]object, 0, samples)
	for i := range samples {
		key, bytes := "x", "x"
		if i%2 == 1 {
			key, bytes = "y", "y"
		}
		timing.mu.Lock()
		timing.active = true
		timing.expected = key
		timing.admitted = time.Now()
		timing.updateStart = time.Time{}
		timing.updateEnd = time.Time{}
		timing.prepared = time.Time{}
		timing.mu.Unlock()
		if _, err = master.Write([]byte(bytes)); err != nil {
			t.Fatal(err)
		}
		select {
		case value := <-timing.done:
			values = append(values, value)
		case <-time.After(3 * time.Second):
			t.Fatalf("PTY key %d did not reach output", i)
		}
	}
	var memoryAfter runtime.MemStats
	runtime.ReadMemStats(&memoryAfter)
	streamCancel()
	expectedDraft := strings.Repeat("xy", samples/2)
	if samples%2 == 1 {
		expectedDraft += "x"
	}
	if a.Draft() != expectedDraft {
		t.Fatal("timed keys did not preserve exact composer draft")
	}
	if streamMode != "" && streamEvents.Load() == 0 {
		t.Fatal("continuous stream workload did not emit")
	}
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("typing measurement mutated native state")
	}
	held := false
	if delayed >= 0 {
		fixtures[delayed].mu.Lock()
		held = fixtures[delayed].activeShellHolds > 0
		fixtures[delayed].mu.Unlock()
		if !held {
			t.Fatal("delayed metadata read ended before the timed input samples; observation cannot claim held-load coverage")
		}
	}
	counts := []object{}
	for i, peer := range fixtures {
		if effects, dispatches := peer.counts(); effects != 0 || dispatches != 0 {
			t.Fatal("peer metadata measurement mutated")
		}
		c := peer.requestCounts()
		counts = append(counts, object{"sourceOrdinal": i + 1, "descriptor": c["/.well-known/t3/environment"], "shell": c["/api/orchestration/shell"], "sockets": c["/ws"]})
	}
	record, _ := json.Marshal(object{"schema": 3, "boundary": "real Linux PTY master key write admission to Tea KeyPress Update/complete View and first slave kernel Write after View; master drained in test; not terminal emulator/pixels/physical input", "load": "actual progressive metadata host load; stream/search only when streamMode is mixed-search", "streamMode": streamMode, "syntheticStreamEvents": streamEvents.Load(), "streamBodyBytes": map[bool]int{false: 0, true: 8192}[streamMode != ""], "streamCadenceMs": 30, "keys": "alternating x/y appended to exact composer draft (bounded at sample count)", "hosts": hosts, "offlineSources": map[bool]int{false: 0, true: 1}[hosts == 4], "delayedReadHeldThroughMeasurements": held, "hostCounts": counts, "samples": samples, "warmup": "initial fixture connection and first frame; no timed warmup excluded", "cells": []int{120, 35}, "effects": 0, "dispatches": 0, "rawStages": values, "goProcessMemory": object{"boundary": "whole Go test process including UI/fixture, excludes Node helpers; sampled before/after workload, not allocation attribution or peak RSS", "totalAllocatedBytes": memoryAfter.TotalAlloc - memoryBefore.TotalAlloc, "gcCycles": memoryAfter.NumGC - memoryBefore.NumGC, "heapAllocBefore": memoryBefore.HeapAlloc, "heapAllocAfter": memoryAfter.HeapAlloc}})
	t.Log(string(record))
}
