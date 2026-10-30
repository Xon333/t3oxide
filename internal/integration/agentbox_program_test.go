package integration_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
)

// These input sequences exercise Bubble Tea's decoder and production Update,
// client and Node adapter. They are non-TTY evidence, not physical-key tests.
func TestAgentBoxProgramModeReplyReviewUndoAndIndependentStop(t *testing.T) {
	f := newFixture(t)
	f.finishNativeThread("thread-a", "Review this completed result.")
	manager, state := newAppManager(t, f)
	a, err := manager.NewSession(f.profile.ID, "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	r, _ := startAppTUI(t, a)
	r.waitText("Review this completed result.")
	r.send("/mode agentbox\x1d")
	r.waitText("AgentBox")
	waitWorkspaceFrame(t, r, func(v string) bool { return strings.Contains(v, "Alpha") && !strings.Contains(v, "Loading…") }, "native inbox page")
	r.send("\r")
	r.waitText("1 Changes")
	r.send("r")
	waitProgramReply(t, r, "Alpha")
	const draft = "jknrelsz123\nexact second line"
	r.send("jknrelsz123\rexact second line")
	r.waitText("exact second line")
	if effects, _ := f.counts(); effects != 0 {
		t.Fatal("inbox hotkeys/Enter stole reply editing")
	}
	r.send("\x1b[98;7u") // Ctrl+Alt+B in the enhanced input protocol.
	waitWorkspaceFrame(t, r, func(v string) bool {
		return !strings.Contains(v, "AgentBox") && strings.Contains(v, "exact second line")
	}, "Developer mode with same draft")
	if a.Draft() != draft || a.Status().ThreadID != "thread-a" {
		t.Fatal("mode return changed source-bound draft/attachment")
	}
	r.send("\x1b[98;7u")
	r.waitText("AgentBox")
	waitProgramReply(t, r, "Alpha")
	if a.Draft() != draft {
		t.Fatal("AgentBox mode return did not retain reply")
	}
	subscriptions := f.requestCounts()["rpc:orchestration.subscribeThread"]
	f.corruptSockets()
	const reconnectedText = "Native result observed after reconnect."
	f.mu.Lock()
	items := f.threads["thread-a"]["visibleTurnItems"].([]any)
	item := items[len(items)-1].(object)["item"].(object)
	item["text"] = reconnectedText
	update := clone(item)
	f.mu.Unlock()
	f.broadcast("thread-a", "turn-item.updated", update, 0)
	waitWorkspaceFrame(t, r, func(v string) bool {
		return f.requestCounts()["rpc:orchestration.subscribeThread"] > subscriptions && a.Status().ConnectionState == "connected" && strings.Contains(v, reconnectedText) && strings.Contains(v, "AgentBox") && hasProgramReply(v, "Alpha") && !strings.Contains(v, "synchronizing")
	}, "AgentBox reconnect on the same native identity")
	if a.Draft() != draft || a.Status().ThreadID != "thread-a" {
		t.Fatal("native reconnect changed AgentBox reply identity/text")
	}
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("mode/reconnect automatically dispatched a mutation")
	}
	r.send("\x1d")
	f.waitMessage(t, draft)
	awaitStatus(t, a, func(s client.Status) bool { return s.Receipt != nil && !s.PendingAction && s.ActiveRunID != "" })
	r.waitEmptyComposer()
	started, release := f.holdNextSendAck()
	r.send("keep the exact next instruction\x1d")
	select {
	case <-started:
	case <-time.After(6 * time.Second):
		t.Fatal("reply did not reach the actual adapter")
	}
	r.send("\x13") // Independent Ctrl+S Stop while reply ACK is pending.
	waitCommandType(t, f, "run.interrupt")
	release()
	awaitStatus(t, a, func(s client.Status) bool { return !s.PendingAction && !s.PendingStop && s.ActiveRunID == "" })
	r.send("\x1b")
	waitWorkspaceFrame(t, r, func(v string) bool { return strings.Contains(v, "1 Changes") && !strings.Contains(v, "Loading…") }, "ready decision after reply and Stop")
	r.send("1")
	r.waitText("export.go")
	r.waitText("selected destination")
	r.send("\x1b")
	waitWorkspaceFrame(t, r, func(v string) bool { return !strings.Contains(v, "╭─ Changes") && strings.Contains(v, "1 Changes") }, "native changes dismissal")
	r.send("e")
	r.waitText("Mark reviewed")
	r.send("\x1b[B\r")
	waitCommandType(t, f, "thread.settle")
	waitWorkspaceFrame(t, r, func(v string) bool {
		return strings.Contains(v, "Reviewed acknowledged") && strings.Contains(v, "Next") && !strings.Contains(v, "Loading…")
	}, "native review acknowledgement")
	r.send("z")
	waitCommandType(t, f, "thread.unsettle")
	if state.Pending("env-a", true) != nil {
		t.Fatal("acknowledged Stop left its journal occupied")
	}
	quitIntuitiveProgram(t, r)
}

