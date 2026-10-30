package integration_test

// End-to-end UI checks for native/full-toolchain runs. The cloud verification
// record distinguishes these from the backend tests actually executable offline.
import (
	"strings"
	"testing"
	"time"

	"github.com/Xon333/t3oxide/internal/client"
)

func TestBubbleTeaFeaturesModelPickersAndExplicitChildNavigation(t *testing.T) {
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
	r.send("/model\x1d")
	r.modalReady("Model · Current thread")
	r.send("\x1b[B\r")
	awaitStatus(t, a, func(s client.Status) bool { return strings.Contains(s.ModelName, "fixture-other") })
	r.waitText("fixture-other")
	emptyComposerReady()
	r.send("/effort\x1d")
	r.modalReady("Reasoning · Current thread")
	r.send("\x1b[B\x1b[B\x1b[B\r")
	r.waitText("high effort")
	emptyComposerReady()
	r.send("/speed\x1d")
	r.modalReady("Service tier · Current thread")
	r.send("\x1b[B\x1b[B\r")
	r.waitText("fast tier")
	emptyComposerReady()
	r.send("/presets save Focus\x1d")
	r.waitText("Model preset saved locally")
	emptyComposerReady()
	if len(state.Presets("env-a")) != 1 {
		t.Fatal("UI preset was not saved")
	}
	r.send("/model fixture-basic\x1d")
	r.waitText("fixture-basic")
	emptyComposerReady()
	r.send("/presets\x1d")
	r.modalReady("Model presets · Current thread")
	r.send("Apply a preset\r")
	r.waitText("fixture-other · high effort · fast tier")
	emptyComposerReady()
	r.send("/presets remove Focus\x1d")
	r.waitText("Preset removed")
	emptyComposerReady()
	if len(state.Presets("env-a")) != 0 {
		t.Fatal("UI preset removal was not saved")
	}
	r.send("/model next fixture-model\x1d")
	r.waitText("Next send · fixture-model")
	emptyComposerReady()
	r.send("One-dispatch UI override\x1d")
	f.waitMessage(t, "One-dispatch UI override")
	r.waitText("Response after One-dispatch UI override")
	deadline := time.Now().Add(3 * time.Second)
	for state.Override("env-a", "thread-a") != nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if state.Override("env-a", "thread-a") != nil {
		t.Fatal("accepted UI override not cleared")
	}
	r.send("/threads\x1d")
	waitNavigatorQuery(t, r, "Filter loaded")
	if strings.Contains(r.views.text(), "[FI◇] child") {
		t.Fatal("subagents expanded before an explicit source action")
	}
	r.send("Alpha")
	waitNavigatorQuery(t, r, "Alpha")
	r.send("\x1ba")
	r.modalReady("More · Conversations")
	r.send("Show working children\r")
	waitNavigatorQuery(t, r, "Alpha")
	r.send("\x15")
	waitNavigatorQuery(t, r, "Filter loaded")
	r.waitText("[FI◇] child")
	r.send("child")
	waitNavigatorQuery(t, r, "child")
	r.send("\r")
	r.waitText("Initial Markdown for child")
	r.send("/parent\x1d")
	r.waitText("Initial Markdown for Alpha")
	r.send("/threads\x1d")
	r.waitText("Conversations")
	r.send("\x15")
	waitNavigatorQuery(t, r, "Filter loaded")
	r.send("\x1ba")
	r.modalReady("More · Conversations")
	r.send("Type · forks\r")
	waitWorkspaceFrame(t, r, func(view string) bool {
		return strings.Contains(view, "[FI◇] fork") && !strings.Contains(view, "[FI◇] Alpha") && !strings.Contains(view, "[FI◇] Beta")
	}, "fork-only native metadata")
	r.send("\r")
	r.waitText("Initial Markdown for fork")
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
	r.send("/model\x1d")
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
