package client

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Xon333/t3oxide/internal/wire"
)

func TestCreationPinOverridesBrowsingButExactCLIAndMissingIdentityStayExplicit(t *testing.T) {
	m, _, env := recoveryManager(t)
	other := env
	other.ID = "other"
	other.EnvironmentID = "other-environment"
	m.Environments = append(m.Environments, other)
	a, err := m.NewSession(env.ID, "attached")
	if err != nil {
		t.Fatal(err)
	}
	if err = a.PinCreationDestination(other.ID, other.EnvironmentID, "project"); err != nil {
		t.Fatal(err)
	}
	got, err := a.creationEnvironment(context.Background())
	if err != nil || got.ID != other.ID {
		t.Fatal(got, err)
	}
	m.CreationProfile = env.ID
	got, err = a.creationEnvironment(context.Background())
	if err != nil || got.ID != env.ID {
		t.Fatal("explicit CLI target lost", err)
	}
	m.CreationProfile = ""
	m.Environments[1].EnvironmentID = "replacement-environment"
	if _, err = a.creationEnvironment(context.Background()); err == nil {
		t.Fatal("changed pinned source silently retargeted")
	}
	if err = a.ClearCreationDestination(); err != nil {
		t.Fatal(err)
	}
	m.Environments[0].DefaultForNew = true
	got, err = a.creationEnvironment(context.Background())
	if err != nil || got.ID != env.ID {
		t.Fatal("reset did not restore configured policy", err)
	}
}

func TestExactRequestPresentationSelectsOnlyOfferedRevisionAndDismissesWithoutMutation(t *testing.T) {
	a, _, log := recoverySession(t, "out={'result':{'sequence':3}}")
	requests := []map[string]any{}
	for _, id := range []string{"R1", "R2"} {
		requests = append(requests, map[string]any{"id": id, "revision": id + "-revision", "kind": "approval_request", "supported": true, "prompt": "Review " + id, "options": []any{map[string]any{"decision": "accept", "label": "Allow"}, map[string]any{"decision": "decline", "label": "Decline"}}})
	}
	if err := a.applyPacket(wire.Packet{Event: "view", Data: recoveryView(2, false, requests)}); err != nil {
		t.Fatal(err)
	}
	called := 0
	a.asker = recoveryAsker(func(_ context.Context, q Question) (Answer, error) {
		called++
		if q.RequestID != "R2" || q.RequestRevision != "R2-revision" || q.ThreadID != "thread" || q.EnvironmentID != "fixture-env" || !q.ConfirmChoice {
			t.Fatalf("wrong offer became actionable %+v", q)
		}
		return Answer{Cancelled: true}, nil
	})
	if err := a.ReviewRequest(context.Background(), "R2", "R2-revision"); err != nil {
		t.Fatal(err)
	}
	if called != 1 || len(requestsFromLog(t, log)) != 0 {
		t.Fatal("closing request cleared/answered it")
	}
	if err := a.ReviewRequest(context.Background(), "R2", "new-revision"); err == nil {
		t.Fatal("unoffered request revision opened")
	}
	if called != 1 {
		t.Fatal("stale request reached presenter")
	}
}

