package integration_test

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Exercise the integrated screen through the real Bubble Tea event loop and
// adapter. Native keyboard parsing, actual rendered cell geometry, bounded
// metadata, historical pages and source-bound lazy inspection all participate.
func TestWorkspaceProgramRailAndToolClickPreserveAttachmentUntilOpen(t *testing.T) {
	f := newFixture(t)
	item := activityFixtureRow("thread-a", "tool-a", "v1")
	f.mu.Lock()
	f.threads["thread-a"]["visibleTurnItems"] = append(f.threads["thread-a"]["visibleTurnItems"].([]any), item)
	f.activityItems = map[string]object{"thread-a/tool-a": clone(item["item"].(object))}
	f.activityItems["thread-a/tool-a"]["output"] = "workspace fixture test output"
	f.activityItems["thread-a/tool-a"]["outputOmitted"] = false
	f.mu.Unlock()
	manager, state := newAppManager(t, f)
	const draft = "  exact source draft\nsecond line  "
	if err := state.SetDraft("env-a", "thread-a", draft); err != nil {
		t.Fatal(err)
	}
	session, err := manager.NewSession(f.profile.ID, "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	r, _ := startAppTUI(t, session)
	r.waitText("Initial Markdown for Alpha")
	r.program.Send(tea.WindowSizeMsg{Width: 120, Height: 35})
	waitWorkspaceFrame(t, r, func(view string) bool {
		lines := strings.Split(view, "\n")
		return len(lines) == 35 && ansi.StringWidth(lines[0]) == 120
	}, "120×35 workspace geometry")
	r.waitText("All environments ▾")
	r.waitText("Beta")
	r.waitText("> go")
	if f.requestCounts()["rpc:orchestration.getTurnItem"] != 0 {
		t.Fatal("visible activity fetched its output before inspection")
	}
	clickWorkspaceText(t, r, "> go")
	r.waitText("workspace fixture test output")
	r.waitText("go test ./...")
	if f.requestCounts()["rpc:orchestration.getTurnItem"] != 1 {
		t.Fatal("one source-bound click did not produce one lazy detail read")
	}
	if session.Draft() != draft || session.Status().ThreadID != "thread-a" {
		t.Fatal("activity inspection changed attachment or exact draft")
	}
	r.send("\x1b")
	waitWorkspaceFrame(t, r, func(view string) bool {
		return strings.Contains(view, "> go") && !strings.Contains(view, "workspace fixture test output")
	}, "activity inspector dismissal")
	r.send("\x10")
	r.modalReady("Commands")
	r.send("Earlier history\r")
	r.waitText("Earlier page for thread-a")
	before := f.requestCounts()
	r.send("\x0fBeta")
	waitNavigatorQuery(t, r, "Beta")
	if !strings.Contains(r.views.text(), "Earlier page for thread-a") || !session.Status().Historical || session.Status().ThreadID != "thread-a" || session.Draft() != draft {
		t.Fatal("filtering navigation replaced historical attachment or exact draft")
	}
	after := f.requestCounts()
	for _, key := range []string{"rpc:orchestration.subscribeThread", "/api/orchestration/threads/thread-a/bounded", "/api/orchestration/threads/thread-a/history", "/api/orchestration/shell", "rpc:orchestration.getTurnItem"} {
		if after[key] != before[key] {
			t.Fatalf("local navigation unexpectedly read %s: %d -> %d", key, before[key], after[key])
		}
	}
	// Only explicit activation changes the detailed attachment. Switching must
	// save the source text and leave the target with its own composer.
	r.send("\r")
	r.waitText("Initial Markdown for Beta")
	if state.Draft("env-a", "thread-a") != draft || state.Draft("env-a", "thread-b") != "" {
		t.Fatal("explicit navigation lost or rebound a composer draft")
	}
	if got := f.requestCounts()["rpc:orchestration.subscribeThread"]; got != before["rpc:orchestration.subscribeThread"]+1 {
		t.Fatalf("explicit navigation changed detailed subscriptions by %d", got-before["rpc:orchestration.subscribeThread"])
	}
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("browsing and tool inspection dispatched a mutation")
	}
	quitIntuitiveProgram(t, r)
}

func waitNavigatorQuery(t *testing.T, r *tuiProgram, query string) {
	t.Helper()
	waitWorkspaceFrame(t, r, func(view string) bool {
		for _, line := range strings.Split(view, "\n") {
			left, _, _ := strings.Cut(line, "│")
			if strings.TrimSpace(left) == query {
				return true
			}
		}
		return false
	}, "focused navigation query "+query)
}

func clickWorkspaceText(t *testing.T, r *tuiProgram, text string) {
	t.Helper()
	for y, line := range strings.Split(r.views.text(), "\n") {
		if index := strings.Index(line, text); index >= 0 {
			x := ansi.StringWidth(line[:index])
			r.program.Send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			return
		}
	}
	t.Fatalf("no rendered hit text %q", text)
}

func waitWorkspaceFrame(t *testing.T, r *tuiProgram, predicate func(string) bool, description string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if predicate(r.views.text()) {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("never rendered %s:\n%s", description, r.views.text())
}
