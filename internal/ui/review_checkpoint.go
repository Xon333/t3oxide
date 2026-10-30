package ui

import tea "charm.land/bubbletea/v2"

type reviewPositionSavedMsg struct {
	generation uint64
	err        error
}

func (m *Model) checkpointReviewPosition() tea.Cmd {
	if m.detailTitle != "Changes" || m.agentbox.review == nil || m.session == nil {
		return nil
	}
	session, target, review, offset, generation := m.session, m.agentbox.reviewTarget, *m.agentbox.review, m.detailViewport.YOffset(), m.generation
	return func() tea.Msg {
		return reviewPositionSavedMsg{generation, session.SaveReviewPosition(target, review, offset)}
	}
}

func (m *Model) closeDetail() tea.Cmd {
	checkpoint := m.checkpointReviewPosition()
	if m.detailTitle == "Preview" {
		m.cancelNavigatorPreview()
	}
	m.detail = ""
	m.resize()
	return tea.Batch(checkpoint, m.restoreFocus())
}
