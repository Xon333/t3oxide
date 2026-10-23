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
	r.waitText("Initial Markdown for Alpha")
	awaitStatus(t, a, func(s client.Status) bool { return s.ModelControls })
	r.send("\x1bm")
	r.modalReady("Model · Current thread")
	r.send("\x1b[B\r")
	awaitStatus(t, a, func(s client.Status) bool { return strings.Contains(s.ModelName, "fixture-other") })
	r.waitText("Thread: fixture-other")
	r.send("\x1be")
	r.modalReady("Reasoning · Current thread")
	r.send("\x1b[B\x1b[B\x1b[B\r")
	r.waitText("high effort")
	r.send("\x1bs")
	r.modalReady("Service tier · Current thread")
	r.send("\x1b[B\x1b[B\r")
	r.waitText("fast tier")
	r.send("/presets save Focus\r")
	r.waitText("Model preset saved locally")
	if len(state.Presets("env-a")) != 1 {
		t.Fatal("UI preset was not saved")
	}
	r.send("/model fixture-basic\r")
	r.waitText("Thread: fixture-basic")
	r.send("/presets\r")
	r.modalReady("Presets · Current thread")
	r.send("\x1b[B\x1b[B\r")
	r.waitText("Thread: fixture-other · high effort · fast tier")
	r.send("/presets remove\r")
	r.modalReady("Remove local preset")
	r.send("\r")
	r.waitText("Preset removed")
	if len(state.Presets("env-a")) != 0 {
		t.Fatal("UI preset removal was not saved")
	}
	r.send("/model next fixture-model\r")
	r.waitText("Next send staged:")
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
	r.modalReady("Threads · all")
	r.send(" ")
	r.waitText("SUB child")
	r.send("\x1b[B\r")
	r.waitText("[SUBAGENT]")
	r.send("/parent\r")
	r.waitText("Initial Markdown for Alpha")
	r.send("/threads\r")
	r.modalReady("Threads · all")
	r.send("\x1b[17~") // F6 opens the authoritative thread-type filter.
	r.modalReady("Thread types")
	r.send("\x1b[B\x1b[B\x1b[B\r")
	r.modalReady("Threads · forks")
	r.waitText("FORK fork")
	r.send("\r")
	r.waitText("[FORK]")
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
	r.send("\x1bm")
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
	r.waitText("Dismissed locally")
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
