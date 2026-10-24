package ui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Summaries precede the retained client output. Exact recovery IDs and payloads
// remain available below; no decoded value is used to issue or retry an action.
func (m *Model) readableClientDetail(name, text string) string {
	var rows []string
	switch name {
	case "runtime":
		rows = append(rows, "Runtime · "+m.attachedTargetStatus())
		if m.snapshot != nil {
			metadata := m.snapshot.Metadata
			if metadata.ModelSelection != nil {
				rows = append(rows, "Saved: "+metadata.ModelSelection.Summary())
			}
			if runtime := metadata.RuntimeDetails; runtime != nil {
				rows = append(rows, "Reported activity: "+runtime.Status+" · scope: "+runtime.Scope)
				if runtime.RunModelSelection != nil {
					rows = append(rows, "Run requested: "+runtime.RunModelSelection.Summary())
				}
				if runtime.ProviderReportedSelection != nil {
					rows = append(rows, "Provider reported: "+runtime.ProviderReportedSelection.Summary())
				} else {
					rows = append(rows, "Provider selection: unreported")
				}
			}
		}
	case "pending":
		rows = append(rows, "Saved operations · explicit review; no automatic replay")
		_, raw, found := strings.Cut(text, "```json\n")
		if found {
			raw, _, _ = strings.Cut(raw, "\n```")
			records := map[string]map[string]any{}
			if json.Unmarshal([]byte(raw), &records) == nil {
				for _, lane := range []string{"action", "stop"} {
					record := records[lane]
					if len(record) == 0 {
						rows = append(rows, lane+": no saved record")
						continue
					}
					fields := []string{lane}
					for _, key := range []string{"environmentId", "threadId", "operation", "op", "operationId"} {
						if value, ok := record[key]; ok {
							fields = append(fields, key+": "+fmt.Sprint(value))
						}
					}
					rows = append(rows, strings.Join(fields, " · "))
					for _, key := range []string{"text", "message", "title"} {
						if value, ok := record[key].(string); ok {
							rows = append(rows, "Intent: "+ansi.Truncate(safeText(value), 160, "…"))
							break
						}
					}
				}
			}
		}
		rows = append(rows, "Commands: Retry saved action / Retry saved action · Stop operation; Resolve requires review.")
	}
	if len(rows) == 0 {
		return text
	}
	return strings.Join(rows, "\n") + "\n\n" + text
}
