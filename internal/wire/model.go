// Package wire implements the existing T3oxide v1 NDJSON client protocol.
// It has no T3 HTTP routes, orchestration engine, or conversation database.
package wire

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Xon333/t3oxide/internal/config"
	"sync"
	"unicode/utf8"
)

const (
	MaxFrameBytes            = 384 * 1024
	MaxInputBytes            = 96 * 1024
	MaxRowsBytes             = 144 * 1024
	MaxTextBytes             = 24 * 1024
	MaxRows                  = 80
	MaxIdentifierBytes       = 4096
	MaxKeyBytes              = 2*MaxIdentifierBytes + 1
	MaxInteger         int64 = 9007199254740991
)

type Row struct {
	Key             string  `json:"key"`
	SourceThreadID  string  `json:"sourceThreadId"`
	ItemID          string  `json:"itemId"`
	MessageID       *string `json:"messageId"`
	Kind            string  `json:"kind"`
	Text            string  `json:"text"`
	Truncated       bool    `json:"truncated"`
	Streaming       bool    `json:"streaming"`
	AttachmentCount int64   `json:"attachmentCount"`
	Position        *int64  `json:"position,omitempty"`
	bytes           int
}

type Metadata struct {
	ModelSelection *config.ModelSelection `json:"modelSelection,omitempty"`
	Lineage        *Lineage               `json:"lineage,omitempty"`
	RuntimeDetails *RuntimeDetails        `json:"runtimeDetails,omitempty"`
	ThreadID       string                 `json:"threadId"`
	Title          string                 `json:"title"`
	ProjectID      *string                `json:"projectId,omitempty"`
	Model          string                 `json:"model"`
	Mode           *string                `json:"mode,omitempty"`
	Status         string                 `json:"status"`
	ActiveRunID    *string                `json:"activeRunId,omitempty"`
	Requests       []map[string]any       `json:"requests"`
	HasMore        bool                   `json:"hasMore"`
	HistoryCursor  *string                `json:"historyCursor,omitempty"`
	Limited        bool                   `json:"limited"`
	Sequence       *int64                 `json:"sequence,omitempty"`
	ObservedAt     *string                `json:"observedAt,omitempty"`
	State          string                 `json:"state"`
	Live           bool                   `json:"live"`
	Historical     bool                   `json:"historical"`
}

type Snapshot struct {
	Revision int64    `json:"revision"`
	Items    []Row    `json:"items"`
	Metadata Metadata `json:"metadata"`
}

type Delta struct {
	ThreadID     string   `json:"threadId"`
	BaseRevision int64    `json:"baseRevision"`
	Revision     int64    `json:"revision"`
	Upserts      []Row    `json:"upserts"`
	Removed      []string `json:"removed"`
	Order        []string `json:"order"`
	Metadata     Metadata `json:"metadata"`
}

type object map[string]json.RawMessage

func parseObject(raw []byte) (object, error) {
	if len(raw) > MaxFrameBytes || !utf8.Valid(raw) {
		return nil, errors.New("transcript frame exceeds its byte limit or is not UTF-8")
	}
	var obj object
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, errors.New("transcript frame must be a JSON object")
	}
	return obj, nil
}
func fieldError(k string) error { return fmt.Errorf("invalid transcript field: %s", k) }
func (o object) str(k string, limit int) (string, error) {
	var s string
	v, ok := o[k]
	if !ok || bytes.Equal(v, []byte("null")) || json.Unmarshal(v, &s) != nil || len(s) > limit {
		return "", fieldError(k)
	}
	return s, nil
}
func (o object) optStr(k string, limit int) (*string, error) {
	if v, ok := o[k]; !ok || bytes.Equal(v, []byte("null")) {
		return nil, nil
	}
	s, e := o.str(k, limit)
	if e != nil {
		return nil, e
	}
	return &s, nil
}
func identifier(s string, limit int) bool {
	if s == "" || len(s) > limit {
		return false
	}
	for _, r := range s {
		if r < 32 {
			return false
		}
	}
	return true
}
func (o object) id(k string, limit int) (string, error) {
	s, e := o.str(k, limit)
	if e != nil {
		return "", e
	}
	if !identifier(s, limit) {
		return "", fieldError(k)
	}
	return s, nil
}
func (o object) optID(k string) (*string, error) {
	s, e := o.optStr(k, MaxIdentifierBytes)
	if e == nil && s != nil && !identifier(*s, MaxIdentifierBytes) {
		e = fieldError(k)
	}
	return s, e
}
func (o object) integer(k string) (int64, error) {
	var n int64
	v, ok := o[k]
	if !ok || bytes.Equal(v, []byte("null")) || json.Unmarshal(v, &n) != nil || n < 0 || n > MaxInteger {
		return 0, fieldError(k)
	}
	return n, nil
}
func (o object) optInt(k string) (*int64, error) {
	if v, ok := o[k]; !ok || bytes.Equal(v, []byte("null")) {
		return nil, nil
	}
	n, e := o.integer(k)
	if e != nil {
		return nil, e
	}
	return &n, nil
}
func (o object) boolean(k string) (bool, error) {
	var b bool
	v, ok := o[k]
	if !ok || (string(v) != "true" && string(v) != "false") {
		return false, fieldError(k)
	}
	if json.Unmarshal(v, &b) != nil {
		return false, fieldError(k)
	}
	return b, nil
}

