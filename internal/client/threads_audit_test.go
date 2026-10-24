package client

import (
	"encoding/json"
	"testing"
)

func TestAuditThreadPageMetadata(t *testing.T) {
	raw := map[string]any{"threads": []Thread{}, "more": true, "observedAt": "2026-10-24T00:00:00Z", "lifecycle": "all", "cursor": json.Number("24"), "limit": 48, "nextCursor": json.Number("48")}
	p, err := DecodeThreadPage(raw)
	if err != nil || p.NextCursor == nil || *p.NextCursor != 48 || p.Cursor != 24 || p.Limit != 48 || p.Lifecycle != "all" {
		t.Fatalf("page %#v %v", p, err)
	}
	for _, value := range []any{1.5, "48", -1} {
		raw["nextCursor"] = value
		if _, err = DecodeThreadPage(raw); err == nil {
			t.Fatalf("accepted bad cursor %v", value)
		}
	}
}
