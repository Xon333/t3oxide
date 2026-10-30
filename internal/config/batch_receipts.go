package config

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const maxBatchReceipts = 16

const (
	BatchAcknowledged   = "Acknowledged"
	BatchNeedsRecovery  = "Needs recovery"
	BatchRemainingDraft = "Remaining draft"
)

// BatchReceipt is a bounded receipt for an explicitly reviewed human list. It
// has no timer, queue, executable payload or dispatcher. Original input lives
// with drafts in the existing private state; item ranges avoid copying it.
type BatchReceipt struct {
	ID            string             `json:"id"`
	EnvironmentID string             `json:"environmentId"`
	Source        string             `json:"source"`
	Original      string             `json:"original"`
	CreatedAt     string             `json:"createdAt"`
	PauseReason   string             `json:"pauseReason,omitempty"`
	Items         []BatchReceiptItem `json:"items"`
}

type BatchReceiptItem struct {
	Start         int    `json:"start"`
	End           int    `json:"end"`
	State         string `json:"state"`
	OperationID   string `json:"operationId,omitempty"`
	EnvironmentID string `json:"environmentId,omitempty"`
	ProjectID     string `json:"projectId,omitempty"`
	ThreadID      string `json:"threadId,omitempty"`
}

func (v BatchReceipt) Clone() BatchReceipt {
	v.Items = append([]BatchReceiptItem(nil), v.Items...)
	return v
}

func (v BatchReceipt) ItemText(index int) string {
	if index < 0 || index >= len(v.Items) {
		return ""
	}
	i := v.Items[index]
	if i.Start < 0 || i.End < i.Start || i.End > len(v.Original) {
		return ""
	}
	return v.Original[i.Start:i.End]
}

func validateBatchReceipt(v BatchReceipt) error {
	if !boundedIdentifier(v.ID, 4096) || !boundedIdentifier(v.EnvironmentID, 4096) || !strings.HasPrefix(v.Source, "\x00file-many/") || len(v.Source) > 4096 || !utf8.ValidString(v.Source) || !utf8.ValidString(v.Original) || len(v.Original) > 32*1024 || len(v.Items) == 0 || len(v.Items) > 20 || len(v.PauseReason) > 1024 {
		return errors.New("invalid bounded batch receipt")
	}
	if _, err := time.Parse(time.RFC3339Nano, v.CreatedAt); err != nil {
		return errors.New("invalid batch receipt time")
	}
	end := 0
	for _, item := range v.Items {
		if item.Start != end || item.End <= item.Start || item.End > len(v.Original) || !utf8.ValidString(v.Original[item.Start:item.End]) {
			return errors.New("invalid batch receipt input range")
		}
		end = item.End
		switch item.State {
		case BatchRemainingDraft:
		case BatchAcknowledged, BatchNeedsRecovery:
			if !boundedIdentifier(item.OperationID, 4096) || !boundedIdentifier(item.EnvironmentID, 4096) || !boundedIdentifier(item.ProjectID, 4096) || !boundedIdentifier(item.ThreadID, 4096) {
				return errors.New("batch action lacks exact native identity")
			}
		default:
			return errors.New("invalid batch receipt disposition")
		}
		for _, identity := range []string{item.OperationID, item.EnvironmentID, item.ProjectID, item.ThreadID} {
			if identity != "" && !boundedIdentifier(identity, 4096) {
				return errors.New("invalid batch item identity")
			}
		}
	}
	if strings.TrimSpace(v.Original[end:]) != "" {
		return errors.New("batch receipt omits human task text")
	}
	return nil
}

func (s *State) BatchReceipts() []BatchReceipt {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]BatchReceipt, 0, len(s.saved.BatchReceipts))
	for _, receipt := range s.saved.BatchReceipts {
		result = append(result, receipt.Clone())
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt == result[j].CreatedAt {
			return result[i].ID < result[j].ID
		}
		return result[i].CreatedAt > result[j].CreatedAt
	})
	return result
}

