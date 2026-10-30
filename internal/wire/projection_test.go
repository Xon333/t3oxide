package wire

import (
	"encoding/json"
	"testing"
)

func TestProjectionEpochSkipsUnchangedAndRevalidatesSameThreadReconnect(t *testing.T) {
	var s Store
	s.Select("thread")
	applyOK(t, &s, "view", modelView(1, modelRow("one", "text")))
	first, epoch, current := s.SnapshotSince(0)
	if first == nil || !current {
		t.Fatal("missing first projection")
	}
	first.Items[0].Text = "caller changed"
	if unchanged, sameEpoch, current := s.SnapshotSince(epoch); unchanged != nil || sameEpoch != epoch || !current {
		t.Fatal("unchanged read copied projection")
	}
	if s.Snapshot().Items[0].Text != "text" {
		t.Fatal("projection escaped mutable store")
	}
	s.Select("thread")
	if snapshot, next, current := s.SnapshotSince(epoch); snapshot != nil || next == epoch || current {
		t.Fatal("same-thread reset retained authoritative generation")
	}
	applyOK(t, &s, "view", modelView(1, modelRow("one", "new helper")))
	if snapshot, next, current := s.SnapshotSince(epoch); snapshot == nil || snapshot.Items[0].Text != "new helper" || next == epoch || !current {
		t.Fatal("new helper same revision was mistaken for old content")
	}
}

func TestTypedProjectionClonePreservesExactUnknownJSONValues(t *testing.T) {
	var s Store
	view := modelView(1, modelRow("one", "text"))
	view["requests"] = []map[string]any{{"id": "request", "revision": "revision/opaque", "future": map[string]any{"number": json.Number("9007199254740991"), "decimal": json.Number("1.234567890123456789"), "values": []any{nil, true, " exact\t\r\n "}}}}
	applyOK(t, &s, "view", view)
	copy := s.Snapshot()
	future := copy.Metadata.Requests[0]["future"].(map[string]any)
	if future["number"] != json.Number("9007199254740991") || future["decimal"] != json.Number("1.234567890123456789") {
		t.Fatal("opaque numbers lost exact representation")
	}
	future["values"].([]any)[2] = "mutated"
	future["number"] = json.Number("0")
	got := s.Snapshot().Metadata.Requests[0]["future"].(map[string]any)
	if got["number"] != json.Number("9007199254740991") || got["values"].([]any)[2] != " exact\t\r\n " {
		t.Fatal("nested read projection aliases store")
	}
}
