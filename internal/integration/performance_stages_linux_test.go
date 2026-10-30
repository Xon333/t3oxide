//go:build linux

package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/ui"
	"github.com/charmbracelet/x/ansi"
)

type pipelineStages struct {
	mu     sync.Mutex
	start  time.Time
	seen   map[string]bool
	values []object
}

func (p *pipelineStages) mark(stage string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.seen[stage] {
		return
	}
	p.seen[stage] = true
	p.values = append(p.values, object{"stage": stage, "atMs": float64(time.Since(p.start)) / float64(time.Millisecond)})
}
func (p *pipelineStages) has(stage string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.seen[stage]
}
func (p *pipelineStages) snapshot() []object {
	p.mu.Lock()
	defer p.mu.Unlock()
	return clone(p.values)
}

type pipelineModel struct {
	model   tea.Model
	session *client.Session
	stages  *pipelineStages
	views   *captureViews
}

func (m *pipelineModel) Init() tea.Cmd { m.stages.mark("tea-init"); return m.model.Init() }
func (m *pipelineModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "x" {
		m.stages.mark("last-key-update-start")
	}
	var cmd tea.Cmd
	m.model, cmd = m.model.Update(msg)
	if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "x" {
		m.stages.mark("last-key-update-end")
	}
	return m, cmd
}
func (m *pipelineModel) View() tea.View {
	v := m.model.View()
	m.views.record(v.Content)
	plain := ansi.Strip(v.Content)
	if strings.Contains(plain, "Initial Markdown for Alpha") {
		m.stages.mark("opening-complete-view-ready")
	}
	if m.stages.has("native-request-event-sent") {
		if strings.Contains(strings.ToLower(plain), "awaiting input") || strings.Contains(plain, "Requests (2)") || strings.Contains(plain, "Requests(2)") {
			m.stages.mark("request-cue-complete-view-ready")
		}
		if strings.Contains(strings.ToLower(plain), "t3 approval") && strings.Contains(plain, "Submit decision") {
			m.stages.mark("request-modal-complete-view-ready")
		}
	}
	return v
}

type pipelineWriter struct {
	file   *os.File
	stages *pipelineStages
}

func (w *pipelineWriter) Fd() uintptr { return w.file.Fd() }
func (w *pipelineWriter) Write(bytes []byte) (int, error) {
	n, err := w.file.Write(bytes)
	if n > 0 && err == nil {
		for _, phase := range []string{"opening", "request-cue", "request-modal"} {
			if w.stages.has(phase + "-complete-view-ready") {
				w.stages.mark(phase + "-first-kernel-write-after-view")
			}
		}
	}
	return n, err
}