func TestAgentBoxProgramFilesRealIsolatedTaskFromNativeReview(t *testing.T) {
	f := newFixture(t)
	manager, state := newAppManager(t, f)
	const source = "original unsent source"
	if err := state.SetDraft("env-a", "thread-a", source); err != nil {
		t.Fatal(err)
	}
	a, err := manager.NewSession(f.profile.ID, "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	r, _ := startAppTUI(t, a)
	r.waitText("Initial Markdown for Alpha")
	r.send("\x1b[98;7u")
	r.waitText("AgentBox")
	r.send("n")
	r.waitText("Project")
	r.send("\r")
	r.waitText("First message")
	const brief = "  Investigate export handling\nkeep the selected path  "
	r.program.Send(tea.PasteMsg{Content: brief})
	r.waitText("keep the selected path")
	r.send("\t\x01\x0bNative task from inbox\r")
	r.waitText("[Create & send]")
	if effects, _ := f.counts(); effects != 0 {
		t.Fatal("reviewed title or brief launched without Create")
	}
	r.send("\r")
	waitWorkspaceFrame(t, r, func(v string) bool {
		return strings.Contains(v, "AgentBox") && strings.Contains(v, "Native task from inbox") && strings.Contains(v, "Working") && !strings.Contains(v, "Loading…")
	}, "created native task selected in inbox")
	f.mu.Lock()
	var launch object
	for _, command := range f.commands {
		if command["title"] == "Native task from inbox" {
			launch = clone(command)
		}
	}
	f.mu.Unlock()
	if launch == nil || launch["workspaceStrategy"].(object)["type"] != "worktree" || launch["initialMessage"].(object)["text"] != brief {
		t.Fatalf("file did not use reviewed native isolated launch: %+v", launch)
	}
	if state.Draft("env-a", "thread-a") != source {
		t.Fatal("filing lost prior source composer")
	}
	quitIntuitiveProgram(t, r)
}

func TestAgentBoxProgramHealthyHostUsableWhileOtherMetadataIsPending(t *testing.T) {
	first := newFixture(t)
	first.finishNativeThread("thread-a", "A healthy native outcome.")
	second := newEnvironmentFixture(t, "env-slow", "slow", "Slow host")
	release := second.holdNativeShell()
	state, err := config.OpenState(filepath.Join(t.TempDir(), "drafts.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	manager := client.NewManager([]config.Environment{first.profile, second.profile}, state)
	defer manager.Close()
	a, err := manager.NewSession(first.profile.ID, "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	r, _ := startAppTUI(t, a)
	r.waitText("A healthy native outcome.")
	r.send("/mode agentbox\x1d")
	waitWorkspaceFrame(t, r, func(v string) bool {
		return strings.Contains(v, "AgentBox") && strings.Contains(v, "Alpha") && strings.Contains(v, "1 loading")
	}, "independent healthy source before slow host")
	r.send("\r")
	r.waitText("1 Changes")
	r.send("r")
	waitProgramReply(t, r, "Alpha")
	r.send("use the healthy host now\x1d")
	fid := first.waitMessage(t, "use the healthy host now")
	if fid == "" {
		t.Fatal("pending unrelated host blocked healthy source action")
	}
	release()
	quitIntuitiveProgram(t, r)
}

func TestAgentBoxProgramFormalDecisionCannotBeResolvedByReplyOrStaleForm(t *testing.T) {
	f := newFixture(t)
	f.addRequests()
	manager, _ := newAppManager(t, f)
	a, err := manager.NewSession(f.profile.ID, "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	r, _ := startAppTUI(t, a)
	r.waitText("Initial Markdown for Alpha")
	dismissInitialAutomaticRequestAttention(t, r, f)
	r.send("/mode agentbox\x1d")
	waitWorkspaceFrame(t, r, func(v string) bool {
		return strings.Contains(v, "AgentBox") && strings.Contains(v, "Alpha") && !strings.Contains(v, "Loading…")
	}, "formal-request inbox")
	r.send("\r")
	r.waitText("1 Requests")
	r.send("1")
	r.waitText("Pending T3 requests")
	r.send("\r")
	r.waitText("T3 approval")
	if effects, _ := f.counts(); effects != 0 {
		t.Fatal("inbox number approved a request without submission")
	}
	// Another native client resolves the offered approval. Its old UI answer
	// must retire; the remaining question is still a separate native request.
	f.mu.Lock()
	f.threads["thread-a"]["runtimeRequests"].([]any)[0].(object)["status"] = "resolved"
	f.sequence++
	sequence := f.sequence
	f.threads["thread-a"]["thread"].(object)["updatedAt"] = f.nativeTimestamp()
	f.mu.Unlock()
	f.broadcast("thread-a", "runtime-request.updated", object{"id": "approval-a", "status": "resolved"}, sequence)
	awaitStatus(t, a, func(s client.Status) bool { return s.Requests == 1 })
	waitWorkspaceFrame(t, r, func(v string) bool {
		return !strings.Contains(v, "T3 approval") && !strings.Contains(v, "Loading…") && strings.Contains(v, "? Waiting · 1 request")
	}, "retired exact approval form and refreshed source")
	r.send("r")
	waitProgramReply(t, r, "Alpha")
	r.send("yes, continue with the plan\x1d")
	f.waitMessage(t, "yes, continue with the plan")
	f.mu.Lock()
	for _, command := range f.commands {
		if command["type"] == "runtime-request.respond" {
			t.Error("ordinary inbox reply became a formal approval")
		}
	}
	pending := f.threads["thread-a"]["runtimeRequests"].([]any)[1].(object)["status"]
	f.mu.Unlock()
	if pending != "pending" {
		t.Fatal("ordinary reply falsely resolved a formal question")
	}
	quitIntuitiveProgram(t, r)
}