func (s *State) BeginBatchReceipt(receipt BatchReceipt) error {
	if err := validateBatchReceipt(receipt); err != nil {
		return err
	}
	for _, item := range receipt.Items {
		if item.State != BatchRemainingDraft || item.OperationID != "" {
			return errors.New("new batch receipt must contain only unsubmitted human drafts")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneState(s.saved)
	if _, exists := next.BatchReceipts[receipt.ID]; exists {
		return errors.New("batch receipt identity already exists")
	}
	if next.Drafts[draftKey(receipt.EnvironmentID, receipt.Source)] != receipt.Original {
		return errors.New("batch draft changed before receipt publication")
	}
	if len(next.BatchReceipts) >= maxBatchReceipts {
		oldest := ""
		for id, r := range next.BatchReceipts {
			complete := true
			for _, item := range r.Items {
				complete = complete && item.State == BatchAcknowledged
			}
			if complete && (oldest == "" || r.CreatedAt < next.BatchReceipts[oldest].CreatedAt) {
				oldest = id
			}
		}
		if oldest == "" {
			return errors.New("batch receipt limit reached; existing unfinished lists preserved")
		}
		delete(next.BatchReceipts, oldest)
	}
	next.BatchReceipts[receipt.ID] = receipt.Clone()
	return s.commit(next)
}

func (s *State) PauseBatchReceipt(id, reason string) error {
	if len(reason) > 1024 || !utf8.ValidString(reason) {
		return errors.New("invalid batch pause reason")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneState(s.saved)
	r, ok := next.BatchReceipts[id]
	if !ok {
		return errors.New("batch receipt is unavailable")
	}
	r.PauseReason = reason
	next.BatchReceipts[id] = r
	return s.commit(next)
}

// updateBatchReceipt runs inside the same transaction as SavePending, ACK or
// an explicitly resolved/rejected operation. A late receipt updates only the
// original item; it never consumes a later list or starts another item.
func updateBatchReceipt(next *savedState, record map[string]any, state string) error {
	b, ok := record["batchDraft"].(map[string]any)
	if !ok {
		return nil
	}
	id, exists := b["receiptId"]
	if !exists {
		return nil
	} // Compatibility with old journaled launches.
	rid, ok := id.(string)
	if !ok {
		return errors.New("invalid saved batch receipt identity")
	}
	r, ok := next.BatchReceipts[rid]
	if !ok {
		return errors.New("saved batch receipt is missing; preserve original action")
	}
	var index int
	raw, err := json.Marshal(b["itemIndex"])
	if err != nil || json.Unmarshal(raw, &index) != nil || index < 0 || index >= len(r.Items) {
		return errors.New("invalid saved batch item")
	}
	if b["environment"] != r.EnvironmentID || b["source"] != r.Source || b["consumed"] != r.ItemText(index) || b["before"] != r.Original[r.Items[index].Start:] {
		return errors.New("saved batch receipt source/input mismatch")
	}
	item := r.Items[index]
	if state == BatchNeedsRecovery {
		for _, previous := range r.Items[:index] {
			if previous.State != BatchAcknowledged {
				return errors.New("earlier batch item is not acknowledged; further filing stays paused")
			}
		}
	}
	op, _ := record["id"].(string)
	env, _ := record["environmentId"].(string)
	project, _ := record["projectId"].(string)
	thread, _ := record["newThreadId"].(string)
	if item.OperationID != "" && item.OperationID != op {
		return errors.New("batch receipt belongs to another operation")
	}
	if item.State == BatchAcknowledged && state != BatchAcknowledged {
		return errors.New("acknowledged batch item cannot be replayed")
	}
	item.State, item.OperationID, item.EnvironmentID, item.ProjectID, item.ThreadID = state, op, env, project, thread
	r.Items[index] = item
	next.BatchReceipts[rid] = r
	return nil
}
