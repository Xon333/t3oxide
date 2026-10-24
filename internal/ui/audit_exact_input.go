package ui

import tea "charm.land/bubbletea/v2"

// This opt-in replacement is bound to the rejected input's attachment and
// unchanged composer. It never sends, normalizes, or merges bytes.
type exactRejectedInput struct {
	generation, edits uint64
	draft, text       string
}

func (m *Model) exactRejectedInputAvailable() bool {
	r := m.rejectedInput
	return r != nil && r.generation == m.generation && r.edits == m.draftEdits && r.draft == m.currentDraft() && r.text != ""
}
func (m *Model) useExactRejectedInput() tea.Cmd {
	if !m.exactRejectedInputAvailable() {
		m.notice = "That rejected input belongs to an earlier draft; use /recall"
		return nil
	}
	r := m.rejectedInput
	if r.draft != "" {
		if err := m.rememberPrompt("cleared", r.draft); err != nil {
			m.problem = "Draft retained: " + safeText(err.Error())
			return nil
		}
	}
	if err := m.storeDraft(r.text); err != nil {
		m.problem = "Exact input retained: " + safeText(err.Error())
		return nil
	}
	m.rejectedInput = nil
	m.notice = "Exact rejected input restored locally · review before sending"
	return m.restoreFocus()
}

func (m *Model) exactRejectedInputLabel() string {
	if m.currentDraft() != "" {
		return "Replace draft with exact rejected input"
	}
	return "Use exact rejected input"
}
