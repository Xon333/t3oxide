package integration_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
)

func nativeInboxThread(t *testing.T, a *client.Session, thread string) client.InboxThread {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	page, err := a.InboxRead(ctx, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range page.Sources {
		if source.Error != "" {
			t.Fatalf("inbox source failed: %s", source.Error)
		}
	}
	for _, row := range page.Rows {
		if row.Target.ThreadID == thread {
			return row
		}
	}
	t.Fatalf("native thread %s missing from bounded inbox", thread)
	return client.InboxThread{}
}

func fileNativeWork(a *client.Session, ctx context.Context, title string) <-chan featureResult {
	out := make(chan featureResult, 1)
	go func() { result, err := a.FileWork(ctx, title); out <- featureResult{result, err} }()
	return out
}

func TestAgentBoxActualNativeMetadataSourcesRemainLazyWithOfflineHost(t *testing.T) {
	first := newFixture(t)
	second := newEnvironmentFixture(t, "env-b", "second", "Fixture")
	offline := newEnvironmentFixture(t, "env-offline", "offline", "Offline")
	offline.server.Close()
	state, err := config.OpenState(filepath.Join(t.TempDir(), "drafts.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	manager := client.NewManager([]config.Environment{first.profile, second.profile, offline.profile}, state)
	defer manager.Close()
	a, err := manager.NewSession(first.profile.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	page, err := a.InboxRead(ctx, "", nil)
	if err != nil || len(page.Rows) != 4 || len(page.Sources) != 3 {
		t.Fatalf("partial inbox lost healthy hosts: %+v %v", page, err)
	}
	if page.Sources[2].Error == "" {
		t.Fatal("offline host became empty success")
	}
	for _, f := range []*fixture{first, second} {
		counts := f.requestCounts()
		if counts["/api/orchestration/shell"] != 1 || counts["/ws"] != 0 || counts["/api/orchestration/threads/thread-a/bounded"] != 0 {
			t.Fatalf("metadata opened a transcript: %+v", counts)
		}
	}
	var chosen client.InboxThread
	for _, row := range page.Rows {
		if row.Target.ProfileID == second.profile.ID && row.Target.ThreadID == "thread-a" {
			chosen = row
		}
	}
	if chosen.Target.EnvironmentID != "env-b" || chosen.Target.Revision == "" {
		t.Fatal("source identity/revision was not retained")
	}
	next, err := a.InboxOpen(page, chosen.Target.Key())
	if err != nil {
		t.Fatal(err)
	}
	if second.requestCounts()["/ws"] != 0 {
		t.Fatal("lazy open target subscribed before attachment")
	}
	stream := streamSession(t, next)
	stream.until(t, func(e client.Event) bool { return e.Snapshot != nil && e.Snapshot.Metadata.Live })
	if next.Status().EnvironmentID != "env-b" || next.Status().ThreadID != "thread-a" {
		t.Fatal("duplicate display labels retargeted attachment")
	}
	if first.requestCounts()["/ws"] != 0 {
		t.Fatal("unselected source acquired detailed attachment")
	}
}

func TestAgentBoxActualNativeFileReplyChangesReviewAndStaleUndo(t *testing.T) {
	f := newFixture(t)
	_, state, a, oldStream := startFeatureSession(t, f, "thread-a")
	const sourceDraft = "  exact source draft\nnever inherited  "
	if err := a.SetDraft(sourceDraft); err != nil {
		t.Fatal(err)
	}
	file := fileNativeWork(a, context.Background(), "Review export flow")
	p := nativeLaunchReview(t, a)
	if p.Question.LaunchReview.Selection.Model != "fixture-model" || !strings.HasPrefix(p.Question.LaunchReview.Location.Workspace, "New worktree from main") {
		t.Fatalf("File did not resolve native isolated defaults: %+v", p.Question.LaunchReview)
	}
	input := client.LaunchInput{Title: "Review export flow", Message: ""}
	p.Respond(client.Answer{Action: "create", Launch: &input})
	result := featureReply(t, file)
	if result.err != nil || result.result.Next == nil {
		t.Fatalf("native file failed: %v", result.err)
	}
	if state.Draft("env-a", "thread-a") != sourceDraft {
		t.Fatal("file lost original source draft")
	}
	f.mu.Lock()
	var thread, operation string
	for id, command := range f.commands {
		if command["title"] == input.Title {
			operation = id
			thread, _ = command["threadId"].(string)
			if command["initialMessage"] != nil {
				t.Error("empty filing started work")
			}
		}
	}
	filed := clone(f.threads[thread])
	f.mu.Unlock()
	if thread == "" || thread == operation || len(filed["runs"].([]any)) != 0 || len(filed["visibleTurnItems"].([]any)) != 0 {
		t.Fatal("filing fabricated execution or conflated IDs")
	}
	oldStream.stop(t)
	a = result.result.Next
	stream := streamSession(t, a)
	stream.until(t, func(e client.Event) bool { return e.Snapshot != nil && e.Snapshot.Metadata.Live })
	if a.Status().ActiveRunID != "" {
		t.Fatal("empty filed thread claimed an active run")
	}
	const reply = "  Investigate the selected destination\nkeep this exact text  "
	if err := a.StoreDraft(reply); err != nil {
		t.Fatal(err)
	}
	if err := a.Send(context.Background(), reply); err != nil {
		t.Fatal(err)
	}
	f.waitMessage(t, reply)
	stream.until(t, func(e client.Event) bool { return e.Status.ActiveRunID != "" })
	f.finishNativeThread(thread, "The export flow now preserves the selected destination.")
	stream.until(t, func(e client.Event) bool {
		return e.Snapshot != nil && e.Snapshot.Metadata.Live && e.Status.ActiveRunID == ""
	})
	row := nativeInboxThread(t, a, thread)
	if client.InboxBucket(row, time.Now()) != "needs" {
		t.Fatal("native completion did not return to needs-you")
	}
	review, err := a.ThreadReview(context.Background(), row.Target)
	if err != nil || !strings.Contains(review.Diff, "+selected destination") || len(review.Files) != 1 || review.Workspace != filed["thread"].(object)["worktreePath"] {
		t.Fatalf("real native review failed: %+v %v", review, err)
	}
	receipt, err := a.AgentBoxAction(context.Background(), row.Target, "settle", "")
	if err != nil || receipt.NativeRevision == "" {
		t.Fatalf("native review disposition failed: %+v %v", receipt, err)
	}
	post := nativeInboxThread(t, a, thread)
	if client.InboxBucket(post, time.Now()) != "done" || post.Target.Revision != receipt.NativeRevision {
		t.Fatal("review marker is not native durable state")
	}
	// A second actual adapter modifies the same native identity. Stale Undo
	// must fail before dispatch; its old post-action revision is not authority.
	other := f.start(t)
	request(t, other, "thread-action", object{"threadId": thread, "action": "pin", "expectedRevision": post.Target.Revision}, wire.NewID())
	_, before := f.counts()
	if _, err = a.AgentBoxAction(context.Background(), post.Target, "unsettle", ""); err == nil {
		t.Fatal("stale multi-client undo reopened a changed item")
	}
	_, after := f.counts()
	if after != before {
		t.Fatal("stale review inverse reached the backend")
	}
	fresh := nativeInboxThread(t, a, thread)
	if _, err = a.AgentBoxAction(context.Background(), fresh.Target, "unsettle", ""); err != nil {
		t.Fatal(err)
	}
	fresh = nativeInboxThread(t, a, thread)
	until := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	if _, err = a.AgentBoxAction(context.Background(), fresh.Target, "snooze", until); err != nil {
		t.Fatal(err)
	}
	fresh = nativeInboxThread(t, a, thread)
	if client.InboxBucket(fresh, time.Now()) != "later" || fresh.Thread.Inbox.SnoozedUntil != until {
		t.Fatal("Later is not exact native state")
	}
	if _, err = a.AgentBoxAction(context.Background(), fresh.Target, "unsnooze", ""); err != nil {
		t.Fatal(err)
	}
}

func TestAgentBoxLostNativeLaunchReconcilesOriginalIdentityAndStopIsIndependent(t *testing.T) {
	f := newFixture(t)
	_, state, a, _ := startFeatureSession(t, f, "thread-a")
	started, release := f.holdNextLaunchAck()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	file := fileNativeWork(a, ctx, "Lost receipt task")
	p := nativeLaunchReview(t, a)
	input := client.LaunchInput{Title: "Lost receipt task", Message: "Run this native task"}
	p.Respond(client.Answer{Action: "create", Launch: &input})
	var operation string
	select {
	case operation = <-started:
	case <-time.After(6 * time.Second):
		t.Fatal("native launch was not dispatched")
	}
	cancel()
	result := featureReply(t, file)
	if result.err == nil || result.result.Next != nil {
		t.Fatal("lost native launch response became success")
	}
	pending := state.Pending("env-a", false)
	thread, _ := pending["newThreadId"].(string)
	if pending["id"] != operation || thread == "" || thread == operation || pending["message"] != input.Message {
		t.Fatalf("launch recovery lost exact identities/payload: %+v", pending)
	}
	if err := a.Stop(context.Background()); err != nil {
		t.Fatalf("saved launch blocked independent Stop: %v", err)
	}
	release()
	recovered, err := a.RetryLaunch(context.Background())
	if err != nil || recovered.Next == nil {
		t.Fatalf("same-ID native reconciliation failed: %v", err)
	}
	defer recovered.Next.Close()
	if state.Pending("env-a", false) != nil {
		t.Fatal("reconciled receipt did not retire its own journal")
	}
	f.mu.Lock()
	launches := 0
	for _, id := range f.dispatches {
		if id == operation {
			launches++
		}
	}
	run := clone(f.threads[thread]["runs"].([]any)[0].(object))
	stopped := f.threads["thread-a"]["runs"].([]any)[0].(object)["status"]
	f.mu.Unlock()
	if launches != 1 || stopped != "interrupted" || run["status"] != "running" {
		t.Fatalf("recovery duplicated launch or Stop retargeted task: launches=%d stopped=%v run=%+v", launches, stopped, run)
	}
	a.Close()
	f.mu.Lock()
	stillRunning := f.threads[thread]["runs"].([]any)[0].(object)["status"]
	f.mu.Unlock()
	if stillRunning != "running" {
		t.Fatal("detaching original UI stopped native backend work")
	}
}

func TestAgentBoxBatchFilesSeparateNativeWorktreesAndExplicitSharedRoot(t *testing.T) {
	f := newFixture(t)
	_, state, a, _ := startFeatureSession(t, f, "thread-a")
	out := make(chan featureResult, 1)
	go func() { r, e := a.FileMany(context.Background()); out <- featureResult{r, e} }()
	p := featurePrompt(t, a)
	if p.Question.Title != "File multiple tasks" || p.Question.StoreText == nil {
		t.Fatal("batch has no explicit human draft")
	}
	const list = "  First isolated task  \nSecond isolated task\nShared root by explicit choice\n"
	p.Respond(client.Answer{Text: list})
	for i := 0; i < 3; i++ {
		p = nativeLaunchReview(t, a)
		if !strings.HasPrefix(p.Question.LaunchReview.Location.Workspace, "New worktree from main") {
			t.Fatal("task inherited source workspace")
		}
		input := p.Question.LaunchReview.Input
		if i == 2 {
			p.Respond(client.Answer{Action: "workspace", Launch: &input})
			p = featurePrompt(t, a)
			p.Respond(client.Answer{ChoiceIDs: []string{"local"}})
			p = featurePrompt(t, a)
			if p.Question.LaunchReview.Location.Workspace != "/fixture/project-root" {
				t.Fatal("explicit shared root was not reviewed")
			}
		}
		p.Respond(client.Answer{Action: "create", Launch: &input})
	}
	result := featureReply(t, out)
	if result.err != nil || result.result.Next == nil {
		t.Fatal(result.err)
	}
	defer result.result.Next.Close()
	f.mu.Lock()
	var worktrees []string
	rootCount := 0
	for _, command := range f.commands {
		if command["title"] == nil {
			continue
		}
		thread := command["threadId"].(string)
		if command["commandId"] == thread {
			t.Error("batch conflated native operation/thread IDs")
		}
		if command["workspaceStrategy"].(object)["type"] == "root" {
			rootCount++
		} else {
			worktrees = append(worktrees, f.threads[thread]["thread"].(object)["worktreePath"].(string))
		}
	}
	f.mu.Unlock()
	if len(worktrees) != 2 || worktrees[0] == worktrees[1] || rootCount != 1 {
		t.Fatalf("batch did not produce isolated native identities plus explicit root: %v %d", worktrees, rootCount)
	}
	source := "\x00file-many/" + config.InboxIdentity("env-a", "project-a", "thread-a")
	if state.Draft("env-a", source) != "" || state.Pending("env-a", false) != nil {
		t.Fatal("acknowledged batch retained consumed task/recovery")
	}
}

func TestAgentBoxBatchLostLaunchRecoveryConsumesOnlyOneTaskAndPauses(t *testing.T) {
	f := newFixture(t)
	_, state, a, _ := startFeatureSession(t, f, "thread-a")
	started, release := f.holdNextLaunchAck()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan featureResult, 1)
	go func() { r, e := a.FileMany(ctx); out <- featureResult{r, e} }()
	p := featurePrompt(t, a)
	const list = "Task before lost receipt\nTask still unlaunched\n"
	p.Respond(client.Answer{Text: list})
	p = nativeLaunchReview(t, a)
	input := p.Question.LaunchReview.Input
	p.Respond(client.Answer{Action: "create", Launch: &input})
	select {
	case <-started:
	case <-time.After(6 * time.Second):
		t.Fatal("batch native launch did not reach backend")
	}
	cancel()
	if result := featureReply(t, out); result.err == nil {
		t.Fatal("lost batch receipt became success")
	}
	source := "\x00file-many/" + config.InboxIdentity("env-a", "project-a", "thread-a")
	if state.Draft("env-a", source) != list {
		t.Fatal("uncertain launch consumed human task")
	}
	release()
	recovered, err := a.RetryLaunch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Next != nil {
		defer recovered.Next.Close()
	}
	if state.Draft("env-a", source) != "Task still unlaunched\n" {
		t.Fatal("exact launch reconciliation did not consume one prefix atomically")
	}
	if effects, dispatches := f.counts(); effects != 1 || dispatches != 1 {
		t.Fatalf("paused batch dispatched unreviewed task or duplicated launch: %d/%d", effects, dispatches)
	}
}
