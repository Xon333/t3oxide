package client

import "github.com/Xon333/t3oxide/internal/wire"

func (a *Session) beginReceiptLocked(id, thread string, s *wire.Snapshot) {
	// An explicit same-ID retry keeps evidence observed before a lost ACK.
	if a.receipt != nil && a.receipt.OperationID == id {
		return
	}
	a.receipt = &Receipt{OperationID: id, MessageID: id + ":message", ThreadID: thread, Phase: "sending", Sequence: -1, ObservedSequence: -1}
	a.receiptBaselineRows = map[string]string{}
	if s != nil {
		for _, row := range s.Items {
			a.receiptBaselineRows[row.Key] = row.Text
		}
	}
}

func (a *Session) observeReceiptLocked(s *wire.Snapshot) {
	r := a.receipt
	if r == nil || s == nil || s.Metadata.Historical || !s.Metadata.Live || s.Metadata.ThreadID != r.ThreadID {
		return
	}
	runtime := s.Metadata.RuntimeDetails
	// Sequence or the latest user row alone cannot associate a queued message
	// with the previous run. Only the native initiating message establishes it.
	if runtime == nil || runtime.RunID == nil || runtime.RunMessageID == nil || *runtime.RunMessageID != r.MessageID {
		// A queued send must not retain changed old-run text as future evidence.
		a.receiptBaselineRows = map[string]string{}
		for _, row := range s.Items {
			a.receiptBaselineRows[row.Key] = row.Text
		}
		return
	}
	r.RunID = *runtime.RunID
	if s.Metadata.Sequence != nil {
		r.ObservedSequence = *s.Metadata.Sequence
	}
	switch runtime.Status {
	case "completed", "failed", "interrupted", "cancelled":
		r.Phase = runtime.Status
		if r.Phase == "cancelled" {
			r.Phase = "interrupted"
		}
		if a.lastAction == "send accepted" || a.lastAction == "send awaiting acknowledgement" {
			a.lastAction = ""
		}
		return
	}
	if receiptTerminal(r.Phase) {
		return
	}
	for _, row := range s.Items {
		old, exists := a.receiptBaselineRows[row.Key]
		if row.Kind == "assistant_message" && row.RunID != nil && *row.RunID == r.RunID && (!exists || old != row.Text) {
			r.Phase = "responding"
			if a.lastAction == "send accepted" || a.lastAction == "send awaiting acknowledgement" {
				a.lastAction = ""
			}
			return
		}
	}
}
func receiptTerminal(phase string) bool {
	return phase == "completed" || phase == "failed" || phase == "interrupted"
}
func (a *Session) acknowledgeReceiptLocked(id string, sequence int64, s *wire.Snapshot) {
	if a.receipt == nil || a.receipt.OperationID != id {
		return
	}
	a.receipt.Sequence = sequence
	a.observeReceiptLocked(s)
	if a.receipt.Phase == "sending" || a.receipt.Phase == "uncertain" {
		a.receipt.Phase = "acknowledged"
	}
}