func decodeMetadata(o object) (Metadata, error) {
	var m Metadata
	if err := decodeExtensions(o, &m); err != nil {
		return m, err
	}
	var err error
	if m.ThreadID, err = o.id("threadId", MaxIdentifierBytes); err != nil {
		return m, err
	}
	if m.Title, err = o.str("title", 512); err != nil {
		return m, err
	}
	if m.ProjectID, err = o.optID("projectId"); err != nil {
		return m, err
	}
	if m.Model, err = o.str("model", 256); err != nil {
		return m, err
	}
	if m.Mode, err = o.optStr("mode", 256); err != nil {
		return m, err
	}
	if m.Status, err = o.str("status", 256); err != nil {
		return m, err
	}
	if m.ActiveRunID, err = o.optID("activeRunId"); err != nil {
		return m, err
	}
	if m.HasMore, err = o.boolean("hasMore"); err != nil {
		return m, err
	}
	if m.Limited, err = o.boolean("limited"); err != nil {
		return m, err
	}
	if m.HistoryCursor, err = o.optStr("historyCursor", 64*1024); err != nil {
		return m, err
	}
	if m.Sequence, err = o.optInt("sequence"); err != nil {
		return m, err
	}
	if m.ObservedAt, err = o.optStr("observedAt", 256); err != nil {
		return m, err
	}
	if m.State, err = o.str("state", 32); err != nil {
		return m, err
	}
	if m.Live, err = o.boolean("live"); err != nil {
		return m, err
	}
	if m.Historical, err = o.boolean("historical"); err != nil {
		return m, err
	}
	if m.State != "synchronizing" && m.State != "live" && m.State != "historical" && m.State != "unavailable" {
		return m, errors.New("unknown transcript state")
	}
	if m.Live != (m.State == "live") || m.Live && m.Historical || m.State == "historical" && !m.Historical {
		return m, errors.New("inconsistent transcript state flags")
	}
	requests, ok := o["requests"]
	if !ok {
		return m, fieldError("requests")
	}
	dec := json.NewDecoder(bytes.NewReader(requests))
	dec.UseNumber()
	if dec.Decode(&m.Requests) != nil || m.Requests == nil || len(m.Requests) > 8 {
		return m, fieldError("requests")
	}
	for _, request := range m.Requests {
		if request == nil {
			return m, fieldError("requests")
		}
	}
	return m, nil
}

func decodeRow(raw []byte) (Row, error) {
	var r Row
	o, err := parseObject(raw)
	if err != nil {
		return r, err
	}
	if r.Key, err = o.id("key", MaxKeyBytes); err != nil {
		return r, err
	}
	if r.SourceThreadID, err = o.id("sourceThreadId", MaxIdentifierBytes); err != nil {
		return r, err
	}
	if r.ItemID, err = o.id("itemId", MaxIdentifierBytes); err != nil {
		return r, err
	}
	if r.Key != r.SourceThreadID+"/"+r.ItemID {
		return r, errors.New("transcript row key does not match its identity")
	}
	if r.MessageID, err = o.optID("messageId"); err != nil {
		return r, err
	}
	if r.Kind, err = o.str("kind", 256); err != nil {
		return r, err
	}
	if r.Text, err = o.str("text", MaxTextBytes); err != nil {
		return r, err
	}
	if r.Truncated, err = o.boolean("truncated"); err != nil {
		return r, err
	}
	if r.Streaming, err = o.boolean("streaming"); err != nil {
		return r, err
	}
	if r.AttachmentCount, err = o.integer("attachmentCount"); err != nil {
		return r, err
	}
	if r.Position, err = o.optInt("position"); err != nil {
		return r, err
	}
	r.bytes = len(raw)
	return r, nil
}