// A fresh actual Node process is launched through the normal Session/Connection
// path. The test entry wraps existing adapter methods; no native API, response,
// scheduler or timer is replaced. Node and Go monotonic domains are separate.
func TestPerformanceActualOpeningAndRequestStages(t *testing.T) {
	f := newFixture(t)
	_, thisFile, _, _ := runtime.Caller(0)
	source := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "../.."))
	measurementSource := os.Getenv("T3OXIDE_STAGE_HARNESS_ROOT")
	if measurementSource == "" {
		measurementSource = source
	}
	dir := t.TempDir()
	traceDir := filepath.Join(dir, "traces")
	if err := os.Mkdir(traceDir, 0700); err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { encoded, _ := json.Marshal(value); return string(encoded) }
	wrapper := filepath.Join(dir, "observed-adapter.mjs")
	entry := "import {observeAdapter} from " + quote("file://"+filepath.Join(measurementSource, "tests/adapter-stage-trace.mjs")) + ";\nawait observeAdapter(" + quote(source) + "," + quote(traceDir) + ");\n"
	if err := os.WriteFile(wrapper, []byte(entry), 0600); err != nil {
		t.Fatal(err)
	}
	f.profile.Adapter = wrapper
	manager, state := newAppManager(t, f)
	old, err := manager.NewSession(f.profile.ID, "thread-b")
	if err != nil {
		t.Fatal(err)
	}
	const exactDraft = "  synthetic outgoing draft\r\nretained  "
	if err = old.SetDraft(exactDraft); err != nil {
		t.Fatal(err)
	}
	stages := &pipelineStages{start: time.Now(), seen: map[string]bool{}}
	stages.mark("open-target-call-admitted")
	a, err := old.Target(f.profile.ID, "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	if state.Draft(f.environmentID, "thread-b") != exactDraft {
		t.Fatal("Target returned before exact outgoing draft was persisted")
	}
	stages.mark("outgoing-draft-verified-and-lazy-target-returned")
	master, slave := openPerformancePTY(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	settings, err := ui.LoadSettings(filepath.Join(dir, "terminal.json"))
	if err != nil {
		t.Fatal(err)
	}
	model := ui.NewModel(ctx, a, settings)
	defer model.Close()
	views := &captureViews{}
	program := tea.NewProgram(&pipelineModel{model, a, stages, views}, tea.WithContext(ctx), tea.WithInput(slave), tea.WithOutput(&pipelineWriter{slave, stages}), tea.WithWindowSize(120, 35), tea.WithoutSignalHandler(), tea.WithEnvironment([]string{"TERM=xterm-256color"}))
	finished := make(chan error, 1)
	stages.mark("tea-run-invoked")
	go func() { _, err := program.Run(); finished <- err }()
	go func() { _, _ = io.Copy(io.Discard, master) }()
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			// Join Tea's terminal reader through normal Quit before fixture
			// descriptor cleanup. Forced Kill skips that join in Tea v2.1.0.
			program.Quit()
			select {
			case <-finished:
			case <-time.After(4 * time.Second):
				t.Error("stage program did not stop")
			}
			cancel()
			_ = model.Close()
			a.Close()
			if err := manager.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	defer stop()
	// Status() is the public defensive projection. These observations are
	// upper bounds after Go state application, sampled at 1 ms plus scheduling.
	go func() {
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				s := a.Status()
				if s.Location.Title == "Alpha" {
					stages.mark("go-first-bounded-state-observed")
				}
				if s.Location.Title == "Alpha" && s.ConnectionState == "connected" {
					stages.mark("go-live-state-observed")
				}
				if s.Requests == 2 {
					stages.mark("go-exact-request-count-observed")
				}
			}
		}
	}()
	wait := func(stage string) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for !stages.has(stage) && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if !stages.has(stage) {
			t.Fatalf("stage %s not observed", stage)
		}
	}
	wait("opening-first-kernel-write-after-view")
	wait("go-live-state-observed")
	stages.mark("key-admitted-to-pty")
	if _, err = master.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	wait("last-key-update-end")
	f.addRequests()
	stages.mark("native-request-event-sent")
	f.broadcast("thread-a", "runtime-request.updated", object{"id": "approval-a"}, 0)
	wait("go-exact-request-count-observed")
	wait("request-cue-first-kernel-write-after-view")
	_, attentionErr := os.Stat(filepath.Join(source, "internal/ui/request_attention.go"))
	attention := attentionErr == nil
	if attention {
		wait("request-modal-first-kernel-write-after-view")
	}
	if a.Draft() != "x" || state.Draft(f.environmentID, "thread-b") != exactDraft {
		t.Fatal("opening/request observation lost exact drafts")
	}
	stop()
	files, err := filepath.Glob(filepath.Join(traceDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	helpers := []object{}
	activeHelpers := 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var trace object
		if err = json.Unmarshal(data, &trace); err != nil {
			t.Fatal(err)
		}
		transcript := false
		nativeStages, _ := trace["stages"].([]any)
		for _, raw := range nativeStages {
			if raw.(object)["stage"] == "subscription-call-admitted" {
				transcript = true
			}
		}
		if transcript {
			activeHelpers++
		}
		trace["helperOrdinal"] = len(helpers) + 1
		trace["transcriptAttachment"] = transcript
		helpers = append(helpers, trace)
	}
	if activeHelpers != 1 {
		t.Fatalf("expected one fresh transcript helper, observed %d", activeHelpers)
	}
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("stage observations mutated fixture")
	}
	record, _ := json.Marshal(object{"schema": 1, "boundary": "actual fresh helper, exact Session.Target draft flush, actual Tea+Linux PTY; independent Node and Go monotonic clocks; no paint or physical input", "goStages": stages.snapshot(), "nativeHelpers": helpers, "goStateObservation": "1ms public Status polling plus scheduling: upper bound after state apply, not internal apply timestamp", "openingAction": "Session.Target call; returned boundary includes outgoing synchronous durable flush and lazy target creation; selection UI excluded", "processBoundary": "Node JS wrapper entry after spawn; no direct cmd.Start timestamp; Tea run invocation is separately observed", "outputBoundary": "first slave kernel write after complete View, may include renderer housekeeping; master drained in test", "automaticRequestAttention": attention, "intentionalIdleGuardMs": 1500, "warmup": "fresh helper processes; warm OS/compiler/module caches; no network/SSH cold claim", "effects": 0, "dispatches": 0, "oldDraftExact": true, "activeDraftExact": true})
	t.Log(string(record))
}
