package integration_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
)

// These helpers return actual offered IDs; fixture thread identities are only
// decoded in the test driver, never in product label routing.
func offeredThreadValue(t *testing.T, id string) string {
	t.Helper()
	var pair []string
	if json.Unmarshal([]byte(id), &pair) == nil && len(pair) == 2 {
		return pair[1]
	}
	return id
}
func offeredThreadID(t *testing.T, p *client.Prompt, thread string) string {
	t.Helper()
	for _, choice := range p.Question.Choices {
		if offeredThreadValue(t, choice.ID) == thread {
			return choice.ID
		}
	}
	t.Fatalf("thread %s was not offered: %+v", thread, p.Question.Choices)
	return ""
}

// Native New deliberately chooses an existing project before showing its
// reviewed form. A selected/attached thread is never used as a settings source.
func nativeLaunchReview(t *testing.T, a *client.Session) *client.Prompt {
	t.Helper()
	p := featurePrompt(t, a)
	if p.Question.Title != "Project" {
		t.Fatalf("New did not offer the native project list: %+v", p.Question)
	}
	found := false
	for _, choice := range p.Question.Choices {
		found = found || choice.ID == "project-a"
	}
	if !found {
		t.Fatalf("existing project with zero or more threads was not offered: %+v", p.Question)
	}
	p.Respond(client.Answer{ChoiceIDs: []string{"project-a"}})
	p = featurePrompt(t, a)
	if p.Question.Surface != client.SurfaceLaunch {
		t.Fatalf("native defaults did not produce the launch form: %+v", p.Question)
	}
	return p
}
func TestRefinementBrowserMultiEnvironmentIdentityLazyReadsAndResume(t *testing.T) {
	first := newFixture(t)
	second := newEnvironmentFixture(t, "env-b", "second", "Fixture")
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
	stream := streamSession(t, a)
	stream.until(t, func(e client.Event) bool { return e.Snapshot != nil && e.Snapshot.Metadata.Live })
	_ = a.SetDraft("  exact\nsource draft  ")
	before := first.requestCounts()
	if before["/api/orchestration/shell"] != 0 {
		t.Fatal("explicit thread performed redundant metadata list")
	}
	browse := featureCommand(a, "threads", "")
	p := featurePrompt(t, a)
	if p.Question.Surface != client.SurfaceBrowser || len(p.Question.BrowserPage.ProfileIDs) != 2 || len(p.Question.BrowserPage.Profiles) != 2 {
		t.Fatalf("missing real scope %+v", p.Question)
	}
	oldID := offeredThreadID(t, p, "thread-a")
	p.Respond(client.Answer{Action: "environments"})
	p = featurePrompt(t, a)
	if p.Question.Kind != client.ChooseMany {
		t.Fatal("environment scope is single selection")
	}
	p.Respond(client.Answer{Action: "continue", ChoiceIDs: []string{first.profile.ID, second.profile.ID}})
	p = featurePrompt(t, a)
	p.Respond(client.Answer{Action: "profile:" + second.profile.ID})
	p = featurePrompt(t, a)
	if p.Question.BrowserPage.ProfileID != second.profile.ID || oldID == offeredThreadID(t, p, "thread-a") {
		t.Fatal("duplicate labels/thread IDs lost profile identity")
	}
	secondCounts := second.requestCounts()
	if secondCounts["rpc:orchestration.subscribeThread"] != 0 {
		t.Fatal("browsing subscribed to detail")
	}
	for path, n := range secondCounts {
		if n > 0 && strings.Contains(path, "/threads/") {
			t.Fatalf("metadata helper read transcript: %s", path)
		}
	}
	second.mu.Lock()
	sockets := len(second.sockets)
	second.mu.Unlock()
	if sockets != 0 {
		t.Fatalf("temporary helper retained %d sockets", sockets)
	}
	first.update("thread-a", "Still active while browsing", 0)
	stream.until(t, func(e client.Event) bool {
		return containsMessage(e, "thread-a/first-thread-a", "Still active while browsing")
	})
	p.Respond(client.Answer{Action: "resume"})
	r := featureReply(t, browse)
	if r.err != nil || r.result.Next != nil || a.Status().ProfileID != first.profile.ID || a.Draft() != "  exact\nsource draft  " {
		t.Fatalf("resume changed attachment %+v %v", r.result, r.err)
	}
	// Scope persists in its manager, while all configured IDs stay stable.
	browse = featureCommand(a, "threads", "")
	p = featurePrompt(t, a)
	if !reflect.DeepEqual(p.Question.BrowserPage.ProfileIDs, []string{first.profile.ID, second.profile.ID}) || len(a.Status().ProfileIDs) != 2 {
		t.Fatal("scope was UI-only or changed theme identity")
	}
	p.Respond(client.Answer{Action: "profile:" + second.profile.ID})
	p = featurePrompt(t, a)
	p.Respond(client.Answer{ChoiceIDs: []string{oldID}})
	if r = featureReply(t, browse); r.err == nil || r.result.Next != nil {
		t.Fatal("stale profile choice retargeted duplicate thread")
	}
}
func TestRefinementLaunchReviewLocalSettingsExactBytesAndSingleReceipt(t *testing.T) {
	f := newFixture(t)
	_, state, a, _ := startFeatureSession(t, f, "thread-a")
	_ = a.SetDraft(" exact source composer\n")
	featureOK(t, a, "model", "next fixture-other")
	override := state.Override("env-a", "thread-a")
	f.mu.Lock()
	source := clone(f.threads["thread-a"]["thread"])
	f.catalog["providers"].([]any)[0].(object)["requiresNewThreadForModelChange"] = true
	f.mu.Unlock()
	launch := featureCommand(a, "new", "")
	p := nativeLaunchReview(t, a)
	if p.Question.Surface != client.SurfaceLaunch || p.Question.LaunchReview.Input.Title != "New conversation" || p.Question.LaunchReview.Location.Workspace != "/fixture/project-root" {
		t.Fatalf("missing compact exact review: %+v", p.Question)
	}
	input := client.LaunchInput{Title: "  Human title  ", Message: "  first message\n\nexact  "}
	p.Respond(client.Answer{Action: "model", Launch: &input})
	p = featurePrompt(t, a)
	p.Respond(client.Answer{ChoiceIDs: []string{"fixture-other"}})
	p = featurePrompt(t, a)
	if p.Question.LaunchReview.Input != input || p.Question.LaunchReview.Selection.Model != "fixture-other" {
		t.Fatal("settings roundtrip lost form")
	}
	p.Respond(client.Answer{Action: "effort", Launch: &input})
	p = featurePrompt(t, a)
	p.Respond(client.Answer{ChoiceIDs: []string{"value:high"}})
	p = featurePrompt(t, a)
	p.Respond(client.Answer{Action: "tier", Launch: &input})
	p = featurePrompt(t, a)
	p.Respond(client.Answer{ChoiceIDs: []string{"value:fast"}})
	p = featurePrompt(t, a)
	if got := p.Question.LaunchReview.Selection; got.Model != "fixture-other" {
		t.Fatal(got)
	}
	p.Respond(client.Answer{Action: "create", Launch: &input})
	r := featureReply(t, launch)
	if r.err != nil || r.result.Next == nil {
		t.Fatalf("launch failed %v", r.err)
	}
	defer r.result.Next.Close()
	f.mu.Lock()
	var payload object
	for _, c := range f.commands {
		if c["type"] == "thread.model-selection.set" {
			t.Error("launch mutated source selection")
		}
	}
	for _, c := range f.commands {
		if c["initialMessage"] != nil {
			payload = clone(c)
		}
	}
	current := clone(f.threads["thread-a"]["thread"])
	f.mu.Unlock()
	if !reflect.DeepEqual(source, current) || a.Draft() != " exact source composer\n" || !reflect.DeepEqual(state.Override("env-a", "thread-a"), override) {
		t.Fatal("launch changed source settings/draft/override")
	}
	if payload == nil || payload["title"] != input.Title || payload["initialMessage"].(object)["text"] != input.Message || payload["workspaceStrategy"].(object)["type"] != "root" || payload["workspaceStrategy"].(object)["worktreePath"] != nil {
		t.Fatalf("wrong launch payload: %+v", payload)
	}
	selection := payload["modelSelection"].(object)
	if selection["model"] != "fixture-other" {
		t.Fatal(selection)
	}
	if e, d := f.counts(); e != 1 || d != 1 {
		t.Fatalf("launch effects %d dispatches %d", e, d)
	}
	if state.Pending("env-a", false) != nil || state.Draft("env-a", "\x00new/project-launch/project-a") != "" {
		t.Fatal("exact launch receipt did not clear its own journal/draft")
	}
}
func TestRefinementLaunchCancelRetainsSeparateDraftAndRejectsUnofferedAction(t *testing.T) {
	f := newFixture(t)
	_, state, a, _ := startFeatureSession(t, f, "thread-a")
	_ = a.SetDraft("source")
	input := client.LaunchInput{Title: "title", Message: " exact first\nmessage "}
	launch := featureCommand(a, "new", "")
	p := nativeLaunchReview(t, a)
	p.Respond(client.Answer{Action: "model", Launch: &input})
	p = featurePrompt(t, a)
	p.Respond(client.Answer{Cancelled: true})
	p = featurePrompt(t, a)
	if p.Question.LaunchReview.Input != input {
		t.Fatal("cancelled local setting lost editors")
	}
	p.Respond(client.Answer{Cancelled: true, Launch: &input})
	r := featureReply(t, launch)
	if r.err != nil || r.result.Next != nil {
		t.Fatal(r.err)
	}
	if state.Draft("env-a", "\x00new/project-launch/project-a") != input.Message || a.Draft() != "source" {
		t.Fatal("cancel draft slots crossed")
	}
	launch = featureCommand(a, "new", "")
	p = nativeLaunchReview(t, a)
	if p.Question.LaunchReview.Input.Message != input.Message {
		t.Fatal("reopen lost first message")
	}
	p.Respond(client.Answer{Action: "fabricated", Launch: &input})
	if r = featureReply(t, launch); r.err == nil {
		t.Fatal("unoffered launch action accepted")
	}
	if e, d := f.counts(); e != 0 || d != 0 {
		t.Fatal("cancelled launch caused effects")
	}
}

