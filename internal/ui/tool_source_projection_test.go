package ui

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Xon333/t3oxide/internal/wire"
)

func TestToolSourcePartsPreserveBytesAndPresence(t *testing.T) {
	exact := "\tfoo\r\n```\r\n  bar\t\n"
	parts := toolSourceParts(wire.ActivityDetail{Input: exact, Output: exact, OutputPresent: true, Truncated: true})
	if len(parts) != 2 || parts[0].Text != exact || parts[1].Text != exact {
		t.Fatal(parts)
	}
	if len(toolSourceParts(wire.ActivityDetail{Output: "hidden", OutputOmitted: true})) != 0 {
		t.Fatal("offered omitted result")
	}
	if parts := toolSourceParts(wire.ActivityDetail{OutputPresent: true}); len(parts) != 1 || parts[0].Text != "" {
		t.Fatal("lost present empty result")
	}
}

func TestToolSourceOfferIdentity(t *testing.T) {
	item := wire.ActivityItem{Key: "source:key", SourceThreadID: "borrowed", ItemID: "tool", UpdatedAt: "r1"}
	m := &Model{generation: 4, snapshot: &wire.Snapshot{Metadata: wire.Metadata{ThreadID: "view", Activity: &wire.ActivityPage{Items: []wire.ActivityItem{item}}}}}
	offer := toolSourceOffer{Generation: 4, Thread: "view", Item: item, Part: "Result", Text: "\tbytes\r\n"}
	raw, _ := json.Marshal(offer)
	var decoded toolSourceOffer
	if json.Unmarshal(raw, &decoded) != nil || !reflect.DeepEqual(offer, decoded) || !m.toolSourceCurrent(decoded) {
		t.Fatal("offer roundtrip")
	}
	m.snapshot.Metadata.Activity.Items[0].UpdatedAt = "r2"
	if m.toolSourceCurrent(decoded) {
		t.Fatal("accepted stale revision")
	}
	m.snapshot.Metadata.Activity.Items[0] = item
	m.generation++
	if m.toolSourceCurrent(decoded) {
		t.Fatal("accepted replaced attachment")
	}
}

func TestRowSearchProjectionMatchesPageAndReusesStableRows(t *testing.T) {
	r := &transcriptRenderer{}
	snapshot := &wire.Snapshot{Items: []wire.Row{{Key: "a", Kind: "user", Text: "Stable **UPPER**"}, {Key: "b", Kind: "assistant", Text: "mutable"}}}
	req := renderRequest{snapshot: snapshot, width: 80, theme: "terminal"}
	content, err := r.render(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	stable := r.cache["a"].lowerLines
	snapshot.Items[1].Text = "changed\nneedle"
	content, err = r.render(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if r.cache["a"].lowerLines != stable {
		t.Fatal("reprojected stable row")
	}
	got := r.prepare(content, 80, "needle")
	want := prepareTranscript(content, 80, "needle")
	if !reflect.DeepEqual(got.lowerLines, want.lowerLines) || !reflect.DeepEqual(got.matches, want.matches) {
		t.Fatal("projection differs", strings.Join(got.lowerLines, "|"), strings.Join(want.lowerLines, "|"))
	}
	// Worker-owned slices must not alias a published prepared result.
	r.searchLines[0] = "replacement"
	if got.lowerLines[0] == "replacement" {
		t.Fatal("shared mutable page slice")
	}
}

func TestSourceCopyRedactionAndLimit(t *testing.T) {
	source := "\tkeep\r\nAuthorization: Bearer fixture-token\r\n"
	redacted := redactSourceBytes(source)
	if strings.Contains(redacted, "fixture-token") || !strings.HasPrefix(redacted, "\tkeep\r\n") {
		t.Fatal("redaction lost byte boundaries or retained credential")
	}
	m := &Model{}
	if cmd := m.copySourceBytes(strings.Repeat("x", 256*1024+1)); cmd != nil || m.problem != "Source exceeds clipboard limit" {
		t.Fatal("oversized source accepted")
	}
}

func TestToolSourceReadCannotReplaceNewModal(t *testing.T) {
	m := testModel(t)
	item := wire.ActivityItem{Key: "tool", SourceThreadID: "thread", ItemID: "item", UpdatedAt: "r1"}
	m.snapshot = &wire.Snapshot{Metadata: wire.Metadata{ThreadID: "thread", Activity: &wire.ActivityPage{Items: []wire.ActivityItem{item}}}}
	offer := toolSourceOffer{Generation: m.generation, Thread: "thread", Item: item}
	msg := toolSourceMsg{serial: m.dialogSerial, offer: offer, detail: wire.ActivityDetail{Key: "tool", SourceThreadID: "thread", ItemID: "item", RequestedRevision: "r1", Input: "source"}}
	m.openLocal("Newer surface", "context", nil)
	current := m.dialog
	m.acceptToolSource(msg)
	if m.dialog != current {
		t.Fatal("late tool source replaced newer modal")
	}
}
