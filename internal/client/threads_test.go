package client

import (
	"strings"
	"testing"
)

func TestDecodeThreadsPreservesBoundedUnicodeAndEmptyResults(t *testing.T) {
	result := map[string]any{"more": true, "observedAt": "ignored future metadata", "threads": []Thread{{ID: "thread/原", Title: strings.Repeat("界", 170), Project: "workspace", Status: "running", Model: "model"}}}
	rows, more, err := DecodeThreads(result)
	if err != nil || !more || len(rows) != 1 || rows[0].ID != "thread/原" || rows[0].Title != strings.Repeat("界", 170) {
		t.Fatalf("valid Unicode list changed: %#v %v %v", rows, more, err)
	}
	rows, more, err = DecodeThreads(map[string]any{"more": false, "threads": []any{}})
	if err != nil || more || rows == nil || len(rows) != 0 {
		t.Fatalf("empty result must retain array shape: %#v %v %v", rows, more, err)
	}
}

func TestDecodeThreadsRejectsMalformedAndUnboundedLists(t *testing.T) {
	validRow := func() map[string]any {
		return map[string]any{"id": "thread", "title": "title", "project": "workspace", "status": "idle", "model": "model"}
	}
	cases := map[string]map[string]any{
		"missing list":       {"more": false},
		"null list":          {"more": false, "threads": nil},
		"object list":        {"more": false, "threads": map[string]any{}},
		"missing more":       {"threads": []any{}},
		"string more":        {"more": "false", "threads": []any{}},
		"null row":           {"more": false, "threads": []any{nil}},
		"duplicate identity": {"more": false, "threads": []any{validRow(), validRow()}},
	}
	for _, field := range []string{"id", "title", "project", "status", "model"} {
		missing := validRow()
		delete(missing, field)
		cases["missing "+field] = map[string]any{"more": false, "threads": []any{missing}}
		wrong := validRow()
		wrong[field] = 123
		cases["nonstring "+field] = map[string]any{"more": false, "threads": []any{wrong}}
	}
	for field, max := range map[string]int{"id": 4096, "title": 512, "project": 256, "status": 64, "model": 256} {
		row := validRow()
		row[field] = strings.Repeat("x", max+1)
		cases["oversize "+field] = map[string]any{"more": false, "threads": []any{row}}
	}
	for _, id := range []string{"", "bad\x1bidentity", "bad\nidentity"} {
		row := validRow()
		row["id"] = id
		cases["invalid identity "+id] = map[string]any{"more": false, "threads": []any{row}}
	}
	cases["too many rows"] = map[string]any{"more": true, "threads": make([]Thread, 201)}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := DecodeThreads(input); err == nil {
				t.Fatalf("malformed result accepted: %#v", input)
			}
		})
	}
}