func TestRefinementStartupListsMetadataThenOpensOnlyExactOfferedThread(t *testing.T) {
	f := newFixture(t)
	m, _ := newAppManager(t, f)
	a, err := m.NewSession(f.profile.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	stream := streamSession(t, a)
	stream.until(t, func(e client.Event) bool { return e.Status.ConnectionState == "browse" })
	select {
	case p := <-a.Prompts():
		t.Fatalf("metadata startup opened an unsolicited prompt: %+v", p.Question)
	default:
	}
	page, err := a.NavigatorRead(context.Background(), f.profile.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	counts := f.requestCounts()
	if counts["rpc:orchestration.subscribeThread"] != 0 || counts["/api/orchestration/threads/thread-a/bounded"] != 0 {
		t.Fatal("startup eagerly watched a highlighted row")
	}
	f.mu.Lock()
	sockets := len(f.sockets)
	f.mu.Unlock()
	if sockets != 0 {
		t.Fatal("startup metadata helper was not closed")
	}
	if _, err := a.NavigatorOpen(page, "not-offered"); err == nil {
		t.Fatal("unoffered metadata target accepted")
	}
	next, err := a.NavigatorOpen(page, "thread-b")
	if err != nil {
		t.Fatal(err)
	}
	if a.Status().ThreadID != "" || f.requestCounts()["rpc:orchestration.subscribeThread"] != 0 {
		t.Fatal("lazy target changed startup attachment before explicit listen")
	}
	selected := streamSession(t, next)
	selected.until(t, func(e client.Event) bool {
		return e.Status.ThreadID == "thread-b" && e.Snapshot != nil && e.Snapshot.Metadata.Live
	})
	counts = f.requestCounts()
	if counts["/api/orchestration/shell"] != 1 || counts["rpc:orchestration.subscribeThread"] != 1 || counts["/api/orchestration/threads/thread-a/bounded"] != 0 {
		t.Fatalf("opening exact thread added redundant reads: %+v", counts)
	}
}
func TestRefinementBrowserUnavailableTabRemainsScopedAndCancelKeepsActive(t *testing.T) {
	first := newFixture(t)
	second := newEnvironmentFixture(t, "env-b", "second", "Fixture")
	bad := second.profile
	bad.EnvironmentID = "expected-other-identity"
	state, err := config.OpenState(filepath.Join(t.TempDir(), "drafts.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	m := client.NewManager([]config.Environment{first.profile, bad}, state)
	defer m.Close()
	a, err := m.NewSession(first.profile.ID, "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	stream := streamSession(t, a)
	stream.until(t, func(e client.Event) bool { return e.Snapshot != nil && e.Snapshot.Metadata.Live })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan featureResult, 1)
	go func() { result, err := a.Command(ctx, "threads", ""); done <- featureResult{result, err} }()
	p := featurePrompt(t, a)
	p.Respond(client.Answer{Action: "environments"})
	p = featurePrompt(t, a)
	p.Respond(client.Answer{Action: "continue", ChoiceIDs: []string{first.profile.ID, bad.ID}})
	p = featurePrompt(t, a)
	p.Respond(client.Answer{Action: "profile:" + bad.ID})
	p = featurePrompt(t, a)
	if p.Question.BrowserPage.ProfileID != bad.ID || len(p.Question.BrowserPage.ProfileIDs) != 2 || p.Question.BrowserPage.Error == "" || len(p.Question.Choices) != 0 {
		t.Fatal("failed tab disappeared or silently retargeted")
	}
	cancel()
	r := featureReply(t, done)
	if r.err == nil || r.result.Next != nil || p.Respond(client.Answer{ChoiceIDs: []string{"stale"}}) {
		t.Fatal("cancelled browser accepted a late selection")
	}
	if a.Status().ProfileID != first.profile.ID {
		t.Fatal("failure detached active session")
	}
	second.mu.Lock()
	sockets := len(second.sockets)
	second.mu.Unlock()
	if sockets != 0 {
		t.Fatal("failed helper retained upstream socket")
	}
	if second.requestCounts()["rpc:orchestration.subscribeThread"] != 0 {
		t.Fatal("failed tab watched detail")
	}
}
func TestRefinementLaunchRejectsFreshlyRemovedCatalogSelection(t *testing.T) {
	f := newFixture(t)
	_, state, a, _ := startFeatureSession(t, f, "thread-a")
	launch := featureCommand(a, "new", "")
	p := nativeLaunchReview(t, a)
	input := client.LaunchInput{Title: "title", Message: "preserve me"}
	p.Respond(client.Answer{Action: "model", Launch: &input})
	p = featurePrompt(t, a)
	p.Respond(client.Answer{ChoiceIDs: []string{"fixture-other"}})
	p = featurePrompt(t, a)
	f.mu.Lock()
	provider := f.catalog["providers"].([]any)[0].(object)
	models := provider["models"].([]any)
	kept := []any{}
	for _, raw := range models {
		if raw.(object)["slug"] != "fixture-other" {
			kept = append(kept, raw)
		}
	}
	provider["models"] = kept
	f.mu.Unlock()
	p.Respond(client.Answer{Action: "create", Launch: &input})
	r := featureReply(t, launch)
	if r.err == nil || r.result.Next != nil {
		t.Fatal("stale launch catalog selection was dispatched")
	}
	if e, d := f.counts(); e != 0 || d != 0 || state.Pending("env-a", false) != nil {
		t.Fatal("local stale validation created operation")
	}
	if state.Draft("env-a", "\x00new/project-launch/project-a") != input.Message {
		t.Fatal("stale selection lost launch draft")
	}
}
func TestRefinementBrowserNewResolvesNativeDefaultsWithoutSelectedSourceWorkspace(t *testing.T) {
	f := newFixture(t)
	f.mu.Lock()
	f.threads["thread-b"]["thread"].(object)["worktreePath"] = "/fixture/selected-checkout"
	f.threads["thread-b"]["thread"].(object)["branch"] = "selected-branch"
	f.mu.Unlock()
	_, state, a, _ := startFeatureSession(t, f, "thread-a")
	_ = a.SetDraft("active composer unchanged")
	browse := featureCommand(a, "threads", "")
	p := featurePrompt(t, a)
	p.Respond(client.Answer{Action: "new", ChoiceIDs: []string{offeredThreadID(t, p, "thread-b")}})
	p = nativeLaunchReview(t, a)
	if p.Question.LaunchReview.Location.ThreadID != "" || p.Question.LaunchReview.Location.Workspace != "/fixture/project-root" {
		t.Fatalf("New inherited an arbitrary source: %+v", p.Question)
	}
	input := client.LaunchInput{Title: "new with native project defaults", Message: "first message"}
	p.Respond(client.Answer{Action: "create", Launch: &input})
	r := featureReply(t, browse)
	if r.err != nil || r.result.Next == nil {
		t.Fatal(r.err)
	}
	defer r.result.Next.Close()
	f.mu.Lock()
	var payload object
	for _, c := range f.commands {
		if c["initialMessage"] != nil {
			payload = clone(c)
		}
	}
	f.mu.Unlock()
	if payload == nil || payload["workspaceStrategy"].(object)["type"] != "root" || payload["workspaceStrategy"].(object)["branch"] != nil || payload["projectId"] != "project-a" {
		t.Fatalf("wrong native project binding %+v", payload)
	}
	if a.Status().ThreadID != "thread-a" || a.Draft() != "active composer unchanged" || state.Draft("env-a", "thread-a") != "active composer unchanged" {
		t.Fatal("new preparation detached or changed active draft")
	}
}

func TestRefinementResponseAndCompletionBeforeAckCannotReviveReceipt(t *testing.T) {
	f := newFixture(t)
	_, state, a, stream := startFeatureSession(t, f, "thread-a")
	started, release := f.holdNextSendAck()
	defer release()
	_ = a.SetDraft(" exact sent message ")
	done := make(chan error, 1)
	go func() { done <- a.Send(context.Background(), " exact sent message ") }()
	var id string
	select {
	case id = <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("send did not reach held receipt")
	}
	f.mu.Lock()
	run := clone(f.threads["thread-a"]["runs"].([]any)[0].(object))
	f.mu.Unlock()
	f.broadcast("thread-a", "run.updated", run, 0)
	event := stream.until(t, func(e client.Event) bool {
		return e.Status.Receipt != nil && e.Status.Receipt.OperationID == id && e.Status.Receipt.Phase == "responding"
	})
	if event.Status.Receipt.RunID != "run-thread-a" || state.Pending("env-a", false)["id"] != id {
		t.Fatal("response cleared uncertainty or lost causal run")
	}
	_ = a.SetDraft("newer draft survives late ACK")
	f.mu.Lock()
	f.threads["thread-a"]["runs"].([]any)[0].(object)["status"] = "completed"
	run = clone(f.threads["thread-a"]["runs"].([]any)[0].(object))
	f.mu.Unlock()
	f.broadcast("thread-a", "run.updated", run, 0)
	stream.until(t, func(e client.Event) bool { return e.Status.Receipt != nil && e.Status.Receipt.Phase == "completed" })
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("released ACK did not finish send")
	}
	if got := a.Status().Receipt; got == nil || got.Phase != "completed" || got.Sequence < 0 || got.RunID != "run-thread-a" {
		t.Fatalf("late ACK revived banner %+v", got)
	}
	if state.Pending("env-a", false) != nil || a.Draft() != "newer draft survives late ACK" {
		t.Fatal("receipt changed newer draft or retained accepted operation")
	}
	if e, d := f.counts(); e != 1 || d != 1 {
		t.Fatalf("receipt race duplicated effect %d/%d", e, d)
	}
}
func TestRefinementQueuedSendDoesNotClaimOldRunOutput(t *testing.T) {
	f := newFixture(t)
	_, _, a, stream := startFeatureSession(t, f, "thread-a")
	if err := a.SetDelivery("queue"); err != nil {
		t.Fatal(err)
	}
	started, release := f.holdNextSendAck()
	defer release()
	done := make(chan error, 1)
	go func() { done <- a.Send(context.Background(), "queued new message") }()
	var id string
	select {
	case id = <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("queue dispatch missing")
	}
	old := object{"id": "older-running-run", "status": "running", "userMessageId": "older-message"}
	f.mu.Lock()
	f.threads["thread-a"]["runs"] = []any{clone(old)}
	for _, raw := range f.threads["thread-a"]["visibleTurnItems"].([]any) {
		raw.(object)["item"].(object)["runId"] = "older-running-run"
	}
	f.mu.Unlock()
	f.broadcast("thread-a", "run.updated", old, 0)
	f.update("thread-a", "Old run output while newer send is queued", 0)
	stream.until(t, func(e client.Event) bool {
		return containsMessage(e, "thread-a/first-thread-a", "Old run output") && e.Snapshot.Metadata.RuntimeDetails != nil && e.Snapshot.Metadata.RuntimeDetails.RunID != nil && *e.Snapshot.Metadata.RuntimeDetails.RunID == "older-running-run"
	})
	if receipt := a.Status().Receipt; receipt.Phase != "sending" || receipt.RunID != "" {
		t.Fatalf("old output owned new send %+v", receipt)
	}
	old["status"] = "completed"
	f.mu.Lock()
	f.threads["thread-a"]["runs"] = []any{clone(old)}
	f.mu.Unlock()
	f.broadcast("thread-a", "run.updated", old, 0)
	stream.until(t, func(e client.Event) bool { return e.Status.RunState == "completed" })
	if a.Status().Receipt.Phase != "sending" {
		t.Fatal("old completion owned new send")
	}
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("queue ACK did not finish")
	}
	if a.Status().Receipt.Phase != "acknowledged" {
		t.Fatal("queue receipt falsely responding")
	}
	newRun := object{"id": "queued-run", "status": "running", "userMessageId": id + ":message"}
	f.mu.Lock()
	f.threads["thread-a"]["runs"] = []any{newRun}
	f.mu.Unlock()
	f.broadcast("thread-a", "run.updated", newRun, 0)
	stream.until(t, func(e client.Event) bool {
		return e.Snapshot != nil && e.Snapshot.Metadata.RuntimeDetails != nil && e.Snapshot.Metadata.RuntimeDetails.RunID != nil && *e.Snapshot.Metadata.RuntimeDetails.RunID == "queued-run"
	})
	if a.Status().Receipt.Phase != "acknowledged" {
		t.Fatal("new run inherited old changed output")
	}
	f.mu.Lock()
	projected := row("thread-a", "queued-real-output", "New queued run's own response")
	item := projected["item"].(object)
	item["runId"] = "queued-run"
	f.threads["thread-a"]["visibleTurnItems"] = append(f.threads["thread-a"]["visibleTurnItems"].([]any), projected)
	response := clone(item)
	f.mu.Unlock()
	f.broadcast("thread-a", "turn-item.updated", response, 0)
	stream.until(t, func(e client.Event) bool {
		return e.Status.Receipt != nil && e.Status.Receipt.Phase == "responding" && e.Status.Receipt.RunID == "queued-run"
	})
}

func TestRefinementUncertainLaunchRetainsExactSelectionAndSameIDRetry(t *testing.T) {
	f := newFixture(t)
	_, state, a, stream := startFeatureSession(t, f, "thread-a")
	_ = a.SetDraft("source remains exact")
	started, release := f.holdNextLaunchAck()
	defer release()
	launch := featureCommand(a, "new", "")
	p := nativeLaunchReview(t, a)
	input := client.LaunchInput{Title: "  exact launch title  ", Message: " first message\n"}
	p.Respond(client.Answer{Action: "model", Launch: &input})
	p = featurePrompt(t, a)
	p.Respond(client.Answer{ChoiceIDs: []string{"fixture-other"}})
	p = featurePrompt(t, a)
	p.Respond(client.Answer{Action: "create", Launch: &input})
	var id string
	select {
	case id = <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("launch did not reach held receipt")
	}
	original := state.Pending("env-a", false)
	if original == nil || original["id"] != id {
		t.Fatal("launch did not persist original identity before dispatch")
	}
	f.mu.Lock()
	payload := clone(f.commands[id])
	f.mu.Unlock()
	f.closeSockets()
	release()
	r := featureReply(t, launch)
	if r.err == nil || r.result.Next != nil {
		t.Fatal("lost launch ACK was inferred successful")
	}
	stream.until(t, func(e client.Event) bool { return !e.Status.Connected })
	stream.until(t, func(e client.Event) bool {
		return e.Status.Connected && e.Status.ConnectionState == "connected" && e.Snapshot != nil && e.Snapshot.Metadata.Live
	})
	if !reflect.DeepEqual(original, state.Pending("env-a", false)) || a.Draft() != "source remains exact" || state.Draft("env-a", "\x00new/project-launch/project-a") != input.Message {
		t.Fatal("uncertain launch changed identity/drafts")
	}
	if e, d := f.counts(); e != 1 || d != 1 {
		t.Fatalf("reconnect replayed launch %d/%d", e, d)
	}
	// The original payload remains authoritative even when current defaults and
	// source workspace changed after an uncertain first dispatch.
	f.mu.Lock()
	provider := f.catalog["providers"].([]any)[0].(object)
	provider["models"] = []any{}
	f.threads["thread-a"]["thread"].(object)["worktreePath"] = "/fixture/changed-after-uncertainty"
	catalogReads := f.catalogReads
	f.mu.Unlock()
	reply, err := a.RetryLaunch(context.Background())
	if err != nil || reply.Next == nil || reply.Next.Status().ThreadID != original["newThreadId"] {
		t.Fatalf("same-ID launch retry failed %+v %v", reply, err)
	}
	defer reply.Next.Close()
	f.mu.Lock()
	retried := clone(f.commands[id])
	readsAfter := f.catalogReads
	f.mu.Unlock()
	if !reflect.DeepEqual(payload, retried) || catalogReads != readsAfter {
		t.Fatal("retry rebuilt payload or reread catalog")
	}
	if e, d := f.counts(); e != 1 || d != 1 {
		t.Fatalf("reconciliation redispatched a launch %d/%d", e, d)
	}
	if state.Pending("env-a", false) != nil || state.Draft("env-a", "\x00new/project-launch/project-a") != "" || a.Draft() != "source remains exact" {
		t.Fatal("acknowledged retry cleared wrong state")
	}
}

func TestRefinementUneditedLaunchKeepsNativeDefaultOptionsWithoutSourceCarry(t *testing.T) {
	f := newFixture(t)
	f.mu.Lock()
	f.threads["thread-a"]["thread"].(object)["modelSelection"] = object{"instanceId": "fixture-provider", "model": "fixture-model", "options": []any{object{"id": "fastMode", "value": true}, object{"id": "legacyOpaque", "value": "retained"}}}
	nativeDefault := object{"instanceId": "fixture-provider", "model": "fixture-model", "options": []any{object{"id": "nativeOpaque", "value": "retain-native-default"}, object{"id": "reasoningEffort", "value": "medium"}}}
	f.catalog["settings"].(object)["defaultModelSelection"] = nativeDefault
	f.mu.Unlock()
	_, _, a, _ := startFeatureSession(t, f, "thread-a")
	launch := featureCommand(a, "new", "")
	p := nativeLaunchReview(t, a)
	p.Respond(client.Answer{Action: "create", Launch: &client.LaunchInput{Title: "Inherited", Message: "first"}})
	r := featureReply(t, launch)
	if r.err != nil || r.result.Next == nil {
		t.Fatalf("unedited inherited settings rejected: %v", r.err)
	}
	defer r.result.Next.Close()
	f.mu.Lock()
	var payload object
	for _, c := range f.commands {
		if c["initialMessage"] != nil {
			payload = clone(c)
		}
	}
	f.mu.Unlock()
	if payload == nil || !reflect.DeepEqual(payload["modelSelection"], clone(nativeDefault)) {
		t.Fatalf("native defaults were lost or source options carried: %+v", payload)
	}
}

func TestRefinementLiveLaunchTypingCtrlQPersistsThroughStateReopen(t *testing.T) {
	f := newFixture(t)
	statePath := filepath.Join(t.TempDir(), "drafts.json")
	state, err := config.OpenState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	manager := client.NewManager([]config.Environment{f.profile}, state)
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = manager.Close()
			_ = state.Close()
		}
	})
	a, err := manager.NewSession(f.profile.ID, "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	source := "exact source composer"
	if err = a.SetDraft(source); err != nil {
		t.Fatal(err)
	}
	program, _ := startAppTUI(t, a)
	program.waitText("Initial Markdown for Alpha")
	program.waitText(source)
	// The normal palette route leaves the nonempty source composer untouched.
	program.send("\t\x10")
	program.modalReady("Commands")
	program.send("new\r")
	program.modalReady("Project")
	program.send("\r")
	program.modalReady("First message")
	message := "  unsent launch\nsecond line  "
	program.send(message)
	program.waitText("second line")
	program.send("\x11")
	select {
	case <-program.done:
		if program.err != nil {
			t.Fatal(program.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("launch CtrlQ did not finish")
	}
	if err = manager.Close(); err != nil {
		t.Fatal(err)
	}
	if err = state.Close(); err != nil {
		t.Fatal(err)
	}
	closed = true
	reopened, err := config.OpenState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := reopened.Draft("env-a", "\x00new/project-launch/project-a"); got != message {
		t.Fatalf("live launch draft lost on quit/reopen: %q", got)
	}
	if got := reopened.Draft("env-a", "thread-a"); got != source {
		t.Fatalf("source composer changed: %q", got)
	}
	if e, d := f.counts(); e != 0 || d != 0 {
		t.Fatal("quit dispatched launch")
	}
}
