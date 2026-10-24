package integration_test

// End-to-end UI checks for native/full-toolchain runs. The cloud verification
// record distinguishes these from the backend tests actually executable offline.
import (
	"strings"
	"testing"
	"time"

	"github.com/Xon333/t3oxide/internal/client"
)

func TestBubbleTeaFeaturesModelPickersAndGroupedNavigation(t *testing.T) {
	f := newFixture(t)
	addFeatureFamily(f)
	m, state := newAppManager(t, f)
	a, err := m.NewSession("fixture", "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	r, _ := startAppTUI(t, a)
	emptyComposerReady := func() {
		// Modern Ctrl+P is available while commands are pending too. Wait for
		// the visible composer to clear before entering the next command.
		r.waitEmptyComposer()
	}
	r.waitText("Initial Markdown for Alpha")
	awaitStatus(t, a, func(s client.Status) bool { return s.ModelControls })
	r.send("/model\r")
	r.modalReady("Model · Current thread")
	r.send("\x1b[B\r")
	awaitStatus(t, a, func(s client.Status) bool { return strings.Contains(s.ModelName, "fixture-other") })
	r.waitText("fixture-other")
	emptyComposerReady()
	r.send("/effort\r")
	r.modalReady("Reasoning · Current thread")
	r.send("\x1b[B\x1b[B\x1b[B\r")
	r.waitText("high effort")
	emptyComposerReady()
	r.send("/speed\r")
	r.modalReady("Service tier · Current thread")
	r.send("\x1b[B\x1b[B\r")
	r.waitText("fast tier")
	emptyComposerReady()
	r.send("/presets save Focus\r")
	r.waitText("Model preset saved locally")
	emptyComposerReady()
	if len(state.Presets("env-a")) != 1 {
		t.Fatal("UI preset was not saved")
	}
	r.send("/model fixture-basic\r")
	r.waitText("fixture-basic")
	emptyComposerReady()
	r.send("/presets\r")
	r.modalReady("Model presets · Current thread")
	r.send("Apply a preset\r")
	r.waitText("fixture-other · high effort · fast tier")
	emptyComposerReady()
	r.send("/presets remove Focus\r")
	r.waitText("Preset removed")
	emptyComposerReady()
	if len(state.Presets("env-a")) != 0 {
		t.Fatal("UI preset removal was not saved")
	}
	r.send("/model next fixture-model\r")
	r.waitText("Next send · fixture-model")
	emptyComposerReady()
	r.send("One-dispatch UI override\r")
	f.waitMessage(t, "One-dispatch UI override")
	r.waitText("Response after One-dispatch UI override")
	deadline := time.Now().Add(3 * time.Second)
	for state.Override("env-a", "thread-a") != nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if state.Override("env-a", "thread-a") != nil {
		t.Fatal("accepted UI override not cleared")
	}
	r.send("/threads\r")
	r.modalReady("Conversations")
	// The query owns Space and arrow editing. Reach the offered group action
	// through the visible More control and its native filterable list.
	r.send("\x1b[Z")
	r.waitText("[More]")
	r.send("\r")
	r.waitText("Refresh")
	r.send("Expand\r")
	r.waitText("SUB child")
	r.send("\x1b[B\r")
	r.waitText("Subagent")
	r.send("/parent\r")
	r.waitText("Initial Markdown for Alpha")
	r.send("/threads\r")
	r.modalReady("Conversations")
	r.send("\x1b[Z")
	r.waitText("[More]")
	r.send("\r")
	r.waitText("Refresh")
	r.send("Thread type\r")
	r.modalReady("Thread types")
	r.send("\x1b[B\x1b[B\x1b[B\r")
	r.modalReady("Conversations")
	r.waitText("FORK fork")
	r.send("\r")
	r.waitText("Agent · fork")
	r.send("\x11")
	select {
	case <-r.done:
		if r.err != nil {
			t.Fatal(r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("feature UI failed to quit")
	}
	assertTerminalReset(t, r.output.raw())
}
func TestBubbleTeaFeaturesStopWhileModelPickerIsOpen(t *testing.T) {
	f := newFixture(t)
	m, _ := newAppManager(t, f)
	a, err := m.NewSession("fixture", "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	r, _ := startAppTUI(t, a)
	r.waitText("Initial Markdown for Alpha")
	awaitStatus(t, a, func(s client.Status) bool { return s.ModelControls })
	r.send("/model\r")
	r.modalReady("Model · Current thread")
	r.send("\x13")
	waitCommandType(t, f, "run.interrupt")
	r.waitText("Stop accepted")
	if effects, dispatches := f.counts(); effects != 1 || dispatches != 1 {
		t.Fatal("picker or Stop duplicated a mutation")
	}
	r.send("\x1b")
	// Let Tea resolve the standalone Escape before sending the next key;
	// adjacent bytes would describe Alt+Ctrl+D to a terminal input parser.
	deadline := time.Now().Add(3 * time.Second)
	for strings.Contains(r.views.text(), "Model · Current thread") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if strings.Contains(r.views.text(), "Model · Current thread") {
		t.Fatal("Escape retained the model picker after Stop")
	}
	r.send("\x11")
	select {
	case <-r.done:
		if r.err != nil {
			t.Fatal(r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("picker Stop quit failed")
	}
	assertTerminalReset(t, r.output.raw())
}
