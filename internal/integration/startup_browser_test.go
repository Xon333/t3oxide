package integration_test

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
)

func TestBubbleTeaStartupBrowserOpensSelectedConversationAndKeepsLaterBrowseIndependent(t *testing.T) {
	f := newFixture(t)
	manager, state := newAppManager(t, f)
	const draft = "  exact startup draft\nsecond line  "
	if err := state.SetDraft("env-a", "thread-b", draft); err != nil {
		t.Fatal(err)
	}
	session, err := manager.NewSession(f.profile.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	r, _ := startAppTUI(t, session)
	r.modalReady("Conversations")
	if counts := f.requestCounts(); counts["rpc:orchestration.subscribeThread"] != 0 {
		t.Fatal("startup subscribed before an explicit open")
	}
	r.send("Beta")
	r.waitText("Filter: Beta")
	r.send("\r")
	awaitStatus(t, session, func(s client.Status) bool {
		return s.ThreadID == "thread-b" && s.Connected
	})
	r.waitText("Initial Markdown for Beta")
	r.waitText("╭─ Message")
	if view := r.views.text(); strings.Contains(view, "Loading…") || strings.Contains(view, "Conversations") {
		t.Fatalf("startup retained the browser after opening the selected thread:\n%s", view)
	}
	if session.Draft() != draft {
		t.Fatalf("startup draft changed: %q", session.Draft())
	}

	// Live updates must not dismiss an intentionally reopened browser.
	r.send("\x0f")
	r.modalReady("Conversations")
	const pushed = "Live update while explicitly browsing"
	f.update("thread-b", pushed, 0)
	f.mu.Lock()
	run := f.threads["thread-b"]["runs"].([]any)[0].(object)
	run["status"] = "completed"
	completed := clone(run)
	f.mu.Unlock()
	f.broadcast("thread-b", "run.updated", completed, 0)
	awaitStatus(t, session, func(s client.Status) bool { return s.RunState == "completed" })
	r.waitText("completed")
	if view := r.views.text(); !strings.Contains(view, "Conversations") || strings.Contains(view, pushed) {
		t.Fatalf("live event replaced the explicit browser:\n%s", view)
	}
	r.send("\x1b")
	r.waitText(pushed)
	r.waitText("╭─ Message")
	if session.Draft() != draft {
		t.Fatal("Resume changed the selected thread draft")
	}
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("browsing/opening mutated the fixture")
	}
	r.send("\x11")
	select {
	case <-r.done:
		if r.err != nil {
			t.Fatal(r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("startup browser program did not quit")
	}
	assertTerminalReset(t, r.output.raw())
}

func TestBubbleTeaLaunchReviewReachesLocalSettingsBeforeCreate(t *testing.T) {
	f := newFixture(t)
	manager, state := newAppManager(t, f)
	session, err := manager.NewSession(f.profile.ID, "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	r, _ := startAppTUI(t, session)
	r.waitText("Initial Markdown for Alpha")
	r.send("/new\r")
	r.waitText("First message")
	const message = "Exact first message before settings"
	r.program.Send(tea.PasteMsg{Content: message})
	r.waitText(message)
	r.send(strings.Repeat("\t", 3)) // Title, Editor, Model.
	r.waitText("[Model]")
	r.send("\r")
	r.modalReady("Model · New conversation")
	r.send("\x1b")
	r.waitText("First message")
	r.waitText(message)
	r.send(strings.Repeat("\t", 4)) // Title, Editor, Model, Effort.
	for _, control := range []string{"Effort", "Tier", "Create", "Cancel"} {
		r.waitText("[" + control + "]")
		if control != "Cancel" {
			r.send("\t")
		}
	}
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("local review/settings focus dispatched a mutation")
	}
	r.send("\r") // Explicit Cancel, never Create.
	r.waitText("Initial Markdown for Alpha")
	if state.Draft("env-a", "\x00new/thread-a") != message || session.Draft() != "" {
		t.Fatal("review cancellation crossed launch/source draft slots")
	}
	r.send("\x11")
	select {
	case <-r.done:
		if r.err != nil {
			t.Fatal(r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("launch control program did not quit")
	}
	assertTerminalReset(t, r.output.raw())
}