func rows(o object, k string) ([]Row, error) {
	raw, ok := o[k]
	if !ok {
		return nil, fieldError(k)
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) != nil || list == nil || len(list) > MaxRows {
		return nil, fieldError(k)
	}
	result := make([]Row, 0, len(list))
	for _, item := range list {
		r, e := decodeRow(item)
		if e != nil {
			return nil, e
		}
		result = append(result, r)
	}
	if err := validateRows(result); err != nil {
		return nil, err
	}
	return result, nil
}
func validateRows(rows []Row) error {
	if len(rows) > MaxRows {
		return errors.New("too many transcript rows")
	}
	seen := map[string]bool{}
	size := 2
	for i, r := range rows {
		if seen[r.Key] {
			return errors.New("duplicate transcript row key")
		}
		seen[r.Key] = true
		size += r.bytes
		if i > 0 {
			size++
		}
	}
	if size > MaxRowsBytes {
		return errors.New("transcript rows exceed their byte budget")
	}
	return nil
}
func keys(o object, k string) ([]string, error) {
	raw, ok := o[k]
	if !ok {
		return nil, fieldError(k)
	}
	var list []string
	if json.Unmarshal(raw, &list) != nil || list == nil || len(list) > MaxRows {
		return nil, fieldError(k)
	}
	seen := map[string]bool{}
	for _, s := range list {
		if !identifier(s, MaxKeyBytes) || seen[s] {
			return nil, fieldError(k)
		}
		seen[s] = true
	}
	return list, nil
}

func DecodeSnapshot(raw []byte) (*Snapshot, error) {
	o, err := parseObject(raw)
	if err != nil {
		return nil, err
	}
	s := &Snapshot{}
	if s.Revision, err = o.integer("revision"); err != nil {
		return nil, err
	}
	if s.Metadata, err = decodeMetadata(o); err != nil {
		return nil, err
	}
	if s.Items, err = rows(o, "items"); err != nil {
		return nil, err
	}
	return s, nil
}
func DecodeDelta(raw []byte) (*Delta, error) {
	o, err := parseObject(raw)
	if err != nil {
		return nil, err
	}
	d := &Delta{}
	if d.ThreadID, err = o.id("threadId", MaxIdentifierBytes); err != nil {
		return nil, err
	}
	if d.BaseRevision, err = o.integer("baseRevision"); err != nil {
		return nil, err
	}
	if d.Revision, err = o.integer("revision"); err != nil {
		return nil, err
	}
	if d.Revision <= d.BaseRevision {
		return nil, errors.New("delta revision must advance its base")
	}
	meta, err := parseObject(o["metadata"])
	if err != nil {
		return nil, err
	}
	if _, exists := meta["items"]; exists {
		return nil, errors.New("delta metadata contains rows")
	}
	if _, exists := meta["revision"]; exists {
		return nil, errors.New("delta metadata contains a revision")
	}
	if d.Metadata, err = decodeMetadata(meta); err != nil {
		return nil, err
	}
	if d.ThreadID != d.Metadata.ThreadID {
		return nil, errors.New("delta metadata identity mismatch")
	}
	if d.Upserts, err = rows(o, "upserts"); err != nil {
		return nil, err
	}
	if d.Removed, err = keys(o, "removed"); err != nil {
		return nil, err
	}
	if d.Order, err = keys(o, "order"); err != nil {
		return nil, err
	}
	removed := map[string]bool{}
	for _, k := range d.Removed {
		removed[k] = true
	}
	for _, r := range d.Upserts {
		if removed[r.Key] {
			return nil, errors.New("delta both removes and upserts a row")
		}
	}
	return d, nil
}

// Store validates an entire candidate before replacing the visible page. A
// helper reconnection must call Select or Reset because revisions are scoped
// to a helper lifetime. Historical pages replace the view; they never append.
type Store struct {
	mu       sync.RWMutex
	selected string
	snapshot *Snapshot
	needs    bool
}

func (s *Store) Select(threadID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.selected = threadID
	s.snapshot = nil
	s.needs = false
}
func (s *Store) Reset()                { s.Select("") }
func (s *Store) NeedsResnapshot() bool { s.mu.RLock(); defer s.mu.RUnlock(); return s.needs }
func (s *Store) Snapshot() *Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSnapshot(s.snapshot)
}

