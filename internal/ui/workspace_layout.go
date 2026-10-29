package ui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Xon333/t3oxide/internal/client"
)

// The split/focus policy is adapted from MIT t3term d1d33e89,
// src/tui/mod.rs::{columns,sidebar_width,cycle} and
// src/tui/details.rs::{layout,beside,pane_area}. Rendering uses the maintained
// Bubble Tea/Lip Gloss components; transport and local state stay t3oxide's.
func (m *Model) mainWidth() int { return max(1, m.width-m.navigatorWidth()) }
func (m *Model) contextVisible() bool {
	return m.sidebar && m.contextPaneFits()
}
func (m *Model) contextPaneFits() bool {
	// Below compact height, required composer and recovery rows can leave
	// only the Context heading. Use the local review dialog instead of
	// allowing keyboard selection of activity that cannot be drawn.
	return m.mainWidth() >= 110 && m.height >= 16 && !m.browserPage && !m.launchPage
}
func (m *Model) mainOriginX() int {
	return m.navigatorWidth() + max(0, (m.mainWidth()-m.workspaceWidth())/2)
}

// At compact widths, changing focus can replace the full-width navigator with
// the conversation. Recompute the complete layout and native render width;
// resizing only the list leaves the hidden conversation's one-cell allocation.
func (m *Model) resizeWorkspaceFocus() tea.Cmd {
	oldWidth := m.contentWidth
	m.resize()
	focus := m.restoreFocus()
	if oldWidth != m.contentWidth {
		return tea.Batch(focus, m.requestRender(false, false))
	}
	return focus
}

func (m *Model) cycleWorkspace(forward bool) tea.Cmd {
	if m.startupBrowser && m.status.ThreadID == "" {
		return m.focusNavigator() // no invisible composer or transcript before an explicit open
	}
	panes := []string{}
	if m.navigationVisible() {
		panes = append(panes, "navigator")
	}
	panes = append(panes, "composer", "transcript")
	if m.recentActivity() != nil {
		panes = append(panes, "activity")
	}
	if m.contextVisible() {
		panes = append(panes, "context")
	}
	current := "composer"
	if m.navigator.focused {
		current = "navigator"
	} else if m.transcriptFocus {
		current = "transcript"
	} else if m.activityFocused {
		current = "activity"
	} else if m.contextFocused {
		current = "context"
	}
	index := 0
	for i, p := range panes {
		if p == current {
			index = i
		}
	}
	delta := 1
	if !forward {
		delta = len(panes) - 1
	}
	next := panes[(index+delta)%len(panes)]
	m.navigator.focused = next == "navigator"
	m.transcriptFocus = next == "transcript"
	m.contextFocused = next == "context"
	m.activityFocused = next == "activity"
	if m.activityFocused {
		if item := m.recentActivity(); item != nil {
			m.activityFocusKey = item.Key
		}
	}
	if m.contextFocused {
		m.reviewContextSelection()
	}
	return m.resizeWorkspaceFocus()
}

func (m *Model) openLocal(title, name string, items []list.Item) {
	m.dialogSerial++
	d := &dialog{serial: m.dialogSerial, kind: client.ChooseOne, title: title, local: name, selected: map[string]bool{}}
	d.list = m.newList(title, items)
	d.list.Filter = commandFilter
	d.list.SetFilterText("")
	d.list.SetFilterState(list.Filtering)
	m.dialog = d
	m.detail = ""
	m.composer.Blur()
	m.navigator.list.FilterInput.Blur()
	m.refreshDialogDetails()
	m.resize()
	m.restoreFocus()
}

func (m *Model) reconcileNavigator() {
	if !m.navigator.loaded || m.status.ThreadID == "" {
		return
	}
	for i, raw := range m.navigator.list.Items() {
		item, ok := raw.(navigatorItem)
		if !ok || item.thread.ID != m.status.ThreadID || item.profile != "" && item.profile != m.status.ProfileID {
			continue
		}
		if m.status.RunState != "" {
			item.thread.Status = m.status.RunState
		}
		m.navigator.list.SetItem(i, item)
		// This is the attached stream only; other rows remain labeled snapshots.
		break
	}
}

func (m *Model) toolbarActions() []struct{ label, action string } {
	actions := []struct{ label, action string }{}
	if m.status.Requests > 0 {
		actions = append(actions, struct{ label, action string }{"Requests", "requests"})
	}
	if m.status.ActiveRunID != "" {
		actions = append(actions, struct{ label, action string }{"Stop", "stop"})
	}
	if m.status.PendingAction || m.status.PendingStop {
		actions = append(actions, struct{ label, action string }{"Saved", "pending"})
	}
	if m.snapshot != nil && m.snapshot.Metadata.Historical {
		actions = append(actions, struct{ label, action string }{"Latest", "latest"})
	}
	return actions
}

func (m *Model) toolbarView(width int) string {
	p := colors(m.theme)
	var labels []string
	for _, a := range m.visibleToolbarActions(width) {
		labels = append(labels, p.plain().Render(a.label))
	}
	return edgeLine(strings.Join(labels, "  "), p.subtle().Render(m.workspacePosition(width)), width)
}

func (m *Model) contextFacts() []string {
	loc := m.status.Location
	var rows []string
	if loc.Workspace != "" {
		rows = append(rows, compactContextPath(loc.Workspace, 28))
	}
	return rows
}

