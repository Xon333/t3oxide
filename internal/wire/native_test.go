package wire

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNativeInboxMetadataKeepsUnknownQueueAndExactRequestIdentity(t *testing.T) {
	value := map[string]any{"nativeRevision": strings.Repeat("a", 64), "updatedAt": "2026-10-29T12:00:00Z", "settled": false, "snoozedUntil": "", "pinnedAt": "", "runId": "run", "queuedCount": -1, "pendingRequestCount": 1, "pendingRequestId": "request", "pendingRequestType": "approval", "latestMessage": map[string]any{"role": "assistant", "text": "bounded summary", "updatedAt": "2026-10-29T12:00:00Z"}}
	raw, _ := json.Marshal(value)
	metadata, err := DecodeInboxMetadata(raw)
	if err != nil || metadata.QueuedCount != -1 || metadata.PendingRequestID != "request" || metadata.LatestMessage.Text != "bounded summary" {
		t.Fatalf("native evidence lost: %+v %v", metadata, err)
	}
	for _, change := range []func(map[string]any){func(v map[string]any) { v["nativeRevision"] = "invented" }, func(v map[string]any) { v["queuedCount"] = -2 }, func(v map[string]any) { v["pendingRequestId"] = "" }, func(v map[string]any) { v["pendingRequestCount"] = 20 }, func(v map[string]any) { v["workspace"] = "relative" }} {
		copy := map[string]any{}
		for k, v := range value {
			copy[k] = v
		}
		change(copy)
		raw, _ = json.Marshal(copy)
		if _, err = DecodeInboxMetadata(raw); err == nil {
			t.Fatalf("invalid native summary accepted: %#v", copy)
		}
	}
	if metadata, err := DecodeInboxMetadata(nil); err != nil || metadata != nil {
		t.Fatal("older adapter without optional metadata stopped working")
	}
}

func TestNativeReviewValidatesDiffBudgetIdentityAndAuthenticLinks(t *testing.T) {
	value := map[string]any{"threadId": "native-thread", "projectId": "native-project", "revision": strings.Repeat("b", 64), "workspace": "/fixture/native-worktree", "diff": "diff --git a/a b/a\n+change", "generatedAt": "2026-10-29T12:00:00Z", "files": []any{map[string]any{"path": "a", "additions": 1, "deletions": 0}}, "sources": []any{map[string]any{"kind": "working-tree", "title": "Working changes", "hash": "reported-hash"}}, "pullRequests": []any{map[string]any{"url": "https://example.test/repository/pull/42", "number": 42, "title": "Native link", "state": "open", "syncedAt": "2026-10-29T12:00:00Z"}}}
	review, err := DecodeThreadReview(value)
	if err != nil || review.ThreadID != "native-thread" || review.PullRequests[0].Number != 42 {
		t.Fatalf("native review missing: %+v %v", review, err)
	}
	value["diff"] = strings.Repeat("x", 128*1024+9)
	if _, err := DecodeThreadReview(value); err == nil {
		t.Fatal("unbounded diff accepted")
	}
	value["diff"] = "bounded"
	value["pullRequests"].([]any)[0].(map[string]any)["url"] = "https://credential:secret@example.test/pull/42"
	if _, err := DecodeThreadReview(value); err == nil {
		t.Fatal("credential-bearing native PR link accepted")
	}
}