func (s *Store) Apply(event string, raw json.RawMessage) (changed bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() {
		if err != nil {
			s.needs = true
		}
	}()
	o, e := parseObject(raw)
	if e != nil {
		return false, e
	}
	thread, e := o.id("threadId", MaxIdentifierBytes)
	if e != nil {
		return false, e
	}
	if s.selected != "" && thread != s.selected {
		return false, nil
	}
	switch event {
	case "view":
		view, e := DecodeSnapshot(raw)
		if e != nil {
			return false, e
		}
		if s.snapshot != nil && view.Metadata.ThreadID == s.snapshot.Metadata.ThreadID && view.Revision <= s.snapshot.Revision {
			return false, nil
		}
		s.snapshot = view
		s.needs = false
		return true, nil
	case "delta":
		d, e := DecodeDelta(raw)
		if e != nil {
			return false, e
		}
		if s.snapshot != nil && d.ThreadID == s.snapshot.Metadata.ThreadID && d.Revision <= s.snapshot.Revision {
			return false, nil
		}
		if s.needs || s.snapshot == nil || d.ThreadID != s.snapshot.Metadata.ThreadID || d.BaseRevision != s.snapshot.Revision {
			return false, errors.New("transcript revision mismatch; request Latest to resynchronize")
		}
		indexed := map[string]Row{}
		for _, r := range s.snapshot.Items {
			indexed[r.Key] = r
		}
		for _, key := range d.Removed {
			if _, exists := indexed[key]; !exists {
				return false, errors.New("delta removes a missing row")
			}
			delete(indexed, key)
		}
		for _, r := range d.Upserts {
			indexed[r.Key] = r
		}
		if len(indexed) != len(d.Order) {
			return false, errors.New("delta order must name every retained row")
		}
		items := make([]Row, 0, len(d.Order))
		for _, key := range d.Order {
			r, exists := indexed[key]
			if !exists {
				return false, errors.New("delta order names a missing row")
			}
			items = append(items, r)
		}
		if err := validateRows(items); err != nil {
			return false, err
		}
		candidate := &Snapshot{d.Revision, items, d.Metadata}
		// Retained rows plus new metadata must still fit a complete view.
		var encoded bytes.Buffer
		encoder := json.NewEncoder(&encoded)
		encoder.SetEscapeHTML(false)
		_ = encoder.Encode(candidate)
		if encoded.Len()-1 > MaxFrameBytes {
			return false, errors.New("resulting view exceeds frame budget")
		}
		s.snapshot = candidate
		return true, nil
	default:
		return false, errors.New("unsupported transcript event")
	}
}

func cloneSnapshot(s *Snapshot) *Snapshot {
	if s == nil {
		return nil
	}
	n := *s
	// Extension pointers belong to the immutable snapshot, never shared with UI callers.
	if s.Metadata.ModelSelection != nil {
		v := s.Metadata.ModelSelection.Clone()
		n.Metadata.ModelSelection = &v
	}
	if s.Metadata.Lineage != nil {
		l := *s.Metadata.Lineage
		l.ParentThreadID = clonePtr(l.ParentThreadID)
		l.RootThreadID = clonePtr(l.RootThreadID)
		l.RelationshipToParent = clonePtr(l.RelationshipToParent)
		n.Metadata.Lineage = &l
	}
	if s.Metadata.RuntimeDetails != nil {
		b, _ := json.Marshal(s.Metadata.RuntimeDetails)
		n.Metadata.RuntimeDetails = nil
		_ = json.Unmarshal(b, &n.Metadata.RuntimeDetails)
	}
	n.Items = append([]Row(nil), s.Items...)
	for i := range n.Items {
		n.Items[i].MessageID = clonePtr(n.Items[i].MessageID)
		n.Items[i].Position = clonePtr(n.Items[i].Position)
	}
	n.Metadata.ProjectID = clonePtr(n.Metadata.ProjectID)
	n.Metadata.Mode = clonePtr(n.Metadata.Mode)
	n.Metadata.ActiveRunID = clonePtr(n.Metadata.ActiveRunID)
	n.Metadata.HistoryCursor = clonePtr(n.Metadata.HistoryCursor)
	n.Metadata.Sequence = clonePtr(n.Metadata.Sequence)
	n.Metadata.ObservedAt = clonePtr(n.Metadata.ObservedAt)
	b, _ := json.Marshal(s.Metadata.Requests)
	n.Metadata.Requests = nil // Unmarshal must not reuse the store's maps/slice.
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	_ = dec.Decode(&n.Metadata.Requests)
	return &n
}
func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
