package client

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

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
		UpdatedAt                                            string
		Inbox                                                json.RawMessage
		ID, Title, Project, Status, Model                    *string
		ProjectID                                            *string
		Lineage                                              json.RawMessage
		ParentTitle                                          string
		ChildCount, Depth                                    int
		WorkingChildCount, KnownChildCount, LoadedChildCount int
		Expanded, ContextOnly                                bool
	}
	if err = json.Unmarshal(encoded, &rows); err != nil || len(rows) > 200 {
		return nil, false, errors.New("invalid bounded thread list")
	}
	threads := make([]Thread, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.UpdatedAt != "" {
			if _, err := time.Parse(time.RFC3339Nano, row.UpdatedAt); err != nil {
				return nil, false, errors.New("invalid thread update time")
			}
		}
		inbox, err := wire.DecodeInboxMetadata(row.Inbox)
		if err != nil {
			return nil, false, err
		}
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
		if len(row.ParentTitle) > 512 || row.ChildCount < 0 || row.ChildCount > 1000000 || row.WorkingChildCount < 0 || row.WorkingChildCount > 1000000 || row.KnownChildCount < 0 || row.KnownChildCount > 1000000 || row.LoadedChildCount < 0 || row.LoadedChildCount > 200 || row.Depth < 0 || row.Depth > 12 {
			return nil, false, errors.New("invalid thread navigation metadata")
		}
		threads = append(threads, Thread{ID: *row.ID, Title: *row.Title, Project: *row.Project, ProjectID: projectID, Status: *row.Status, Model: *row.Model, Lineage: lineage, ParentTitle: row.ParentTitle, ChildCount: row.ChildCount, Depth: row.Depth, Expanded: row.Expanded, ContextOnly: row.ContextOnly})
		threads[len(threads)-1].WorkingChildCount = row.WorkingChildCount
		threads[len(threads)-1].UpdatedAt = row.UpdatedAt
		threads[len(threads)-1].Inbox = inbox
		threads[len(threads)-1].KnownChildCount = row.KnownChildCount
		threads[len(threads)-1].LoadedChildCount = row.LoadedChildCount
	}
	return threads, more, nil
}

// ThreadPage preserves completeness and observation metadata without changing
// the legacy array-oriented DecodeThreads API.
type ThreadPage struct {
	Threads    []Thread `json:"threads"`
	More       bool     `json:"more"`
	ObservedAt string   `json:"observedAt,omitempty"`
	Lifecycle  string   `json:"lifecycle,omitempty"`
	Cursor     int      `json:"cursor"`
	Limit      int      `json:"limit,omitempty"`
	NextCursor *int     `json:"nextCursor"`
}

func DecodeThreadPage(result map[string]any) (ThreadPage, error) {
	rows, more, err := DecodeThreads(result)
	if err != nil {
		return ThreadPage{}, err
	}
	page := ThreadPage{Threads: rows, More: more}
	if raw, exists := result["observedAt"]; exists {
		value, ok := raw.(string)
		if !ok || len(value) > 64 {
			return ThreadPage{}, errors.New("invalid thread observation time")
		}
		if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
			return ThreadPage{}, errors.New("invalid thread observation time")
		}
		page.ObservedAt = value
	}
	if value, ok := result["lifecycle"].(string); ok {
		if value != "active" && value != "all" && value != "completed" && value != "failed" {
			return ThreadPage{}, errors.New("invalid conversation lifecycle")
		}
		page.Lifecycle = value
	}
	if value, ok := result["cursor"]; ok {
		var n int
		encoded, _ := json.Marshal(value)
		if json.Unmarshal(encoded, &n) != nil || n < 0 || n > 1000000 {
			return ThreadPage{}, errors.New("invalid conversation cursor")
		}
		page.Cursor = int(n)
	}
	if value, ok := result["nextCursor"]; ok && value != nil {
		var n int
		encoded, _ := json.Marshal(value)
		if json.Unmarshal(encoded, &n) != nil || n <= page.Cursor || n > 1000000 {
			return ThreadPage{}, errors.New("invalid next conversation cursor")
		}
		next := int(n)
		page.NextCursor = &next
	}
	if value, ok := result["limit"]; ok {
		var n int
		encoded, _ := json.Marshal(value)
		if json.Unmarshal(encoded, &n) != nil || n < 1 || n > 200 {
			return ThreadPage{}, errors.New("invalid conversation page limit")
		}
		page.Limit = n
	}
	return page, nil
}
