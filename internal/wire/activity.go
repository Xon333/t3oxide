package wire

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

const (
	MaxActivityItems       = 24
	MaxActivityBytes       = 24 * 1024
	MaxActivityDetailBytes = 72 * 1024
)

// ActivityPage describes only the server window read for this page. Total is
// the number of tool rows in that window, never a whole-conversation count.
type ActivityPage struct {
	Scope         string         `json:"scope"`
	Items         []ActivityItem `json:"items"`
	Total         int            `json:"total"`
	Limited       bool           `json:"limited"`
	HasMore       bool           `json:"hasMore"`
	HistoryCursor *string        `json:"historyCursor"`
}

type ActivityItem struct {
	Key            string `json:"key"`
	SourceThreadID string `json:"sourceThreadId"`
	ItemID         string `json:"itemId"`
	RunID          string `json:"runId"`
	Kind           string `json:"kind"`
	Title          string `json:"title"`
	Summary        string `json:"summary"`
	Status         string `json:"status"`
	UpdatedAt      string `json:"updatedAt"`
	OutputOmitted  bool   `json:"outputOmitted"`
	Truncated      bool   `json:"truncated"`
	Position       int64  `json:"position"`
}

type ActivityList struct {
	ThreadID   string       `json:"threadId"`
	Title      string       `json:"title,omitempty"`
	ObservedAt string       `json:"observedAt"`
	Activity   ActivityPage `json:"activity"`
}

// RequestedRevision is a cache key, not a conditional or historical read.
// Changed explicitly records when T3 returned a newer/different item version.
type ActivityDetail struct {
	Key               string `json:"key"`
	SourceThreadID    string `json:"sourceThreadId"`
	ItemID            string `json:"itemId"`
	Kind              string `json:"kind"`
	Title             string `json:"title"`
	Status            string `json:"status"`
	Input             string `json:"input"`
	Output            string `json:"output"`
	OutputPresent     bool   `json:"outputPresent"`
	OutputOmitted     bool   `json:"outputOmitted"`
	UpdatedAt         string `json:"updatedAt"`
	RequestedRevision string `json:"requestedRevision"`
	Changed           bool   `json:"changed"`
	Truncated         bool   `json:"truncated"`
}

func IsActivityKind(kind string) bool {
	switch kind {
	case "command_execution", "file_change", "file_search", "web_search", "dynamic_tool", "subagent", "todo_list":
		return true
	default:
		return false
	}
}

func activityIdentity(o object) (key, thread, item, kind string, err error) {
	if key, err = o.id("key", MaxKeyBytes); err != nil {
		return
	}
	if thread, err = o.id("sourceThreadId", MaxIdentifierBytes); err != nil {
		return
	}
	if item, err = o.id("itemId", MaxIdentifierBytes); err != nil {
		return
	}
	if key != thread+"/"+item {
		err = errors.New("activity key does not match its source identity")
		return
	}
	if kind, err = o.str("kind", 64); err != nil {
		return
	}
	if !IsActivityKind(kind) {
		err = errors.New("unsupported activity item kind")
	}
	return
}

func decodeActivityItem(raw []byte) (a ActivityItem, err error) {
	o, err := parseObject(raw)
	if err != nil {
		return a, err
	}
	if a.Key, a.SourceThreadID, a.ItemID, a.Kind, err = activityIdentity(o); err != nil {
		return
	}
	if a.RunID, err = o.str("runId", MaxIdentifierBytes); err != nil {
		return
	}
	if a.RunID != "" && !identifier(a.RunID, MaxIdentifierBytes) {
		return a, fieldError("activity.runId")
	}
	if a.Title, err = o.str("title", 256); err != nil {
		return
	}
	if a.Summary, err = o.str("summary", 768); err != nil {
		return
	}
	if a.Status, err = o.str("status", 64); err != nil {
		return
	}
	if a.UpdatedAt, err = o.str("updatedAt", 128); err != nil {
		return
	}
	if a.OutputOmitted, err = o.boolean("outputOmitted"); err != nil {
		return
	}
	if a.Truncated, err = o.boolean("truncated"); err != nil {
		return
	}
	if a.Position, err = o.integer("position"); err != nil {
		return
	}
	return
}