func TestSavedActionRemainsVisibleWhenOriginalEnvironmentIsRemoved(t *testing.T) {
	m, state, env := recoveryManager(t)
	a, err := m.NewSession(env.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	record := map[string]any{"id": "original", "environmentId": "removed-environment", "threadId": "original-thread", "op": "send", "message": "exact\r\n\ttext"}
	if err = state.SavePending("removed-environment", false, record); err != nil {
		t.Fatal(err)
	}
	actions := a.SavedActions()
	if len(actions) != 1 || actions[0].ProfileID != "" || actions[0].OperationID != "original" || actions[0].Record["message"] != "exact\r\n\ttext" {
		t.Fatal("missing route hid original recovery", actions)
	}
	a.asker = recoveryAsker(func(_ context.Context, q Question) (Answer, error) {
		if !strings.Contains(q.Prompt, "removed-environment") || !strings.Contains(q.Title, "May already be sent") {
			t.Fatal("original source not disclosed")
		}
		return Answer{ChoiceIDs: []string{"reconcile"}}, nil
	})
	if _, err = a.ReviewSavedAction(context.Background()); err == nil || !strings.Contains(err.Error(), "no longer configured") {
		t.Fatal("recovery silently chose another environment", err)
	}
	if state.Pending("removed-environment", false)["id"] != "original" {
		t.Fatal("unsupported recovery discarded journal")
	}
}

func defaultsResult() map[string]any {
	selection := map[string]any{"instanceId": "saved-A", "model": "saved-model", "options": []any{map[string]any{"id": "opaque-native-option", "value": "retain-me"}}}
	return map[string]any{"projectId": "project", "projectTitle": "Project", "scope": "project", "revision": strings.Repeat("a", 64), "writeSupported": false, "writeReason": "Native settings write lacks atomic revision and operation-ID reconciliation.", "settingsRoute": "Native T3 Settings", "fields": []any{map[string]any{"key": "defaultModelSelection", "source": "project", "overridden": true, "value": selection, "inheritedValue": nil}, map[string]any{"key": "futureNativeField", "source": "environment", "value": map[string]any{"number": json.Number("9007199254740991")}}}, "storedModelSelection": selection, "modelAvailable": false, "modelUnavailableReason": "Saved instance is disabled", "desktopLocal": "unreported", "futureExtension": map[string]any{"kept": "ignored bounded extra"}}
}

func TestNativeDefaultsPreserveUnavailableIdentityUnknownOptionsAndExactNumbers(t *testing.T) {
	input := defaultsResult()
	v, err := DecodeNativeDefaults(input, "project", "project")
	if err != nil {
		t.Fatal(err)
	}
	if v.StoredModelSelection.InstanceID != "saved-A" || v.ModelAvailable || v.StoredModelSelection.Options[0].Value != "retain-me" || v.Fields[1].Value.(map[string]any)["number"] != json.Number("9007199254740991") {
		t.Fatal("read changed opaque native defaults")
	}
	for _, want := range []string{"Editing unavailable: " + v.WriteReason, "Native Settings: " + v.SettingsRoute, "Source: project", "Source: environment", "9007199254740991", "retain-me", "Saved instance is disabled", "Reset would remove only this field", v.Revision} {
		if !strings.Contains(nativeDefaultsText(v, "exact-environment"), want) {
			t.Fatalf("complete native defaults inspection omitted %q", want)
		}
	}
	input["writeSupported"] = true
	if _, err = DecodeNativeDefaults(input, "project", "project"); err == nil {
		t.Fatal("unverified additive flag acquired write authority")
	}
	input["writeSupported"] = false
	if _, err = DecodeNativeDefaults(input, "other", "project"); err == nil {
		t.Fatal("scope identity mismatch accepted")
	}
}

func TestReviewPositionOnlyRestoresExactCompleteDiffHashes(t *testing.T) {
	m, _, env := recoveryManager(t)
	a, err := m.NewSession(env.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	target := InboxTarget{ProfileID: env.ID, EnvironmentID: env.EnvironmentID, ProjectID: "project", ThreadID: "thread"}
	review := wire.ThreadReview{ThreadID: "thread", ProjectID: "project", Sources: []wire.ReviewSource{{Kind: "working-tree", Hash: "H1"}}}
	if err = a.SaveReviewPosition(target, review, 42); err != nil {
		t.Fatal(err)
	}
	if offset, _ := a.ReviewPosition(target, review); offset != 42 {
		t.Fatal("matching hashes did not restore")
	}
	other := target
	other.EnvironmentID = "other"
	if offset, _ := a.ReviewPosition(other, review); offset != 0 {
		t.Fatal("cross-source identical hashes restored")
	}
	review.Sources[0].Hash = "H2"
	if offset, notice := a.ReviewPosition(target, review); offset != 0 || !strings.Contains(notice, "updated") {
		t.Fatal("changed diff inherited old reading progress")
	}
	review.Sources[0].Hash = "H1"
	review.DiffTruncated = true
	if offset, notice := a.ReviewPosition(target, review); offset != 0 || !strings.Contains(notice, "clipped") {
		t.Fatal("clipped diff inherited complete progress")
	}
	if err = a.SaveReviewPosition(target, review, 4); err != nil {
		t.Fatal(err)
	}
	review.DiffTruncated = false
	if offset, _ := a.ReviewPosition(target, review); offset != 0 {
		t.Fatal("invalidated position persisted")
	}
}

func TestReviewPositionNilStateHasSafeFreshFallback(t *testing.T) {
	if offset, _ := ((*Session)(nil)).ReviewPosition(InboxTarget{}, wire.ThreadReview{}); offset != 0 {
		t.Fatal(offset)
	}
}

func TestInboxUsesNativeWorkingMembershipAndChildAttention(t *testing.T) {
	row := inboxFixtureRow("env", "project", "thread", "completed")
	yes, no := true, false
	row.Thread.Inbox.Working = &yes
	if InboxBucket(row, time.Now()) != "working" {
		t.Fatal("native awaited background membership became settled work")
	}
	row.Thread.Inbox.PendingChildRequest = true
	if InboxBucket(row, time.Now()) != "needs" {
		t.Fatal("collapsed child request was hidden by working membership")
	}
	row.Thread.Inbox.PendingChildRequest = false
	row.Thread.Inbox.Working = &no
	row.Thread.Status = "active"
	if InboxBucket(row, time.Now()) == "working" {
		t.Fatal("legacy status overrode exact native membership")
	}
	row.Thread.Inbox.Working = nil
	if InboxBucket(row, time.Now()) != "working" {
		t.Fatal("older metadata lost its explicit active-state fallback")
	}
}