func (m *Model) openContext() tea.Cmd {
	if m.contextPaneFits() {
		m.sidebar = !m.sidebar
		m.contextFocused = m.sidebar
		m.navigator.focused = false
		m.transcriptFocus = false
		m.activityFocused = false
		m.reviewContextSelection()
		m.resize()
		return tea.Batch(m.requestRender(false, false), m.restoreFocus())
	}
	items := []list.Item{choiceItem{title: "Settings", command: "settings"}, choiceItem{title: "Runtime details", command: "runtime"}}
	if m.status.Activity {
		items = append(items, choiceItem{title: "Activity", command: "activity"})
	}
	if m.status.PendingAction || m.status.PendingStop {
		items = append(items, choiceItem{title: "Saved operations", command: "pending"})
	}
	m.openLocal("Conversation context", "context", items)
	m.dialog.description = strings.Join(m.contextFacts(), "\n")
	m.refreshDialogDetails()
	m.resize()
	return nil
}

func (m *Model) contextView(width, height int) string {
	content, _, _ := m.contextPanel(width, height)
	return content
}

// The same bounded viewport supplies text and click geometry. The focused
// activity remains on screen even when facts or earlier tools fill the pane.
func (m *Model) contextPanel(width, height int) (string, int, int) {
	p := colors(m.theme)
	label := "Activity"
	if m.contextFocused {
		label = "> " + label
	}
	var rows []string
	for _, value := range m.contextFacts() {
		if value != "" {
			rows = append(rows, p.plain().Render(fitLine(safeText(value), width)))
		}
	}
	first, count := -1, 0
	if m.snapshot != nil && m.snapshot.Metadata.Activity != nil {
		activity := m.snapshot.Metadata.Activity
		if len(rows) > 0 {
			rows = append(rows, "")
		}
		first, count = len(rows), len(activity.Items)
		for i, item := range activity.Items {
			text := navigatorStatus(item.Status) + " " + activityLabel(item)
			style := p.plain()
			if m.contextFocused && i == m.contextSelection {
				text = "> " + text
				style = p.selection()
			}
			rows = append(rows, style.Render(fitLine(text, width)))
		}
		if activity.HasMore {
			rows = append(rows, p.subtle().Render("More · /activity"))
		} else if activity.Limited {
			rows = append(rows, p.subtle().Render("Summaries shortened"))
		}
	}
	if m.status.Activity && (m.snapshot == nil || m.snapshot.Metadata.Activity == nil || len(m.snapshot.Metadata.Activity.Items) == 0) {
		rows = append(rows, p.subtle().Render("No activity in this page"))
	}
	v := viewport.New(viewport.WithWidth(width), viewport.WithHeight(max(1, height-1)))
	v.SetContent(strings.Join(rows, "\n"))
	if m.contextFocused && count > 0 {
		v.SetYOffset(max(0, first+min(m.contextSelection, count-1)-v.Height()+1))
	}
	return padBlock(p.active().Render(label)+"\n"+v.View(), width, height), first + 1, v.YOffset()
}

func (m *Model) reviewContextSelection() {
	m.contextFocusKey = ""
	if m.snapshot != nil && m.snapshot.Metadata.Activity != nil {
		items := m.snapshot.Metadata.Activity.Items
		if len(items) > 0 {
			m.contextSelection = min(max(0, m.contextSelection), len(items)-1)
			m.contextFocusKey = items[m.contextSelection].Key
		}
	}
}

func (m *Model) contextKey(msg tea.KeyPressMsg) tea.Cmd {
	k := m.keyMap()
	switch {
	case key.Matches(msg, k.Focus), key.Matches(msg, k.FocusBack):
		return m.cycleWorkspace(key.Matches(msg, k.Focus))
	case key.Matches(msg, k.Threads):
		return m.focusNavigator()
	case key.Matches(msg, k.Search):
		return m.beginSearch()
	}
	count := 0
	if m.snapshot != nil && m.snapshot.Metadata.Activity != nil {
		count = len(m.snapshot.Metadata.Activity.Items)
	}
	switch msg.String() {
	case "esc":
		m.contextFocused = false
		return m.restoreFocus()
	case "up":
		m.contextSelection = max(0, m.contextSelection-1)
		m.reviewContextSelection()
	case "down":
		m.contextSelection = min(max(0, count-1), m.contextSelection+1)
		m.reviewContextSelection()
	case "enter":
		if count == 0 && m.status.Activity {
			return m.execute("activity", "")
		}
		if count > 0 {
			item := m.snapshot.Metadata.Activity.Items[min(m.contextSelection, count-1)]
			if item.Key != m.contextFocusKey {
				m.notice = "Activity changed; select it again"
				return nil
			}
			return m.execute("activity", item.Key)
		}
	}
	return nil
}

// Calls here are reads/local navigation except the explicit pre-existing
// source-bound Settings, Requests and Stop flows.
func (m *Model) toolbarAction(action string) tea.Cmd {
	switch action {
	case "commands":
		return m.openPalette()
	case "context":
		return m.openContext()
	case "stop":
		return m.stop()
	default:
		return m.execute(action, "")
	}
}

func mainSeparator(theme string, height int) string {
	p := colors(theme)
	return lipgloss.NewStyle().Foreground(lipgloss.Color(p.muted)).Render(strings.TrimSuffix(strings.Repeat("│\n", max(1, height)), "\n"))
}

func (m *Model) operationLine() string {
	var parts []string
	if m.status.PendingAction {
		parts = append(parts, "Saved action")
	}
	if m.status.PendingStop {
		parts = append(parts, "Saved Stop")
	}
	if m.status.DraftSaveFailed {
		parts = append(parts, "Draft not saved")
	}
	if receipt := m.status.Receipt; receipt != nil {
		switch receipt.Phase {
		case "sending", "acknowledged", "uncertain":
			parts = append(parts, "Send "+receipt.Phase)
		}
	}
	return strings.Join(parts, " · ")
}
func (m *Model) transcriptTop() int {
	rows := 2
	if m.toolbarView(m.workspaceWidth()) != "" {
		rows++
	}
	if m.operationLine() != "" {
		rows++
	}
	return rows
}
