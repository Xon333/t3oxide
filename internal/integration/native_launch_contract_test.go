package integration_test

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
)

func TestNativeSameProjectNewUsesCreationProfileAndOnlyProjectIdentity(t *testing.T) {
	source := newFixture(t)
	target := newEnvironmentFixture(t, "env-b", "creation", "Creation")
	target.profile.DefaultForNew = true
	source.mu.Lock()
	source.threads["thread-a"]["thread"].(object)["modelSelection"] = object{"instanceId": "fixture-provider", "model": "fixture-other", "options": []any{object{"id": "reasoningEffort", "value": "high"}}}
	source.threads["thread-a"]["thread"].(object)["runtimeMode"] = "full-access"
	source.mu.Unlock()
	target.mu.Lock()
	nativeSelection := object{"instanceId": "fixture-provider", "model": "fixture-model", "options": []any{object{"id": "reasoningEffort", "value": "medium"}}}
	target.catalog["settings"].(object)["defaultModelSelection"] = nativeSelection
	target.mu.Unlock()
	state, err := config.OpenState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	manager := client.NewManager([]config.Environment{source.profile, target.profile}, state)
	defer manager.Close()
	a, err := manager.NewSession(source.profile.ID, "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	stream := streamSession(t, a)
	stream.until(t, func(e client.Event) bool { return e.Snapshot != nil && e.Snapshot.Metadata.Live })
	if err := a.SetDraft(" exact source draft\n"); err != nil {
		t.Fatal(err)
	}
	before := source.requestCounts()
	launch := featureCommand(a, "new-same", "")
	p := featurePrompt(t, a)
	if p.Question.Surface != client.SurfaceLaunch || p.Question.LaunchReview.Location.ProfileID != target.profile.ID || p.Question.LaunchReview.Location.ProjectID != "project-a" || p.Question.LaunchReview.Location.ThreadID != "" || p.Question.LaunchReview.Location.Workspace != "/fixture/project-root" || p.Question.LaunchReview.Selection.Model != "fixture-model" || !strings.Contains(p.Question.LaunchReview.Summary, "approval-required") {
		t.Fatalf("same-project carried non-project state: %+v", p.Question)
	}
	p.Respond(client.Answer{Action: "create", Launch: &client.LaunchInput{Title: "Native empty filing"}})
	r := featureReply(t, launch)
	if r.err != nil || r.result.Next == nil || r.result.Next.Status().ProfileID != target.profile.ID {
		t.Fatalf("default target launch failed: %+v %v", r.result, r.err)
	}
	defer r.result.Next.Close()
	target.mu.Lock()
	filed := clone(target.threads[r.result.Next.Status().ThreadID])
	var command object
	for _, value := range target.commands {
		command = clone(value)
	}
	target.mu.Unlock()
	if len(filed["runs"].([]any)) != 0 || len(filed["visibleTurnItems"].([]any)) != 0 || command["initialMessage"] != nil || !reflect.DeepEqual(command["modelSelection"], clone(nativeSelection)) || command["workspaceStrategy"].(object)["type"] != "root" {
		t.Fatalf("filing launched work or inherited source settings: %+v %+v", filed, command)
	}
	if effects, dispatches := source.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("source environment was mutated")
	}
	if !reflect.DeepEqual(before, source.requestCounts()) || a.Draft() != " exact source draft\n" || state.Draft("env-a", "thread-a") != " exact source draft\n" {
		t.Fatal("new preparation read/mutated source attachment or lost draft")
	}
}

