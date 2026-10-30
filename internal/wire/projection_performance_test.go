package wire

import (
	"fmt"
	"strings"
	"testing"
)

var projectionBenchmarkSink *Snapshot

// Compatible with an overlay on the untouched packet; measures defensive read
// allocation, not transport/terminal or native performance.
func BenchmarkRefinementDefensiveProjection(b *testing.B) {
	for _, count := range []int{24, 80} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			var store Store
			rows := make([]map[string]any, count)
			for i := range rows {
				rows[i] = modelRow(fmt.Sprint(i), strings.Repeat("Synthetic row text. ", 20))
			}
			view := modelView(1, rows...)
			view["requests"] = []map[string]any{{"id": "synthetic-request", "revision": "opaque-revision", "kind": "approval_request", "options": []any{map[string]any{"decision": "accept", "opaque": map[string]any{"nested": []any{true, nil, "preserved"}}}}}}
			if _, err := store.Apply("view", modelRaw(view)); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				projectionBenchmarkSink = store.Snapshot()
			}
		})
	}
}
