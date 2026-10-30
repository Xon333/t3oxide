package integration_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestIntuitiveBrowserMoreHelpAndRefreshPreserveActualAttachment(t *testing.T) {
	for _, size := range [][2]int{{60, 16}, {80, 24}, {120, 35}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) { intuitiveBrowserMoreProgram(t, size) })
	}
}

func intuitiveBrowserMoreProgram(t *testing.T, size [2]int) {
	t.Helper()
	f := newFixture(t)
	manager, state := newAppManager(t, f)
	const draft = "  source draft\nsecond line  "
	if err := state.SetDraft("env-a", "thread-a", draft); err != nil {
		t.Fatal(err)
	}
	session, err := manager.NewSession(f.profile.ID, "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	r, _ := startAppTUI(t, session)
	r.waitText("Initial Markdown for Alpha")
	r.program.Send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
	// Select the exact visible palette title, then use the integrated metadata
	// navigator at every width without changing the detailed attachment.
	r.send("\x10")
	r.modalReady("Commands")
	r.send("Conversations\r")
	waitNavigatorQuery(t, r, "Filter loaded")
	r.send("Beta")
	waitNavigatorQuery(t, r, "Beta")
	before := f.requestCounts()
	r.send("\x1ba") // Alt+A opens the current source-bound More menu.
	r.modalReady("More · Conversations")
	r.send("Refresh")
	r.waitText("Filter: Refresh")
	r.send("\x00") // Commands may inspect Help while retaining this menu.
	r.modalReady("Commands")
	r.send("help\r")
	r.waitText("Keyboard and commands")
	if view := r.views.text(); strings.Count(view, "Keyboard and commands") != 1 || strings.Count(view, "Ctrl+Y copy") != 1 {
		t.Fatalf("Help acquired duplicate or missing local detail controls:\n%s", view)
	}
	r.send("\x1b")
	r.modalReady("More · Conversations")
	r.waitText("Filter: Refresh")
	r.send("\r")
	waitNavigatorQuery(t, r, "Beta")
	if session.Draft() != draft || session.Status().ThreadID != "thread-a" {
		t.Fatal("More changed the active conversation or its exact draft")
	}
	if after := f.requestCounts(); after["rpc:orchestration.subscribeThread"] != before["rpc:orchestration.subscribeThread"] {
		t.Fatal("local More/Help/Refresh opened a detailed attachment")
	}
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("More/Help/Refresh dispatched a mutation")
	}
	r.send("\x0f")
	r.waitText("Initial Markdown for Alpha")
	if session.Draft() != draft {
		t.Fatal("Resume changed the exact source draft")
	}
	quitIntuitiveProgram(t, r)
}

func TestIntuitiveTitleEnterFocusesCreateBeforeOneExactLaunch(t *testing.T) {
	f := newFixture(t)
	manager, state := newAppManager(t, f)
	const sourceDraft = "  source stays here\nsecond line  "
	if err := state.SetDraft("env-a", "thread-a", sourceDraft); err != nil {
		t.Fatal(err)
	}
	session, err := manager.NewSession(f.profile.ID, "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	source := clone(f.threads["thread-a"]["thread"])
	f.mu.Unlock()
	r, _ := startAppTUI(t, session)
	r.waitText("Initial Markdown for Alpha")
	r.send("\t\x10") // Reading focus makes Ctrl+P a command action.
	r.modalReady("Commands")
	r.send("New conversation\r")
	r.modalReady("Project")
	r.send("\r")
	r.waitText("First message")
	const message = "  first message\nsecond line  "
	const title = "Literal /model title"
	r.program.Send(tea.PasteMsg{Content: message})
	r.waitText("second line")
	r.send("\t")
	r.send("\x01\x0b" + title + "\r") // Replace the prefilled title using its native editor.
	r.waitText("[Create & send]")
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("finishing the title dispatched before explicit Create")
	}
	r.send("\r")
	r.waitText("Initial Markdown for " + title)
	f.mu.Lock()
	var payload object
	for _, command := range f.commands {
		if command["initialMessage"] != nil {
			payload = clone(command)
		}
	}
	current := clone(f.threads["thread-a"]["thread"])
	f.mu.Unlock()
	if payload == nil || payload["title"] != title || payload["initialMessage"].(object)["text"] != message || !reflect.DeepEqual(payload["workspaceStrategy"], object{"type": "root"}) || payload["projectId"] != "project-a" {
		t.Fatalf("Create changed the exact payload/source binding: %+v", payload)
	}
	if !reflect.DeepEqual(source, current) || state.Draft("env-a", "thread-a") != sourceDraft {
		t.Fatal("Create changed the source settings or draft")
	}
	if effects, dispatches := f.counts(); effects != 1 || dispatches != 1 {
		t.Fatalf("Create produced %d effects/%d dispatches", effects, dispatches)
	}
	if state.Pending("env-a", false) != nil || state.Draft("env-a", "\x00new/project-launch/project-a") != "" {
		t.Fatal("launch acknowledgment retained its own pending action/draft")
	}
	quitIntuitiveProgram(t, r)
}

func TestIntuitiveHistoryKeepsVerifiedRequestsVisibleAndReviewable(t *testing.T) {
	for _, size := range [][2]int{{60, 16}, {80, 24}, {120, 35}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			f := newFixture(t)
			f.addRequests()
			manager, _ := newAppManager(t, f)
			session, err := manager.NewSession(f.profile.ID, "thread-a")
			if err != nil {
				t.Fatal(err)
			}
			r, _ := startAppTUI(t, session)
			r.waitText("Initial Markdown for Alpha")
			dismissInitialAutomaticRequestAttention(t, r, f)
			r.program.Send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			r.waitText("Awaiting input (2)")
			r.send("/earlier\x1d")
			r.waitText("Earlier page for thread-a")
			r.waitText("Awaiting input (2)")
			r.waitText("Ctrl+R requests")
			const draft = "historical draft\nsecond line"
			r.program.Send(tea.PasteMsg{Content: draft})
			r.waitText("second line")
			before := f.requestCounts()
			r.send("\x12")
			r.modalReady("Pending T3 requests")
			r.send("\r")
			r.modalReady("T3 approval")
			r.send("\x1b")
			r.waitText("Earlier page for thread-a")
			r.waitText("Ctrl+R requests")
			if view := r.views.text(); strings.Count(view, "Awaiting input (2)") != 1 {
				t.Fatalf("historical request count repeated or disappeared:\n%s", view)
			}
			if !session.Status().Historical || session.Status().Requests != 2 || session.Draft() != draft {
				t.Fatal("request review changed historical authority, offers or exact draft")
			}
			after := f.requestCounts()
			for _, key := range []string{"rpc:orchestration.subscribeThread", "/api/orchestration/threads/thread-a/bounded", "/api/orchestration/threads/thread-a/history"} {
				if after[key] != before[key] {
					t.Fatalf("local historical review refreshed the page: %s", key)
				}
			}
			if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
				t.Fatal("dismissing historical request review dispatched a mutation")
			}
			quitIntuitiveProgram(t, r)
		})
	}
}

func quitIntuitiveProgram(t *testing.T, r *tuiProgram) {
	t.Helper()
	r.send("\x11")
	select {
	case <-r.done:
		if r.err != nil {
			t.Fatal(r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("refinement program did not quit normally")
	}
	assertTerminalReset(t, r.output.raw())
}
