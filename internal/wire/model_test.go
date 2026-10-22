package wire

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func modelRow(id, text string) map[string]any {
	return map[string]any{"key": "thread/" + id, "sourceThreadId": "thread", "itemId": id, "messageId": "message-" + id, "kind": "assistant_message", "text": text, "truncated": false, "streaming": true, "attachmentCount": 0, "position": 1}
}
func modelMetadata() map[string]any {
	return map[string]any{"threadId": "thread", "title": "Title", "projectId": "project", "model": "model", "status": "running", "activeRunId": "run", "requests": []map[string]any{}, "hasMore": true, "historyCursor": "opaque-cursor", "limited": false, "sequence": 5, "observedAt": "2026-10-22T00:00:00Z", "state": "live", "live": true, "historical": false}
}
func modelView(rev int, rows ...map[string]any) map[string]any {
	m := modelMetadata()
	m["revision"] = rev
	if rows == nil {
		rows = []map[string]any{}
	}
	m["items"] = rows
	return m
}
func modelDelta(base, rev int, upserts []map[string]any, removed, order []string) map[string]any {
	if upserts == nil {
		upserts = []map[string]any{}
	}
	if removed == nil {
		removed = []string{}
	}
	if order == nil {
		order = []string{}
	}
	return map[string]any{"threadId": "thread", "baseRevision": base, "revision": rev, "upserts": upserts, "removed": removed, "order": order, "metadata": modelMetadata()}
}
func modelRaw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func applyOK(t *testing.T, s *Store, event string, v any) {
	t.Helper()
	changed, err := s.Apply(event, modelRaw(v))
	if err != nil || !changed {
		t.Fatalf("apply %s: changed %v, err %v", event, changed, err)
	}
}

