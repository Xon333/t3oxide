package integration_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Xon333/t3oxide/internal/client"
)

func activityFixtureRow(thread, id, revision string) object {
	return object{"sourceThreadId": thread, "sourceItemId": id, "position": 2, "visibility": "local",
		"item": object{"id": id, "threadId": thread, "runId": nil, "type": "command_execution", "title": "Run tests",
			"input": "go test ./...", "status": "running", "updatedAt": revision, "outputOmitted": true}}
}

func TestActivityActualAdapterLazySourceBoundInspectionAndLiveUpdates(t *testing.T) {
	f := newFixture(t)
	item := activityFixtureRow("thread-a", "tool-a", "v1")
	f.mu.Lock()
	f.threads["thread-a"]["visibleTurnItems"] = append(f.threads["thread-a"]["visibleTurnItems"].([]any), item)
	f.activityItems = map[string]object{"thread-a/tool-a": clone(item["item"].(object))}
	f.activityItems["thread-a/tool-a"]["output"] = "fixture test output"
	f.activityItems["thread-a/tool-a"]["outputOmitted"] = false
	f.mu.Unlock()
	_, state, a, stream := startFeatureSession(t, f, "thread-a")
	if !a.Status().Activity || len(a.ActivityItems().Items) != 1 {
		t.Fatal("activity capability/metadata missing")
	}
	if f.requestCounts()["rpc:orchestration.getTurnItem"] != 0 {
		t.Fatal("opening transcript fetched outputs eagerly")
	}
	if err := a.SetDraft("exact draft"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		r, err := a.InspectActivity(context.Background(), "thread-a/tool-a")
		if err != nil || !strings.Contains(r.Detail, "fixture test output") {
			t.Fatalf("%+v %v", r, err)
		}
	}
	if f.requestCounts()["rpc:orchestration.getTurnItem"] != 1 {
		t.Fatal("same source/revision was refetched")
	}
	f.mu.Lock()
	updated := clone(item["item"].(object))
	updated["status"] = "completed"
	updated["updatedAt"] = "v2"
	f.mu.Unlock()
	f.broadcast("thread-a", "turn-item.updated", updated, 0)
	stream.until(t, func(e client.Event) bool {
		return e.Snapshot != nil && e.Snapshot.Metadata.Activity != nil && len(e.Snapshot.Metadata.Activity.Items) == 1 && e.Snapshot.Metadata.Activity.Items[0].Status == "completed"
	})
	if a.Draft() != "exact draft" || state.Pending(f.environmentID, false) != nil {
		t.Fatal("activity altered draft or saved an operation")
	}
	if effects, writes := f.counts(); effects != 0 || writes != 0 {
		t.Fatal("activity wrote to T3")
	}
}

func TestActivityActualAdapterInheritedItemUsesItsSourceThread(t *testing.T) {
	f := newFixture(t)
	inherited := activityFixtureRow("thread-b", "parent-tool", "v1")
	f.mu.Lock()
	f.threads["thread-a"]["visibleTurnItems"] = append(f.threads["thread-a"]["visibleTurnItems"].([]any), inherited)
	f.activityItems = map[string]object{"thread-b/parent-tool": clone(inherited["item"].(object))}
	f.activityItems["thread-b/parent-tool"]["output"] = "parent output"
	f.activityItems["thread-b/parent-tool"]["outputOmitted"] = false
	f.mu.Unlock()
	_, _, a, _ := startFeatureSession(t, f, "thread-a")
	r, err := a.InspectActivity(context.Background(), "thread-b/parent-tool")
	if err != nil || !strings.Contains(r.Detail, "parent output") || !strings.Contains(r.Detail, "Source: thread-b") {
		t.Fatalf("%+v %v", r, err)
	}
	if a.Status().ThreadID != "thread-a" || f.requestCounts()["rpc:orchestration.subscribeThread"] != 1 {
		t.Fatal("inherited inspection changed attachment")
	}
}

func TestActivityActualAdapterEarlierToolsDoNotReplaceConversationPage(t *testing.T) {
	f := newFixture(t)
	latest := activityFixtureRow("thread-a", "latest-tool", "v2")
	older := activityFixtureRow("thread-a", "older-tool", "v1")
	f.mu.Lock()
	f.threads["thread-a"]["visibleTurnItems"] = append(f.threads["thread-a"]["visibleTurnItems"].([]any), latest)
	f.activityHistory = map[string][]any{"thread-a": {older}}
	f.activityItems = map[string]object{"thread-a/older-tool": clone(older["item"].(object))}
	f.activityItems["thread-a/older-tool"]["output"] = "older output"
	f.activityItems["thread-a/older-tool"]["outputOmitted"] = false
	f.mu.Unlock()
	_, _, a, _ := startFeatureSession(t, f, "thread-a")
	if err := a.SetDraft("retained while inspecting"); err != nil {
		t.Fatal(err)
	}
	read := featureCommand(a, "activity", "")
	prompt := featurePrompt(t, a)
	prompt.Respond(client.Answer{ChoiceIDs: []string{"\x00earlier"}})
	prompt = featurePrompt(t, a)
	prompt.Respond(client.Answer{ChoiceIDs: []string{"thread-a/older-tool"}})
	r := featureReply(t, read)
	if r.err != nil || !strings.Contains(r.result.Detail, "older output") {
		t.Fatalf("%+v", r)
	}
	if a.Draft() != "retained while inspecting" || a.Status().Historical || a.ActivityItems().Items[0].ItemID != "latest-tool" {
		t.Fatal("tool history replaced main page or draft")
	}
	if f.requestCounts()["rpc:orchestration.subscribeThread"] != 1 {
		t.Fatal("tool history opened another subscription")
	}
}

func TestActivityActualAdapterRejectsMismatchedReturnedItem(t *testing.T) {
	f := newFixture(t)
	item := activityFixtureRow("thread-a", "tool", "v1")
	f.mu.Lock()
	f.threads["thread-a"]["visibleTurnItems"] = append(f.threads["thread-a"]["visibleTurnItems"].([]any), item)
	f.activityItems = map[string]object{"thread-a/tool": clone(item["item"].(object))}
	f.activityItems["thread-a/tool"]["threadId"] = "thread-b"
	f.mu.Unlock()
	_, state, a, _ := startFeatureSession(t, f, "thread-a")
	if _, err := a.InspectActivity(context.Background(), "thread-a/tool"); err == nil {
		t.Fatal("mismatched item accepted")
	}
	if state.Pending(f.environmentID, false) != nil {
		t.Fatal("read failure created recovery write")
	}
}