func TestNativeFirstThreadAndMissingSameProjectUseExistingProjectChoices(t *testing.T) {
	f := newFixture(t)
	f.mu.Lock()
	f.projects = append(f.projects, object{"id": "empty-project", "title": "Empty project", "workspaceRoot": "/fixture/empty-project"})
	f.mu.Unlock()
	manager, _ := newAppManager(t, f)
	a, err := manager.NewSession(f.profile.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan featureResult, 1)
	go func() {
		value, err := a.NewConversationForProject(context.Background(), "First thread", "missing-project", false)
		result <- featureResult{value, err}
	}()
	p := featurePrompt(t, a)
	if p.Question.Title != "Project" || !strings.Contains(p.Question.Prompt, "not registered") {
		t.Fatalf("missing project did not require deliberate choice: %+v", p.Question)
	}
	p.Respond(client.Answer{ChoiceIDs: []string{"empty-project"}})
	p = featurePrompt(t, a)
	if p.Question.Surface != client.SurfaceLaunch || p.Question.LaunchReview.Location.ProjectID != "empty-project" || p.Question.LaunchReview.Location.Workspace != "/fixture/empty-project" {
		t.Fatalf("empty existing project unavailable: %+v", p.Question)
	}
	p.Respond(client.Answer{Action: "create", Launch: &client.LaunchInput{Title: "First thread"}})
	r := featureReply(t, result)
	if r.err != nil || r.result.Next == nil {
		t.Fatalf("first thread failed: %v", r.err)
	}
	defer r.result.Next.Close()
	f.mu.Lock()
	created := clone(f.threads[r.result.Next.Status().ThreadID])
	f.mu.Unlock()
	if created["thread"].(object)["projectId"] != "empty-project" || len(created["runs"].([]any)) != 0 {
		t.Fatal("first thread did not use selected existing project")
	}
	counts := f.requestCounts()
	if counts["rpc:orchestration.subscribeThread"] != 0 {
		t.Fatal("new preparation subscribed a source transcript")
	}
	for path, count := range counts {
		if count > 0 && strings.Contains(path, "/threads/") {
			t.Fatalf("metadata-only New read a source transcript: %s", path)
		}
	}
}

func TestNativeRestoredFormKeepsOriginalBindingUntilExplicitDefaults(t *testing.T) {
	f := newFixture(t)
	_, state, a, _ := startFeatureSession(t, f, "thread-a")
	input := client.LaunchInput{Title: "  exact title  ", Message: "\nexact first message\n"}
	launch := featureCommand(a, "new", "")
	p := nativeLaunchReview(t, a)
	p.Respond(client.Answer{Cancelled: true, Launch: &input})
	if r := featureReply(t, launch); r.err != nil {
		t.Fatal(r.err)
	}
	original, ok := state.LaunchDraft("env-a", "project-launch/project-a")
	if !ok {
		t.Fatal("complete native form not saved")
	}
	f.mu.Lock()
	f.catalog["settings"].(object)["defaultRuntimeMode"] = "full-access"
	f.catalog["settings"].(object)["defaultModelSelection"] = object{"instanceId": "fixture-provider", "model": "fixture-other"}
	f.projects[0].(object)["workspaceRoot"] = "/fixture/new-root"
	f.mu.Unlock()
	for attempt := 0; attempt < 2; attempt++ {
		launch = featureCommand(a, "new", "")
		p = nativeLaunchReview(t, a)
		review := p.Question.LaunchReview
		if review.Input != input || review.Unavailable == "" || review.Selection.Model != original.Selection.Model || review.Location.Workspace != original.NativeWorkspaceRoot || !strings.Contains(review.Summary, original.RuntimeMode) {
			t.Fatalf("restored form silently adopted changed defaults: %+v", review)
		}
		if attempt == 0 {
			p.Respond(client.Answer{Cancelled: true, Launch: &input})
			if r := featureReply(t, launch); r.err != nil {
				t.Fatal(r.err)
			}
			saved, _ := state.LaunchDraft("env-a", "project-launch/project-a")
			if !reflect.DeepEqual(saved, original) {
				t.Fatalf("cancel accepted changed native defaults: %#v", saved)
			}
			continue
		}
		p.Respond(client.Answer{Action: "defaults", Launch: &input})
		p = featurePrompt(t, a)
		if p.Question.LaunchReview.Unavailable != "" || p.Question.LaunchReview.Selection.Model != "fixture-other" || p.Question.LaunchReview.Location.Workspace != "/fixture/new-root" || !strings.Contains(p.Question.LaunchReview.Summary, "full-access") || p.Question.LaunchReview.Input != input {
			t.Fatalf("explicit default refresh lost form or failed rebind: %+v", p.Question)
		}
		p.Respond(client.Answer{Action: "create", Launch: &input})
		r := featureReply(t, launch)
		if r.err != nil || r.result.Next == nil {
			t.Fatalf("explicit reviewed rebind failed: %v", r.err)
		}
		r.result.Next.Close()
	}
	if effects, dispatches := f.counts(); effects != 1 || dispatches != 1 {
		t.Fatalf("restore/cancel dispatched unexpectedly: %d/%d", effects, dispatches)
	}
}

