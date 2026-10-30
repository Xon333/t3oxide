package integration_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Xon333/t3oxide/internal/client"
)

// Native search matches message content, not just the titles in the loaded
// metadata page. Exercise the complete zero-local-result -> explicit server
// search -> visible source-bound row -> actual mouse attachment path.
func TestNavigatorProgramServerContentSearchShowsNativeMatchesAndOpensExactRow(t *testing.T) {
	for _, size := range intuitiveMouseSizes {
		for _, search := range []string{"enter", "mouse"} {
			t.Run(fmt.Sprintf("%dx%d/%s", size[0], size[1], search), func(t *testing.T) {
				f := newFixture(t)
				manager, state := newAppManager(t, f)
				draft := "  exact attached draft\nsecond line  "
				if err := state.SetDraft("env-a", "thread-a", draft); err != nil {
					t.Fatal(err)
				}
				session, err := manager.NewSession("fixture", "thread-a")
				if err != nil {
					t.Fatal(err)
				}
				r, _ := startAppTUI(t, session)
				r.waitText("Initial Markdown for Alpha")
				awaitStatus(t, session, func(s client.Status) bool { return s.Connected && s.Writable })
				sizeIntuitiveProgram(t, r, size, "developer")
				r.send("\x0f")
				waitNavigatorQuery(t, r, "Filter loaded")
				r.waitText("[FI◇] Beta")
				before := f.requestCounts()
				r.send("Markdown")
				waitNavigatorQuery(t, r, "Markdown")
				r.waitText("No loaded matches")
				counts := f.requestCounts()
				if counts["rpc:orchestration.searchThreads"] != 0 || counts["/api/orchestration/shell"] != before["/api/orchestration/shell"] {
					t.Fatal("typing a local filter performed remote reads", counts)
				}
				if search == "mouse" {
					clickIntuitiveControl(t, r, "Search server")
				} else {
					r.send("\r")
				}
				r.waitText("[FI◇] Alpha")
				r.waitText("[FI◇] Beta")
				waitNavigatorQuery(t, r, "Markdown")
				counts = f.requestCounts()
				if counts["rpc:orchestration.searchThreads"] != 1 || counts["rpc:orchestration.subscribeThread"] != before["rpc:orchestration.subscribeThread"] || counts["/api/orchestration/threads/thread-a/bounded"] != before["/api/orchestration/threads/thread-a/bounded"] || counts["/api/orchestration/threads/thread-b/bounded"] != 0 {
					t.Fatal("explicit search did not remain one native read without attachment/prefetch", counts)
				}
				if session.Status().ThreadID != "thread-a" || session.Draft() != draft {
					t.Fatal("server search changed the attached source or exact draft")
				}
				clickIntuitiveControl(t, r, "[FI◇] Beta")
				waitWorkspaceFrame(t, r, func(view string) bool {
					return strings.Contains(view, "Initial Markdown for Beta") && f.requestCounts()["rpc:orchestration.subscribeThread"] == before["rpc:orchestration.subscribeThread"]+1
				}, "exact target subscription after its eager bounded frame")
				counts = f.requestCounts()
				if counts["rpc:orchestration.subscribeThread"] != before["rpc:orchestration.subscribeThread"]+1 || state.Draft("env-a", "thread-a") != draft || state.Draft("env-a", "thread-b") != "" {
					t.Fatal("clicking the actual native search row lost its target or source-bound draft", counts)
				}
				if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
					t.Fatal("native search or row opening dispatched a mutation", effects, dispatches)
				}
				quitIntuitiveProgram(t, r)
			})
		}
	}
}

func TestAgentBoxProgramServerContentSearchShowsNativeMatchesAndOpensExactRow(t *testing.T) {
	for _, size := range intuitiveMouseSizes {
		for _, search := range []string{"enter", "mouse"} {
			t.Run(fmt.Sprintf("%dx%d/%s", size[0], size[1], search), func(t *testing.T) {
				f := newFixture(t)
				manager, state := newAppManager(t, f)
				draft := "  exact AgentBox source draft\nsecond line  "
				if err := state.SetDraft("env-a", "thread-a", draft); err != nil {
					t.Fatal(err)
				}
				session, err := manager.NewSession("fixture", "thread-a")
				if err != nil {
					t.Fatal(err)
				}
				r, _ := startAppTUI(t, session)
				r.waitText("Initial Markdown for Alpha")
				awaitStatus(t, session, func(s client.Status) bool { return s.Connected && s.Writable })
				// This existing helper performs the explicit mode change and
				// selects All without touching the attached human draft.
				sizeIntuitiveProgram(t, r, size, "agentbox")
				waitWorkspaceFrame(t, r, func(view string) bool {
					return strings.Contains(view, "AgentBox") && !strings.Contains(view, "Loading")
				}, "completed native inbox metadata")
				before := f.requestCounts()
				r.send("/Markdown")
				r.waitText("[Search server conversations]")
				counts := f.requestCounts()
				if counts["rpc:orchestration.searchThreads"] != 0 || counts["/api/orchestration/shell"] != before["/api/orchestration/shell"] || session.Draft() != draft {
					t.Fatal("AgentBox local query read remotely or changed the attached draft", counts)
				}
				if search == "mouse" {
					clickIntuitiveControl(t, r, "Search server conversations")
				} else {
					r.send("\r")
				}
				r.waitText("[FI◇] Alpha")
				r.waitText("[FI◇] Beta")
				r.waitText("1 searched · 1 incomplete")
				counts = f.requestCounts()
				if counts["rpc:orchestration.searchThreads"] != 1 || counts["rpc:orchestration.subscribeThread"] != before["rpc:orchestration.subscribeThread"] || counts["/api/orchestration/threads/thread-a/bounded"] != before["/api/orchestration/threads/thread-a/bounded"] || counts["/api/orchestration/threads/thread-b/bounded"] != 0 || session.Status().ThreadID != "thread-a" || session.Draft() != draft {
					t.Fatal("AgentBox search changed attachment, preloaded a transcript or lost exact read scope", counts)
				}
				clickIntuitiveControl(t, r, "[FI◇] Beta")
				waitWorkspaceFrame(t, r, func(view string) bool {
					return strings.Contains(view, "* [FI◇] Beta") && f.requestCounts()["rpc:orchestration.subscribeThread"] == before["rpc:orchestration.subscribeThread"]+1
				}, "exact AgentBox target subscription after its eager bounded frame")
				counts = f.requestCounts()
				if counts["rpc:orchestration.subscribeThread"] != before["rpc:orchestration.subscribeThread"]+1 || state.Draft("env-a", "thread-a") != draft || state.Draft("env-a", "thread-b") != "" {
					t.Fatal("AgentBox clicked native result did not attach its exact source", counts)
				}
				if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
					t.Fatal("AgentBox search/open acquired mutation authority", effects, dispatches)
				}
				quitIntuitiveProgram(t, r)
			})
		}
	}
}
