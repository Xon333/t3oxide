package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/ui"
	"github.com/charmbracelet/x/ansi"
)

type captureBuffer struct {
	mu    sync.Mutex
	value bytes.Buffer
}

func (b *captureBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.value.Write(p)
}
func (b *captureBuffer) text() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return ansi.Strip(b.value.String())
}
func (b *captureBuffer) raw() string { b.mu.Lock(); defer b.mu.Unlock(); return b.value.String() }

// Tea emits terminal differences, so stripping ANSI from captured output does
// not reconstruct a screen: a changed "Notes" title may emit only "Note" and
// retain its final "s" from the previous frame. Observe the actual complete
// View passed to Tea instead. Raw terminal output is still captured separately
// for lifecycle assertions; input and every production Update/Cmd are unchanged.
type captureViews struct {
	mu     sync.Mutex
	latest string
	frames []string
}

func (v *captureViews) record(content string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	plain := ansi.Strip(content)
	if plain == v.latest {
		return
	}
	v.latest = plain
	if len(v.frames) == 128 {
		copy(v.frames, v.frames[1:])
		v.frames = v.frames[:127]
	}
	v.frames = append(v.frames, plain)
}
func (v *captureViews) text() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.latest
}
func (v *captureViews) history() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return strings.Join(v.frames, "\n\n")
}

type observedModel struct {
	model tea.Model
	views *captureViews
}

func (m *observedModel) Init() tea.Cmd { return m.model.Init() }
func (m *observedModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.model, cmd = m.model.Update(msg)
	return m, cmd
}
func (m *observedModel) View() tea.View {
	v := m.model.View()
	m.views.record(v.Content)
	return v
}

// This rig runs the direct Bubble Tea model/event loop, real T3oxide client and
// real Node adapter. Keyboard bytes enter Bubble Tea's normal input parser. It is a
// non-TTY program test; termios and the physical macOS clipboard remain native
// acceptance items.
type tuiProgram struct {
	t       *testing.T
	program *tea.Program
	input   *io.PipeWriter
	reader  *io.PipeReader
	output  *captureBuffer
	views   *captureViews
	cancel  context.CancelFunc
	done    chan struct{}
	err     error
}

func startAppTUI(t *testing.T, a *client.Session, initialTheme ...string) (*tuiProgram, string) {
	t.Helper()
	settingsPath := filepath.Join(t.TempDir(), "terminal.json")
	settings, err := ui.LoadSettings(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(initialTheme) > 0 {
		if err := settings.SetTheme(initialTheme[0]); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	reader, input := io.Pipe()
	r := &tuiProgram{t: t, input: input, reader: reader, output: &captureBuffer{}, views: &captureViews{}, cancel: cancel, done: make(chan struct{})}
	model := ui.NewModel(ctx, a, settings)
	r.program = tea.NewProgram(&observedModel{model: model, views: r.views}, tea.WithContext(ctx), tea.WithInput(reader), tea.WithOutput(r.output),
		tea.WithWindowSize(100, 34), tea.WithoutSignalHandler(), tea.WithEnvironment([]string{"TERM=xterm-256color"}))
	go func() { _, r.err = r.program.Run(); close(r.done) }()
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("direct Bubble Tea last complete view:\n%s", r.views.text())
			t.Logf("direct Bubble Tea output:\n%s", r.output.text())
		}
		r.stop()
		model.Close()
		if a != nil {
			a.Close()
		}
	})
	return r, settingsPath
}
func (r *tuiProgram) stop() {
	r.cancel()
	_ = r.input.Close()
	_ = r.reader.Close()
	select {
	case <-r.done:
	case <-time.After(4 * time.Second):
		r.program.Kill()
		r.t.Error("direct Bubble Tea did not shut down after context cancellation")
	}
}
func (r *tuiProgram) send(keys string) {
	r.t.Helper()
	done := make(chan error, 1)
	go func() { _, err := r.input.Write([]byte(keys)); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			r.t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		r.t.Fatal("direct Bubble Tea stopped reading keyboard input")
	}
}
func (r *tuiProgram) waitText(want string) {
	r.t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(r.views.text(), want) {
			return
		}
		select {
		case <-r.done:
			r.t.Fatalf("direct Bubble Tea exited early: %v\n%s", r.err, r.output.text())
		default:
		}
		time.Sleep(15 * time.Millisecond)
	}
	r.t.Fatalf("direct Bubble Tea never rendered %q\n%s", want, r.views.text())
}

