package ui

import (
	"path"
	"strings"

	"charm.land/bubbles/v2/key"
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
		return 2 // a quiet separating row, then the compact activity itself
	}
	return 0
}

func (m *Model) activityStripView(width int) string {
	item := m.recentActivity()
	if item == nil {
		return ""
	}
	text := navigatorStatus(item.Status) + " " + activityLabel(*item)
	if item.Truncated {
		text += " …"
	}
	style := colors(m.theme).operational()
	if m.activityFocused {
		text = "> " + text
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
	return m.resizeWorkspaceFocus()
}

func (m *Model) activityKey(msg tea.KeyPressMsg) tea.Cmd {
	k := m.keyMap()
	switch {
	case key.Matches(msg, k.Focus), key.Matches(msg, k.FocusBack):
		return m.cycleWorkspace(key.Matches(msg, k.Focus))
	case key.Matches(msg, k.Threads):
		return m.focusNavigator()
	case key.Matches(msg, k.Search):
		return m.beginSearch()
	}
	switch msg.String() {
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
	}
	return nil
}

func activityLabel(item wire.ActivityItem) string {
	summary, _, _ := strings.Cut(safeText(item.Summary), "\n")
	title := safeText(item.Title)
	if item.Kind == "command_execution" || strings.EqualFold(title, "command") {
		// This is display-only lexical shortening, deliberately not a shell
		// parser. Quotes, variables and command strings are never evaluated.
		fields := strings.Fields(summary)
		if len(fields) > 0 && !strings.ContainsAny(fields[0], "=\"'`$;") {
			return path.Base(fields[0])
		}
		return "Command"
	}
	if strings.EqualFold(title, "tool") || title == "" {
		title = summary
	}
	if head, _, found := strings.Cut(title, " · "); found {
		title = head
	}
	if item.Kind == "dynamic_tool" {
		if fields := strings.Fields(title); len(fields) > 0 {
			title = fields[0]
		}
		if strings.HasPrefix(title, "mcp__") {
			parts := strings.Split(title, "__")
			title = parts[len(parts)-1]
		}
	}
	if title == "" {
		title = strings.ReplaceAll(item.Kind, "_", " ")
	}
	return title
}
