package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/wire"
)

// One current/recent source-owned tool is visible without opening another
// surface. Summaries are already in the attached bounded page; this view never
// issues a read. Explicit inspection uses its exact source-qualified key.
func (m *Model) recentActivity() *wire.ActivityItem {
	if !m.status.Activity || m.snapshot == nil || m.snapshot.Metadata.Activity == nil {
		return nil
	}
	items := m.snapshot.Metadata.Activity.Items
	if len(items) == 0 {
		return nil
	}
	for i := len(items) - 1; i >= 0; i-- {
		switch items[i].Status {
		case "running", "active", "in_progress", "inProgress", "pending", "waiting":
			return &items[i]
		}
	}
	return &items[len(items)-1]
}

func (m *Model) activityStripRows() int {
	if m.recentActivity() != nil {
		return 1
	}
	return 0
}

func (m *Model) activityStripView(width int) string {
	item := m.recentActivity()
	if item == nil {
		return ""
	}
	kind := strings.ReplaceAll(item.Kind, "_", " ")
	switch item.Kind {
	case "command_execution":
		kind = "Command"
	case "file_change":
		kind = "Files"
	case "dynamic_tool":
		kind = "Tool"
	case "subagent":
		kind = "Subagent"
	case "web_search":
		kind = "Web search"
	case "file_search":
		kind = "File search"
	case "todo_list":
		kind = "Tasks"
	}
	text := kind + " · " + item.Status + "  " + activityLabel(*item)
	if item.Truncated {
		text += " …"
	}
	style := colors(m.theme).operational()
	if m.activityFocused {
		style = colors(m.theme).selection()
	}
	return style.Render(fitLine(text, width))
}

func (m *Model) focusActivity() tea.Cmd {
	m.activityFocused = true
	m.navigator.focused = false
	m.contextFocused = false
	m.transcriptFocus = false
	if item := m.recentActivity(); item != nil {
		m.activityFocusKey = item.Key
	}
	m.resizeNavigator()
	return m.restoreFocus()
}

func (m *Model) activityKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "tab", "shift+tab":
		return m.cycleWorkspace(msg.String() == "tab")
	case "esc":
		m.activityFocused = false
		m.transcriptFocus = true
		return m.restoreFocus()
	case "enter":
		item := m.recentActivity()
		if item == nil || item.Key != m.activityFocusKey {
			m.notice = "Activity changed; select it again"
			return nil
		}
		return m.execute("activity", item.Key)
	case "a":
		return m.execute("activity", "")
	case "ctrl+o":
		if m.navigationVisible() {
			return m.focusNavigator()
		}
		return m.execute("switch", "")
	case "ctrl+r":
		return m.execute("requests", "")
	}
	return nil
}

func activityLabel(item wire.ActivityItem) string {
	summary, _, _ := strings.Cut(safeText(item.Summary), "\n")
	title := safeText(item.Title)
	if summary == "" {
		return title
	}
	switch strings.ToLower(title) {
	case "command", "file change", "file search", "web search", "subagent", "tasks", "tool":
		return summary
	}
	if title == summary {
		return title
	}
	return title + " · " + summary
}