func (r *tuiProgram) modalReady(want string) {
	r.t.Helper()
	r.waitText(want)

}

func TestBubbleTeaProgramDrivesActualAppComposerThemeResizeAndQuit(t *testing.T) {
	f := newFixture(t)
	m, state := newAppManager(t, f)
	a, err := m.NewSession("fixture", "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	r, settingsPath := startAppTUI(t, a)
	r.waitText("Alpha")
	awaitStatus(t, a, func(s client.Status) bool { return strings.Contains(s.State, "running") })
	r.send("Message through the actual composer\r")
	f.waitMessage(t, "Message through the actual composer")
	r.waitText("Response after Message")
	if state.Pending("env-a", false) != nil {
		t.Fatal("composer success did not acknowledge operation")
	}
	r.send("/theme cyan\r")
	deadline := time.Now().Add(5 * time.Second)
	changed := false
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(settingsPath)
		if err == nil {
			var settings map[string]any
			if json.Unmarshal(data, &settings) == nil {
				environments, _ := settings["environmentThemes"].(map[string]any)
				if environments["fixture"] != "cyan" {
					continue
				}
				changed = true
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !changed {
		t.Fatalf("/theme did not persist through direct Bubble Tea: %s", r.output.text())
	}
	r.program.Send(tea.WindowSizeMsg{Width: 44, Height: 16})
	r.send("draft after resize")
	deadline = time.Now().Add(3 * time.Second)
	for a.Draft() != "draft after resize" && time.Now().Before(deadline) {
		time.Sleep(15 * time.Millisecond)
	}
	if a.Draft() != "draft after resize" {
		t.Fatalf("composer lost focus after resize: %q", a.Draft())
	}
	r.program.Send(tea.WindowSizeMsg{Width: 160, Height: 42})
	// Ctrl+U clears this unsent buffer; it is not a remote stop.
	r.send("\x15/quit\r")
	select {
	case <-r.done:
		if r.err != nil {
			t.Fatalf("normal direct Bubble Tea quit: %v", r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("direct Bubble Tea quit stalled\n%s", r.output.text())
	}
	assertTerminalReset(t, r.output.raw())
	if effects, dispatches := f.counts(); effects != 1 || dispatches != 1 {
		t.Fatalf("UI lifecycle duplicated a submission or stopped remote work: %d/%d", effects, dispatches)
	}
}

func assertTerminalReset(t *testing.T, raw string) {
	t.Helper()
	// View colors are owned by Bubble Tea and must leave the user's terminal
	// defaults intact, just like the alternate screen and bracketed paste.
	for _, pair := range [][2]string{{"\x1b]10;", "\x1b]110"}, {"\x1b]11;", "\x1b]111"}} {
		if strings.Contains(raw, pair[0]) && strings.LastIndex(raw, pair[1]) < strings.LastIndex(raw, pair[0]) {
			t.Fatalf("terminal color %q was not restored", pair[0])
		}
	}
	for _, pair := range [][2]string{{"\x1b[?1049h", "\x1b[?1049l"}, {"\x1b[?25l", "\x1b[?25h"}, {"\x1b[?2004h", "\x1b[?2004l"}} {
		if strings.Contains(raw, pair[0]) && strings.LastIndex(raw, pair[1]) < strings.LastIndex(raw, pair[0]) {
			t.Fatalf("terminal mode %q was enabled without final restoration %q", pair[0], pair[1])
		}
	}
}

func TestBubbleTeaTerminalThemeStartsInheritedAndReleasesFixedColors(t *testing.T) {
	r, settingsPath := startAppTUI(t, nil, "terminal")
	r.waitText("t3oxide")
	if raw := r.output.raw(); strings.Contains(raw, "\x1b]10;") || strings.Contains(raw, "\x1b]11;") {
		t.Fatal("terminal theme changed terminal defaults on startup")
	}
	awaitOutput := func(ready func(string) bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if ready(r.output.raw()) {
				return
			}
			time.Sleep(15 * time.Millisecond)
		}
		t.Fatal("native renderer did not apply/reset terminal colors")
	}
	r.send("/theme cyan\r")
	awaitOutput(func(raw string) bool { return strings.Contains(raw, "\x1b]10;") && strings.Contains(raw, "\x1b]11;") })
	r.send("/theme terminal\r")
	awaitOutput(func(raw string) bool {
		return strings.LastIndex(raw, "\x1b]110") > strings.LastIndex(raw, "\x1b]10;") && strings.LastIndex(raw, "\x1b]111") > strings.LastIndex(raw, "\x1b]11;")
	})
	data, err := os.ReadFile(settingsPath)
	if err != nil || !strings.Contains(string(data), `"theme": "terminal"`) {
		t.Fatalf("terminal theme did not persist: %s, %v", data, err)
	}
	r.send("/quit\r")
	select {
	case <-r.done:
		if r.err != nil {
			t.Fatal(r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("terminal theme quit stalled")
	}
	assertTerminalReset(t, r.output.raw())
}

func TestAppRequestDialogIsCancelledOnDetach(t *testing.T) {
	f := newFixture(t)
	f.addRequests()
	m, _ := newAppManager(t, f)
	a, err := m.NewSession("fixture", "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	stream := streamSession(t, a)
	stream.until(t, func(e client.Event) bool { return containsMessage(e, "thread-a/first-thread-a", "Initial") })
	awaitStatus(t, a, func(s client.Status) bool { return strings.Contains(s.State, "awaiting input") })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := a.Command(ctx, "requests", ""); result <- err }()
	// No test responder drains Prompts: the call is parked at the same modal
	// boundary the production direct Bubble Tea listener owns.
	stream.stop(t)
	select {
	case <-result:
	case <-time.After(2 * time.Second):
		t.Fatal("outgoing pending request remained attached after local detach")
	}
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("dismissing a question synthesized a remote answer")
	}
}

func TestBubbleTeaProgramThreadSwitchHistoryReconnectAndSearch(t *testing.T) {
	f := newFixture(t)
	m, state := newAppManager(t, f)
	a, err := m.NewSession("fixture", "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	r, _ := startAppTUI(t, a)
	r.waitText("Initial Markdown for Alpha")
	r.send("/switch Beta\r")
	r.modalReady("Conversations")
	r.send("\r")
	r.waitText("Initial Markdown for Beta")
	f.update("thread-a", "A retired conversation must not render here", 0)
	f.update("thread-b", "Current Beta live revision", 0)
	r.waitText("Current Beta live revision")
	if strings.Contains(r.views.history(), "A retired conversation must not render here") {
		t.Fatal("old conversation update crossed a UI attachment generation")
	}
	r.send("/earlier\r")
	r.waitText("Earlier page for thread-b")
	f.update("thread-b", "Beta changed while reading an earlier page", 0)
	r.send("/latest\r")
	r.waitText("Beta changed while reading an earlier page")
	r.send("/search Beta changed\r")
	r.waitText("Find 1/1 matching lines")
	r.send("Unsent β draft after search")
	deadline := time.Now().Add(3 * time.Second)
	for state.Draft("env-a", "thread-b") != "Unsent β draft after search" && time.Now().Before(deadline) {
		time.Sleep(15 * time.Millisecond)
	}
	if got := state.Draft("env-a", "thread-b"); got != "Unsent β draft after search" {
		t.Fatalf("search did not return focus to composer: %q", got)
	}
	f.corruptSockets()
	f.update("thread-b", "Beta resumed after malformed upstream frame", 0)
	r.waitText("Beta resumed after malformed upstream frame")
	if state.Draft("env-a", "thread-b") != "Unsent β draft after search" {
		t.Fatal("reconnect lost the active conversation's draft")
	}
	if state.Draft("env-a", "thread-a") != "" {
		t.Fatal("new conversation draft was written into retired conversation")
	}
	r.send("\x11")
	select {
	case <-r.done:
		if r.err != nil {
			t.Fatal(r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("quit failed after history/reconnect/navigation")
	}
	assertTerminalReset(t, r.output.raw())
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatalf("navigation or local quit caused a remote action: %d/%d", effects, dispatches)
	}
}

func TestBubbleTeaProgramAnswersActualT3ApprovalAndQuestions(t *testing.T) {
	f := newFixture(t)
	f.addRequests()
	m, state := newAppManager(t, f)
	a, err := m.NewSession("fixture", "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	r, _ := startAppTUI(t, a)
	r.waitText("Alpha")
	awaitStatus(t, a, func(s client.Status) bool { return strings.Contains(s.State, "awaiting input (2)") })
	r.send("/requests\r")
	r.modalReady("Pending T3 requests")
	r.send("\r")
	r.modalReady("Once")
	r.send("\r")
	approval := f.waitRequest(t, "approval-a")
	if approval["decision"] != "accept" {
		t.Fatalf("first offered approval changed: %#v", approval)
	}
	awaitStatus(t, a, func(s client.Status) bool { return strings.Contains(s.State, "awaiting input (1)") })
	// The host projection can arrive before the program consumes actionDone.
	// Wait for its empty composer before typing another slash command.
	r.waitText("Ctrl+R requests  Ctrl+P commands  Enter send")
	r.send("/requests\r")
	r.modalReady("Choice")
	r.send("Continue\r")
	r.modalReady("Notes")
	r.send("operator note\r")
	question := f.waitRequest(t, "question-a")
	answers, ok := question["answers"].(object)
	if !ok || answers["choice"] != "Continue" || answers["notes"] != "operator note" {
		t.Fatalf("T3 question IDs/answers changed: %#v", question)
	}
	deadline := time.Now().Add(3 * time.Second)
	for state.Pending("env-a", false) != nil && time.Now().Before(deadline) {
		time.Sleep(15 * time.Millisecond)
	}
	if state.Pending("env-a", false) != nil {
		t.Fatal("question acknowledgement did not settle")
	}
	r.waitText("Ctrl+S Stop/hold  Ctrl+P commands  Enter send")
	r.send("/quit\r")
	select {
	case <-r.done:
		if r.err != nil {
			t.Fatal(r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("quit failed after request dialogs")
	}
	if effects, dispatches := f.counts(); effects != 2 || dispatches != 2 {
		t.Fatalf("request UI duplicated or synthesized actions: %d/%d", effects, dispatches)
	}
}

func (f *fixture) waitRequest(t *testing.T, requestID string) object {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		for _, command := range f.commands {
			if command["requestId"] == requestID {
				result := clone(command)
				f.mu.Unlock()
				return result
			}
		}
		f.mu.Unlock()
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("actual request UI did not answer %s", requestID)
	return nil
}

func TestBubbleTeaStopShortcutWorksDuringRequestModalAndPreservesComposer(t *testing.T) {
	f := newFixture(t)
	f.addRequests()
	m, _ := newAppManager(t, f)
	a, err := m.NewSession("fixture", "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	r, _ := startAppTUI(t, a)
	r.waitText("Alpha")
	awaitStatus(t, a, func(s client.Status) bool { return strings.Contains(s.State, "running") })
	r.send("Unsent composer while stopping")
	deadline := time.Now().Add(3 * time.Second)
	for a.Draft() != "Unsent composer while stopping" && time.Now().Before(deadline) {
		time.Sleep(15 * time.Millisecond)
	}
	// The request review owns the ordinary action path. The global Stop key
	// must still use its independent request and must not answer this modal.
	r.send("\x12")
	r.modalReady("Pending T3 requests")
	r.send("\x13")
	deadline = time.Now().Add(4 * time.Second)
	var stop object
	for time.Now().Before(deadline) {
		f.mu.Lock()
		for _, command := range f.commands {
			if command["type"] == "run.interrupt" {
				stop = clone(command)
			}
		}
		f.mu.Unlock()
		if stop != nil {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if stop == nil || stop["holdQueue"] != true || stop["runId"] != "run-thread-a" {
		t.Fatalf("Ctrl+S did not send the reviewed Stop with queue hold: %#v\n%s", stop, r.output.text())
	}
	if a.Draft() != "Unsent composer while stopping" {
		t.Fatal("Stop shortcut altered the composer draft")
	}
	if effects, dispatches := f.counts(); effects != 1 || dispatches != 1 {
		t.Fatal("Stop shortcut also submitted composer text")
	}
	r.send("\x11")
	select {
	case <-r.done:
		if r.err != nil {
			t.Fatal(r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("quit failed after Stop")
	}
}