func TestKeyedReplacementReorderRemovalAndMetadataReplacement(t *testing.T) {
	var s Store
	s.Select("thread")
	applyOK(t, &s, "view", modelView(1, modelRow("one", "first"), modelRow("two", "second")))
	delta := modelDelta(1, 2, []map[string]any{modelRow("two", "rewritten"), modelRow("three", "third")}, []string{"thread/one"}, []string{"thread/three", "thread/two"})
	meta := delta["metadata"].(map[string]any)
	meta["activeRunId"] = nil
	delete(meta, "projectId")
	meta["requests"] = []map[string]any{{"id": "request", "prompt": "answer me"}}
	applyOK(t, &s, "delta", delta)
	got := s.Snapshot()
	if len(got.Items) != 2 || got.Items[0].ItemID != "three" || got.Items[1].Text != "rewritten" {
		t.Fatalf("incorrect keyed update: %+v", got.Items)
	}
	if got.Metadata.ActiveRunID != nil || got.Metadata.ProjectID != nil || len(got.Metadata.Requests) != 1 {
		t.Fatal("metadata was merged instead of replaced")
	}
}
func TestFailedDeltaIsAtomicAndRequiresNewSnapshot(t *testing.T) {
	var s Store
	applyOK(t, &s, "view", modelView(5, modelRow("one", "keep")))
	bad := modelDelta(5, 6, []map[string]any{modelRow("one", "must not leak")}, nil, []string{"thread/missing"})
	if changed, err := s.Apply("delta", modelRaw(bad)); changed || err == nil {
		t.Fatal("invalid order accepted")
	}
	if s.Snapshot().Items[0].Text != "keep" || !s.NeedsResnapshot() {
		t.Fatal("failed delta changed page or did not fail closed")
	}
	valid := modelDelta(5, 7, []map[string]any{modelRow("one", "new")}, nil, []string{"thread/one"})
	if _, err := s.Apply("delta", modelRaw(valid)); err == nil {
		t.Fatal("applied delta while resnapshot required")
	}
	applyOK(t, &s, "view", modelView(8, modelRow("one", "fresh")))
	if s.NeedsResnapshot() {
		t.Fatal("full snapshot did not recover")
	}
}
func TestRevisionGapAndStaleThreadOrRevision(t *testing.T) {
	var s Store
	s.Select("thread")
	applyOK(t, &s, "view", modelView(10, modelRow("one", "current")))
	for _, stale := range []struct {
		event string
		value map[string]any
	}{{"view", modelView(9, modelRow("old", "old"))}, {"delta", modelDelta(8, 9, nil, nil, []string{})}} {
		if changed, err := s.Apply(stale.event, modelRaw(stale.value)); changed || err != nil {
			t.Fatalf("stale event not ignored: %v", err)
		}
	}
	other := modelView(99, modelRow("old", "old"))
	other["threadId"] = "different"
	if changed, err := s.Apply("view", modelRaw(other)); changed || err != nil {
		t.Fatal("old thread changed new selection")
	}
	if s.Snapshot().Revision != 10 || s.NeedsResnapshot() {
		t.Fatal("stale events corrupted baseline")
	}
	if _, err := s.Apply("delta", modelRaw(modelDelta(12, 13, nil, nil, []string{}))); err == nil {
		t.Fatal("revision gap accepted")
	}
	s.Select("thread")
	applyOK(t, &s, "view", modelView(1))
	if s.Snapshot().Revision != 1 {
		t.Fatal("helper reconnect did not reset revision scope")
	}
}
func TestHistoryReplacesPageAndLatestRestoresLive(t *testing.T) {
	var s Store
	applyOK(t, &s, "view", modelView(1, modelRow("new", "Latest")))
	page := modelDelta(1, 2, []map[string]any{modelRow("old", "Earlier")}, []string{"thread/new"}, []string{"thread/old"})
	m := page["metadata"].(map[string]any)
	m["historical"] = true
	m["live"] = false
	m["state"] = "historical"
	m["activeRunId"] = nil
	applyOK(t, &s, "delta", page)
	if got := s.Snapshot(); len(got.Items) != 1 || got.Items[0].Text != "Earlier" || got.Metadata.Live || !got.Metadata.Historical {
		t.Fatal("history appended or remained live")
	}
	applyOK(t, &s, "view", modelView(3, modelRow("new", "Current")))
	if got := s.Snapshot(); got.Metadata.Historical || !got.Metadata.Live || len(got.Items) != 1 {
		t.Fatal("Latest failed")
	}
}
func TestSnapshotReadIsDeepCopy(t *testing.T) {
	var s Store
	view := modelView(1, modelRow("one", "text"))
	view["requests"] = []map[string]any{{"id": "request", "questions": []any{map[string]any{"id": "question"}}}}
	applyOK(t, &s, "view", view)
	copy := s.Snapshot()
	copy.Items[0].Text = "changed"
	*copy.Items[0].MessageID = "changed"
	*copy.Metadata.ActiveRunID = "changed"
	copy.Metadata.Requests[0]["id"] = "changed"
	copy.Metadata.Requests[0]["questions"].([]any)[0].(map[string]any)["id"] = "changed"
	got := s.Snapshot()
	if got.Items[0].Text != "text" || *got.Items[0].MessageID != "message-one" || *got.Metadata.ActiveRunID != "run" || got.Metadata.Requests[0]["id"] != "request" {
		t.Fatal("snapshot exposes mutable store")
	}
}
func TestMalformedFieldsAndBudgetsAreRejected(t *testing.T) {
	cases := []struct {
		name   string
		change func(map[string]any)
	}{
		{"wrong key", func(v map[string]any) { v["items"].([]map[string]any)[0]["key"] = "wrong" }},
		{"numeric boolean", func(v map[string]any) { v["live"] = 1 }},
		{"missing boolean", func(v map[string]any) { delete(v, "limited") }},
		{"state mismatch", func(v map[string]any) { v["historical"] = true }},
		{"negative revision", func(v map[string]any) { v["revision"] = -1 }},
		{"unsafe revision", func(v map[string]any) { v["revision"] = MaxInteger + 1 }},
		{"float revision", func(v map[string]any) { v["revision"] = 1.5 }},
		{"null requests", func(v map[string]any) { v["requests"] = nil }},
		{"long UTF8 text", func(v map[string]any) {
			v["items"].([]map[string]any)[0]["text"] = strings.Repeat("界", MaxTextBytes/3+1)
		}},
		{"duplicate rows", func(v map[string]any) { v["items"] = []map[string]any{modelRow("one", "a"), modelRow("one", "b")} }},
		{"row count", func(v map[string]any) {
			rows := []map[string]any{}
			for i := 0; i < 81; i++ {
				rows = append(rows, modelRow(fmt.Sprint(i), "text"))
			}
			v["items"] = rows
		}},
		{"row byte budget", func(v map[string]any) {
			rows := []map[string]any{}
			for i := 0; i < 8; i++ {
				rows = append(rows, modelRow(fmt.Sprint(i), strings.Repeat("x", MaxTextBytes)))
			}
			v["items"] = rows
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := modelView(1, modelRow("one", "text"))
			tc.change(v)
			if _, err := DecodeSnapshot(modelRaw(v)); err == nil {
				t.Fatal("malformed snapshot accepted")
			}
		})
	}
}
func TestDeltaRejectsDuplicateOrderAndConflictingKeys(t *testing.T) {
	for _, delta := range []map[string]any{
		modelDelta(1, 2, nil, nil, []string{"thread/one", "thread/one"}),
		modelDelta(1, 2, []map[string]any{modelRow("one", "x")}, []string{"thread/one"}, []string{}),
		modelDelta(2, 2, nil, nil, nil),
	} {
		if _, err := DecodeDelta(modelRaw(delta)); err == nil {
			t.Fatal("invalid delta accepted")
		}
	}
}
