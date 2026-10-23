package client

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/Xon333/t3oxide/internal/wire"
)

// DecodeThreads validates the adapter's bounded list result for both the TUI
// and read-only CLI. Limits mirror adapter/t3.mjs's list projection. Optional
// future metadata is ignored; identities and required display fields are not.
func DecodeThreads(result map[string]any) ([]Thread, bool, error) {
	more, ok := result["more"].(bool)
	if !ok {
		return nil, false, errors.New("thread list is missing its boolean more flag")
	}
	encoded, err := json.Marshal(result["threads"])
	if err != nil || len(encoded) == 0 || encoded[0] != '[' {
		return nil, false, errors.New("thread list must be an array")
	}
	var rows []struct {
		ID, Title, Project, Status, Model *string
		ProjectID                         *string
		Lineage                           json.RawMessage
		ParentTitle                       string
		ChildCount, Depth                 int
		Expanded, ContextOnly             bool
	}
	if err = json.Unmarshal(encoded, &rows); err != nil || len(rows) > 200 {
		return nil, false, errors.New("invalid bounded thread list")
	}
	threads := make([]Thread, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.ID == nil || *row.ID == "" || len(*row.ID) > 4096 || seen[*row.ID] || strings.IndexFunc(*row.ID, func(r rune) bool { return r < 32 }) >= 0 {
			return nil, false, errors.New("invalid thread identity in list")
		}
		if row.Title == nil || len(*row.Title) > 512 || row.Project == nil || len(*row.Project) > 256 || row.Status == nil || len(*row.Status) > 64 || row.Model == nil || len(*row.Model) > 256 {
			return nil, false, errors.New("invalid bounded thread display fields")
		}
		seen[*row.ID] = true
		projectID := ""
		if row.ProjectID != nil {
			if *row.ProjectID == "" || len(*row.ProjectID) > 4096 || strings.IndexFunc(*row.ProjectID, func(r rune) bool { return r < 32 }) >= 0 {
				return nil, false, errors.New("invalid project identity in list")
			}
			projectID = *row.ProjectID
		}
		var lineage *wire.Lineage
		if len(row.Lineage) > 0 && string(row.Lineage) != "null" {
			var fields map[string]json.RawMessage
			if json.Unmarshal(row.Lineage, &fields) != nil {
				return nil, false, errors.New("invalid thread lineage")
			}
			if _, ok := fields["relationshipToParent"]; ok {
				if _, ok = fields["parentThreadId"]; ok {
					lineage = &wire.Lineage{}
					if json.Unmarshal(row.Lineage, lineage) != nil {
						return nil, false, errors.New("invalid thread lineage")
					}
					if err := lineage.Validate(); err != nil {
						return nil, false, err
					}
				}
			}
		}
		if len(row.ParentTitle) > 512 || row.ChildCount < 0 || row.ChildCount > 1000000 || row.Depth < 0 || row.Depth > 12 {
			return nil, false, errors.New("invalid thread navigation metadata")
		}
		threads = append(threads, Thread{ID: *row.ID, Title: *row.Title, Project: *row.Project, ProjectID: projectID, Status: *row.Status, Model: *row.Model, Lineage: lineage, ParentTitle: row.ParentTitle, ChildCount: row.ChildCount, Depth: row.Depth, Expanded: row.Expanded, ContextOnly: row.ContextOnly})
	}
	return threads, more, nil
}