func TestNativeLostLaunchAbsentAmbiguousAndIncompleteEvidenceKeepsSameID(t *testing.T) {
	f := newFixture(t)
	manager, state := newAppManager(t, f)
	a, err := manager.NewSession(f.profile.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	started, release := f.holdNextLaunchAck()
	defer release()
	launch := featureCommand(a, "new", "")
	p := nativeLaunchReview(t, a)
	input := client.LaunchInput{Title: "Lost response", Message: "exact original launch"}
	p.Respond(client.Answer{Action: "create", Launch: &input})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("launch did not arrive")
	}
	pending := state.Pending("env-a", false)
	thread, _ := pending["newThreadId"].(string)
	if thread == "" || pending["id"] == thread {
		t.Fatal("command/thread identities not retained independently")
	}
	f.mu.Lock()
	authoritative := clone(f.threads[thread])
	f.mu.Unlock()
	f.closeSockets()
	release()
	if r := featureReply(t, launch); r.err == nil || r.result.Next != nil {
		t.Fatal("lost launch response became inferred success")
	}
	for _, evidence := range []string{"absent", "wrong model", "wrong workspace", "missing message"} {
		f.mu.Lock()
		f.threads[thread] = clone(authoritative)
		switch evidence {
		case "absent":
			delete(f.threads, thread)
		case "wrong model":
			f.threads[thread]["thread"].(object)["modelSelection"] = object{"instanceId": "another-account", "model": "another-model"}
		case "wrong workspace":
			f.threads[thread]["thread"].(object)["worktreePath"] = "/fixture/unrelated-worktree"
		case "missing message":
			f.threads[thread]["runs"], f.threads[thread]["visibleTurnItems"] = []any{}, []any{}
		}
		f.mu.Unlock()
		if result, err := a.RetryLaunch(context.Background()); err == nil || result.Next != nil {
			t.Fatalf("%s evidence cleared uncertainty", evidence)
		}
		if !reflect.DeepEqual(pending, state.Pending("env-a", false)) {
			t.Fatalf("%s recovery changed original ID/payload", evidence)
		}
		if effects, dispatches := f.counts(); effects != 1 || dispatches != 1 {
			t.Fatalf("%s recovery redispatched launch: %d/%d", evidence, effects, dispatches)
		}
	}
	f.mu.Lock()
	f.threads[thread] = clone(authoritative)
	f.mu.Unlock()
	r, err := a.RetryLaunch(context.Background())
	if err != nil || r.Next == nil || r.Next.Status().ThreadID != thread {
		t.Fatalf("exact native evidence failed reconciliation: %+v %v", r, err)
	}
	r.Next.Close()
	if state.Pending("env-a", false) != nil {
		t.Fatal("exact reconciliation retained obsolete pending record")
	}
}

func TestNativeLaunchKeepsIndependentStopAvailableWhileReceiptIsHeld(t *testing.T) {
	f := newFixture(t)
	_, state, a, _ := startFeatureSession(t, f, "thread-a")
	started, release := f.holdNextLaunchAck()
	defer release()
	launch := featureCommand(a, "new", "")
	p := nativeLaunchReview(t, a)
	p.Respond(client.Answer{Action: "create", Launch: &client.LaunchInput{Title: "Parallel launch", Message: "first"}})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("launch did not enter pending lane")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.Stop(ctx); err != nil {
		t.Fatalf("held launch blocked independent Stop: %v", err)
	}
	if state.Pending("env-a", false) == nil || state.Pending("env-a", true) != nil {
		t.Fatal("Stop changed the ordinary operation lane")
	}
	f.mu.Lock()
	stopped := f.threads["thread-a"]["runs"].([]any)[0].(object)["status"]
	f.mu.Unlock()
	if stopped != "interrupted" {
		t.Fatalf("Stop did not interrupt reviewed source run: %v", stopped)
	}
	release()
	r := featureReply(t, launch)
	if r.err != nil || r.result.Next == nil {
		t.Fatalf("launch did not finish after separate Stop: %v", r.err)
	}
	r.result.Next.Close()
}
