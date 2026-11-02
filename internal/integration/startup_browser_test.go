package integration_test

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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
	r.waitText("Alpha")
	r.waitText("Beta")
	if counts := f.requestCounts(); counts["rpc:orchestration.subscribeThread"] != 0 {
		t.Fatal("startup subscribed before an explicit open")
	}
	if view := r.views.text(); strings.Contains(view, "Initial Markdown") || strings.Contains(view, "Message") || strings.Contains(view, "╭─ Conversations") {
		t.Fatalf("startup is not the metadata list and initially empty conversation: %s", view)
	}
	r.send("Beta")
	r.waitText("Beta")
	r.send("\r")
	r.waitText("Initial Markdown for Beta")
	r.waitText("second line")
	if view := r.views.text(); strings.Contains(view, "Loading…") || !strings.Contains(view, "Conversations") {
		t.Fatalf("explicit open did not retain the unified list: %s", view)
	}
	if state.Draft("env-a", "thread-b") != draft || session.Status().ThreadID != "" {
		t.Fatal("lazy explicit open changed the exact draft or retargeted the startup session")
	}

	// Metadata focus is independent of the same live conversation. Events
	// continue on the right without forcing focus or opening another thread.
	r.send("\x0f")
	r.waitText("› Conversations")
	const pushed = "Live update while explicitly browsing"
	f.update("thread-b", pushed, 0)
	r.waitText(pushed)
	if view := r.views.text(); !strings.Contains(view, "› Conversations") {
		t.Fatalf("live update stole metadata focus: %s", view)
	}
	r.send("\x0f")
	r.waitText("╭─ > Message")
	if state.Draft("env-a", "thread-b") != draft {
		t.Fatal("metadata focus changed the selected thread draft")
	}
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("browsing/opening mutated the fixture")
	}
	if counts := f.requestCounts(); counts["rpc:orchestration.subscribeThread"] != 1 || counts["/api/orchestration/threads/thread-a/bounded"] != 0 {
		t.Fatalf("metadata focus created extra detail reads: %+v", counts)
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
	r.send("/new\x1d")
	r.modalReady("╭─ New ─")
	const message = "Exact first message before settings"
	r.program.Send(tea.PasteMsg{Content: message})
	r.waitText(message)
	r.send("\r")
	r.modalReady("Project")
	r.send("\r")
	r.waitText("[Create & send]")
	r.send("\t\t") // Optional Model.
	r.waitText("[Model]")
	r.send("\r")
	r.modalReady("Model · New conversation")
	r.send("\x1b")
	r.waitText("[Create & send]")
	r.waitText(message)
	r.send("\t\t\t") // Optional Effort.
	r.waitText("[Effort]")
	r.send("\r")
	r.modalReady("Reasoning · New conversation")
	r.send("\x1b")
	r.waitText("[Create & send]")
	r.send("\t")
	r.waitText("[Cancel]")
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("local review/settings focus dispatched a mutation")
	}
	r.send("\r") // Explicit Cancel, never Create.
	waitWorkspaceFrame(t, r, func(v string) bool {
		return strings.Contains(v, "Initial Markdown for Alpha") && !strings.Contains(v, "╭─ New conversation")
	}, "launch canceled")
	r.waitEmptyComposer() // The command receipt clears only its own /new text.
	if state.Draft("env-a", "\x00new/project-launch/project-a") != message || session.Draft() != "" {
		t.Fatalf("review cancellation crossed launch/source draft slots: launch=%q source=%q", state.Draft("env-a", "\x00new/project-launch/project-a"), session.Draft())
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
