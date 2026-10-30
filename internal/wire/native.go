package wire

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
)

// InboxMetadata is a bounded native shell projection. QueuedCount == -1 means
// unreported. PendingRequestCount is 0 or the shell's one offered summary; the
// complete actionable request form is acquired only after explicit attachment.
type InboxMetadata struct {
	NativeRevision      string           `json:"nativeRevision"`
	UpdatedAt           string           `json:"updatedAt"`
	Settled             bool             `json:"settled"`
	SnoozedUntil        string           `json:"snoozedUntil"`
	PinnedAt            string           `json:"pinnedAt"`
	PinOrderKey         string           `json:"pinOrderKey,omitempty"`
	Working             *bool            `json:"working,omitempty"`
	LatestUserMessageAt string           `json:"latestUserMessageAt,omitempty"`
	PendingChildRequest bool             `json:"pendingChildRequest,omitempty"`
	RunID               string           `json:"runId"`
	QueuedCount         int              `json:"queuedCount"`
	PendingRequestCount int              `json:"pendingRequestCount"`
	PendingRequestID    string           `json:"pendingRequestId"`
	PendingRequestType  string           `json:"pendingRequestType"`
	LatestMessage       *InboxMessage    `json:"latestMessage"`
	Workspace           string           `json:"workspace"`
	Branch              string           `json:"branch"`
	PullRequests        []PullRequestRef `json:"pullRequests"`
}
type InboxMessage struct {
	Role      string `json:"role"`
	Text      string `json:"text"`
	UpdatedAt string `json:"updatedAt"`
	Truncated bool   `json:"truncated"`
}
type PullRequestRef struct {
	URL        string `json:"url"`
	Number     int    `json:"number"`
	Repository string `json:"repository"`
	Title      string `json:"title"`
	State      string `json:"state"`
	SyncedAt   string `json:"syncedAt"`
}
type ReviewFile struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}
type ReviewSource struct {
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Hash      string `json:"hash"`
	Truncated bool   `json:"truncated"`
}
type ThreadReview struct {
	ThreadID      string           `json:"threadId"`
	ProjectID     string           `json:"projectId"`
	Revision      string           `json:"revision"`
	Workspace     string           `json:"workspace"`
	Branch        string           `json:"branch"`
	Diff          string           `json:"diff"`
	DiffTruncated bool             `json:"diffTruncated"`
	Files         []ReviewFile     `json:"files"`
	Sources       []ReviewSource   `json:"sources"`
	PullRequests  []PullRequestRef `json:"pullRequests"`
	GeneratedAt   string           `json:"generatedAt"`
	Unavailable   string           `json:"unavailable,omitempty"`
}

func nativeRevisionValid(v string) bool {
	return len(v) == 64 && strings.Trim(v, "0123456789abcdef") == ""
}
func nativeDate(v string, optional bool) bool {
	if v == "" {
		return optional
	}
	_, err := time.Parse(time.RFC3339Nano, v)
	return err == nil
}
func validatePRs(prs []PullRequestRef) error {
	if len(prs) > 32 {
		return errors.New("too many native pull request links")
	}
	for _, p := range prs {
		u, err := url.Parse(p.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || len(p.URL) > 2048 || p.Number <= 0 || len(p.Repository) > 512 || len(p.Title) > 512 || len(p.State) > 64 || !nativeDate(p.SyncedAt, true) {
			return errors.New("invalid native pull request link")
		}
	}
	return nil
}
func DecodeInboxMetadata(raw json.RawMessage) (*InboxMetadata, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if len(raw) > 96*1024 {
		return nil, errors.New("native inbox metadata exceeds budget")
	}
	var v InboxMetadata
	if json.Unmarshal(raw, &v) != nil || !nativeRevisionValid(v.NativeRevision) || !nativeDate(v.UpdatedAt, false) || !nativeDate(v.SnoozedUntil, true) || !nativeDate(v.PinnedAt, true) || len(v.PinOrderKey) > 256 || !nativeDate(v.LatestUserMessageAt, true) || v.QueuedCount < -1 || v.QueuedCount > 1000000 || v.PendingRequestCount < 0 || v.PendingRequestCount > 1 || len(v.Workspace) > 4096 || v.Workspace != "" && !strings.HasPrefix(v.Workspace, "/") || strings.ContainsAny(v.Workspace, "\x00\n\r") || len(v.Branch) > 512 || len(v.PendingRequestType) > 64 || (v.PendingRequestCount == 1) != (v.PendingRequestID != "") {
		return nil, errors.New("invalid native inbox metadata")
	}
	for _, id := range []string{v.RunID, v.PendingRequestID} {
		if id != "" && !identifier(id, MaxIdentifierBytes) {
			return nil, errors.New("invalid native inbox identity")
		}
	}
	if m := v.LatestMessage; m != nil {
		if (m.Role != "user" && m.Role != "assistant" && m.Role != "system") || len(m.Text) > 1024 || !nativeDate(m.UpdatedAt, true) {
			return nil, errors.New("invalid native message summary")
		}
	}
	if err := validatePRs(v.PullRequests); err != nil {
		return nil, err
	}
	return &v, nil
}
func DecodeThreadReview(result map[string]any) (ThreadReview, error) {
	var v ThreadReview
	b, err := json.Marshal(result)
	if err != nil || len(b) > MaxFrameBytes || json.Unmarshal(b, &v) != nil || !identifier(v.ThreadID, MaxIdentifierBytes) || !identifier(v.ProjectID, MaxIdentifierBytes) || !nativeRevisionValid(v.Revision) || !strings.HasPrefix(v.Workspace, "/") || len(v.Workspace) > 4096 || strings.ContainsAny(v.Workspace, "\x00\n\r") || len(v.Branch) > 512 || len(v.Diff) > 128*1024+8 || len(v.Files) > 200 || len(v.Sources) > 8 || len(v.Unavailable) > 4096 || !nativeDate(v.GeneratedAt, true) {
		return ThreadReview{}, errors.New("invalid native thread review")
	}
	for _, f := range v.Files {
		if f.Path == "" || len(f.Path) > 4096 || f.Additions < 0 || f.Deletions < 0 {
			return ThreadReview{}, errors.New("invalid native review file")
		}
	}
	for _, s := range v.Sources {
		if (s.Kind != "working-tree" && s.Kind != "branch-range") || len(s.Title) > 256 || len(s.Hash) > 128 {
			return ThreadReview{}, errors.New("invalid native review source")
		}
	}
	if err := validatePRs(v.PullRequests); err != nil {
		return ThreadReview{}, err
	}
	return v, nil
}