func DecodeActivityPage(raw []byte) (page ActivityPage, err error) {
	o, err := parseObject(raw)
	if err != nil {
		return page, err
	}
	if page.Scope, err = o.str("scope", 32); err != nil {
		return
	}
	if page.Scope != "page" && page.Scope != "history" {
		return page, fieldError("activity.scope")
	}
	count, err := o.integer("total")
	if err != nil || count > 1000000 {
		return page, fieldError("activity.total")
	}
	page.Total = int(count)
	if page.Limited, err = o.boolean("limited"); err != nil {
		return
	}
	if page.HasMore, err = o.boolean("hasMore"); err != nil {
		return
	}
	if page.HistoryCursor, err = o.optStr("historyCursor", 64*1024); err != nil {
		return
	}
	if page.HistoryCursor != nil && *page.HistoryCursor == "" {
		return page, fieldError("activity.historyCursor")
	}
	items, ok := o["items"]
	var rawItems []json.RawMessage
	if !ok || json.Unmarshal(items, &rawItems) != nil || rawItems == nil || len(rawItems) > MaxActivityItems {
		return page, fieldError("activity.items")
	}
	page.Items = make([]ActivityItem, 0, len(rawItems))
	seen := make(map[string]bool)
	for _, rawItem := range rawItems {
		item, e := decodeActivityItem(rawItem)
		if e != nil {
			return page, e
		}
		if seen[item.Key] {
			return page, errors.New("duplicate activity identity")
		}
		seen[item.Key] = true
		page.Items = append(page.Items, item)
	}
	if page.Total < len(page.Items) || page.Total > len(page.Items) && !page.Limited {
		return page, errors.New("inconsistent activity completeness")
	}
	if activityJSONSize(page.Items) > MaxActivityBytes {
		return page, errors.New("activity items exceed their byte budget")
	}
	return page, nil
}

func DecodeActivityList(raw []byte) (list ActivityList, err error) {
	o, err := parseObject(raw)
	if err != nil {
		return list, err
	}
	if list.ThreadID, err = o.id("threadId", MaxIdentifierBytes); err != nil {
		return
	}
	if title, e := o.optStr("title", 512); e != nil {
		return list, e
	} else if title != nil {
		list.Title = *title
	}
	if list.ObservedAt, err = o.str("observedAt", 128); err != nil {
		return
	}
	if list.Activity, err = DecodeActivityPage(o["activity"]); err != nil {
		return
	}
	return
}

func DecodeActivityDetail(raw []byte) (detail ActivityDetail, err error) {
	o, err := parseObject(raw)
	if err != nil {
		return detail, err
	}
	if detail.Key, detail.SourceThreadID, detail.ItemID, detail.Kind, err = activityIdentity(o); err != nil {
		return
	}
	if detail.Title, err = o.str("title", 256); err != nil {
		return
	}
	if detail.Status, err = o.str("status", 64); err != nil {
		return
	}
	if detail.Input, err = o.str("input", MaxTextBytes); err != nil {
		return
	}
	if detail.Output, err = o.str("output", MaxTextBytes); err != nil {
		return
	}
	if detail.UpdatedAt, err = o.str("updatedAt", 128); err != nil {
		return
	}
	if detail.RequestedRevision, err = o.str("requestedRevision", 128); err != nil {
		return
	}
	if detail.OutputPresent, err = o.boolean("outputPresent"); err != nil {
		return
	}
	if detail.OutputOmitted, err = o.boolean("outputOmitted"); err != nil {
		return
	}
	if detail.Changed, err = o.boolean("changed"); err != nil {
		return
	}
	if detail.Truncated, err = o.boolean("truncated"); err != nil {
		return
	}
	changed := detail.RequestedRevision != "" && detail.UpdatedAt != "" && detail.RequestedRevision != detail.UpdatedAt
	if detail.Changed != changed || !detail.OutputPresent && detail.Output != "" {
		return detail, errors.New("inconsistent activity detail metadata")
	}
	if activityJSONSize(detail) > MaxActivityDetailBytes {
		return detail, errors.New("activity detail exceeds its byte budget")
	}
	return detail, nil
}

// Request results can pass through a Go map before decoding. Go's json.Marshal
// escapes HTML and U+2028/U+2029 even though the Node wire does not. Validate the
// original compact-JSON budget rather than rejecting a harmless local encoding
// expansion. The outer parser still bounds every received frame at 384 KiB.
func activityJSONSize(value any) int {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if encoder.Encode(value) != nil {
		return MaxFrameBytes + 1
	}
	size := buffer.Len() - 1
	decoder := json.NewDecoder(bytes.NewReader(buffer.Bytes()))
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return MaxFrameBytes + 1
		}
		if text, ok := token.(string); ok {
			size -= 3 * (strings.Count(text, "\u2028") + strings.Count(text, "\u2029"))
		}
	}
	return size
}

func (p *ActivityPage) Clone() *ActivityPage {
	if p == nil {
		return nil
	}
	copy := *p
	copy.Items = append([]ActivityItem(nil), p.Items...)
	copy.HistoryCursor = clonePtr(p.HistoryCursor)
	return &copy
}
