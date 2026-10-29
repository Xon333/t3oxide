package ui

import (
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
)

// Only local editor and reading position are retained. Transcripts, requests,
// receipts and execution state continue to come from the exact native session.
type workspaceReturn struct {
	editor                      textarea.Model
	draft, preserved            string
	anchor                      readingAnchor
	offset                      int
	follow, reading, navigation bool
	context, activity           bool
	contextSelection            int
	contextKey, activityKey     string
}

func workspaceIdentity(environment, thread string) string { return environment + "\x00" + thread }

func (m *Model) rememberWorkspace() {
	if m.status.EnvironmentID == "" || m.status.ThreadID == "" {
		return
	}
	if m.workspaceReturns == nil {
		m.workspaceReturns = make(map[string]workspaceReturn)
	}
	key := workspaceIdentity(m.status.EnvironmentID, m.status.ThreadID)
	if _, ok := m.workspaceReturns[key]; !ok {
		if len(m.workspaceReturnOrder) >= 64 {
			delete(m.workspaceReturns, m.workspaceReturnOrder[0])
			m.workspaceReturnOrder = m.workspaceReturnOrder[1:]
		}
		m.workspaceReturnOrder = append(m.workspaceReturnOrder, key)
	}
	anchor := m.readingAnchor
	if anchor.key == "" || m.anchorOffset != m.viewport.YOffset() {
		anchor = anchorAt(m.transcriptRows, m.viewport.YOffset())
	}
	m.workspaceReturns[key] = workspaceReturn{editor: m.composer, draft: m.currentDraft(), preserved: m.preservedDraft, anchor: anchor, offset: m.viewport.YOffset(), follow: m.follow, reading: m.transcriptFocus, navigation: m.navigator.focused, context: m.contextFocused, activity: m.activityFocused, contextSelection: m.contextSelection, contextKey: m.contextFocusKey, activityKey: m.activityFocusKey}
}

func (m *Model) recallWorkspace(s *client.Session) {
	m.pendingWorkspaceReturn = nil
	if s == nil {
		return
	}
	status := s.Status()
	saved, ok := m.workspaceReturns[workspaceIdentity(status.EnvironmentID, status.ThreadID)]
	if !ok {
		return
	}
	if s.Draft() == saved.draft {
		m.composer = saved.editor
		m.preservedDraft = saved.preserved
	}
	m.transcriptFocus = saved.reading
	m.navigator.focused = saved.navigation
	m.contextFocused, m.activityFocused = saved.context, saved.activity
	m.contextSelection, m.contextFocusKey, m.activityFocusKey = saved.contextSelection, saved.contextKey, saved.activityKey
	m.follow = saved.follow
	m.pendingWorkspaceReturn = &saved
}

func (m *Model) restoreWorkspaceReading(rows []renderedRow) bool {
	saved := m.pendingWorkspaceReturn
	if saved == nil {
		return false
	}
	m.pendingWorkspaceReturn = nil
	m.follow = saved.follow
	if saved.follow {
		m.viewport.GotoBottom()
		return true
	}
	offset := saved.offset
	if saved.anchor.key != "" {
		for _, row := range rows {
			if row.key == saved.anchor.key {
				offset = row.start + saved.anchor.offset
				break
			}
		}
	}
	m.viewport.SetYOffset(offset)
	m.readingAnchor = anchorAt(rows, m.viewport.YOffset())
	m.anchorOffset = m.viewport.YOffset()
	return true
}

// Resolve visible row numbering before launching the async open. The closure
// carries its complete reviewed source page, never a future numeric position.
func (m *Model) openVisibleNavigatorNumber(number int) tea.Cmd {
	if !m.navigationVisible() || m.dialog != nil || m.detail != "" || number < 1 || number > 9 {
		return nil
	}
	items := m.navigator.list.VisibleItems()
	start, end := m.navigator.list.Paginator.GetSliceBounds(len(items))
	index := start + number - 1
	if index >= end || index >= len(items) {
		return nil
	}
	m.navigator.list.Select(index)
	page, row, ok := m.navigatorSource()
	if !ok || !m.navigatorPageLive(page) {
		return nil
	}
	return m.navigatorTarget(page, row.ID, "open")
}

func (m *Model) leaveActivityDetail() tea.Cmd {
	if !m.detailIsActivity || m.detailGeneration != m.generation || m.dialog != nil {
		return nil
	}
	m.detail, m.detailTitle, m.detailCopy = "", "", ""
	m.detailIsActivity = false
	if m.contextVisible() {
		m.contextFocused = true
		m.navigator.focused, m.transcriptFocus, m.activityFocused = false, false, false
		m.reviewContextSelection()
		m.resize()
		return m.restoreFocus()
	}
	if m.recentActivity() != nil {
		m.resize()
		return m.focusActivity()
	}
	m.resize()
	return m.restoreFocus()
}
