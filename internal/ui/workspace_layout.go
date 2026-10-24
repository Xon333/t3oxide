package ui

import (
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/charmbracelet/x/ansi"
)

// The split/focus policy is adapted from MIT t3term d1d33e89,
// src/tui/mod.rs::{columns,sidebar_width,cycle} and
// src/tui/details.rs::{layout,beside,pane_area}. Rendering uses the maintained
// Bubble Tea/Lip Gloss components; transport and local state stay t3oxide's.
func (m *Model) mainWidth() int { return max(1, m.width-m.navigatorWidth()) }
func (m *Model) contextVisible() bool {
	return m.sidebar && m.mainWidth() >= 110 && !m.browserPage && !m.launchPage
}
func (m *Model) mainOriginX() int {
	return m.navigatorWidth() + max(0, (m.mainWidth()-m.workspaceWidth())/2)
}

func (m *Model) cycleWorkspace(forward bool) tea.Cmd {
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
	m.resizeNavigator()
	return m.restoreFocus()
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
	if !m.navigator.loaded || m.navigator.profile != m.status.ProfileID || m.status.ThreadID == "" {
		return
	}
	for i, raw := range m.navigator.list.Items() {
		item, ok := raw.(navigatorItem)
		if !ok || item.thread.ID != m.status.ThreadID {
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
	actions := []struct{ label, action string }{{"Actions", "commands"}, {"Settings", "settings"}}
	if m.status.Requests > 0 {
		actions = append(actions, struct{ label, action string }{"Requests", "requests"})
	}
	if m.status.Activity {
		actions = append(actions, struct{ label, action string }{"Activity", "activity"})
	}
	if m.snapshot != nil && m.snapshot.Metadata.HasMore {
		actions = append(actions, struct{ label, action string }{"Earlier", "earlier"})
	}
	if m.snapshot != nil && m.snapshot.Metadata.Historical {
		actions = append(actions, struct{ label, action string }{"Latest", "latest"})
	}
	actions = append(actions, struct{ label, action string }{"Context", "context"})
	if m.status.ActiveRunID != "" {
		actions = append(actions, struct{ label, action string }{"Stop", "stop"})
	}
	return actions
}

func (m *Model) toolbarView(width int) string {
	p := colors(m.theme)
	var labels []string
	used := 0
	for _, a := range m.toolbarActions() {
		if used+ansi.StringWidth(a.label) > width {
			break
		}
		labels = append(labels, p.plain().Render(a.label))
		used += ansi.StringWidth(a.label) + 2
	}
	return strings.Join(labels, "  ")
}

func (m *Model) contextFacts() []string {
	loc := m.status.Location
	env := loc.Environment
	if env == "" {
		env = m.status.ProfileID
	}
	title := loc.Title
	if title == "" {
		title = m.status.ThreadID
	}
	rows := []string{env, title}
	if loc.Workspace != "" {
		rows = append(rows, loc.Workspace)
	}
	access := "Read only"
	if m.status.Writable {
		access = "Writes enabled"
	}
	rows = append(rows, m.status.ModelName, m.authoritativeStatus(), access, m.connectionStatus())
	if m.snapshot != nil {
		if r := m.snapshot.Metadata.RuntimeDetails; r != nil {
			if r.RunModelSelection != nil {
				rows = append(rows, "Requested: "+r.RunModelSelection.Summary())
			}
			if r.ProviderReportedSelection != nil {
				rows = append(rows, "Reported: "+r.ProviderReportedSelection.Summary())
			}
		}
	}
	return rows
}

func (m *Model) openContext() tea.Cmd {
	if m.mainWidth() >= 110 {
		m.sidebar = !m.sidebar
		m.contextFocused = m.sidebar
		m.navigator.focused = false
		m.transcriptFocus = false
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
	p := colors(m.theme)
	rows := []string{p.active().Render("Context")}
	for _, value := range m.contextFacts() {
		if value != "" {
			rows = append(rows, p.plain().Render(fitLine(safeText(value), width)))
		}
	}
	if m.snapshot != nil && m.snapshot.Metadata.Activity != nil {
		activity := m.snapshot.Metadata.Activity
		rows = append(rows, "", p.active().Render("Activity"))
		for i, item := range activity.Items {
			if len(rows) >= height-1 {
				break
			}
			text := fitLine(activityLabel(item)+" · "+item.Status, width)
			style := p.plain()
			if m.contextFocused && i == m.contextSelection {
				style = p.selection()
			}
			rows = append(rows, style.Render(text))
		}
		if activity.HasMore {
			rows = append(rows, p.subtle().Render("Earlier activity · Actions"))
		} else if activity.Limited {
			rows = append(rows, p.subtle().Render("Summaries shortened"))
		}
	}
	if m.status.Activity && (m.snapshot == nil || m.snapshot.Metadata.Activity == nil || len(m.snapshot.Metadata.Activity.Items) == 0) {
		rows = append(rows, "", p.plain().Render("Activity"))
	}
	return padBlock(strings.Join(rows, "\n"), width, height)
}

func (m *Model) contextKey(msg tea.KeyPressMsg) tea.Cmd {
	count := 0
	if m.snapshot != nil && m.snapshot.Metadata.Activity != nil {
		count = len(m.snapshot.Metadata.Activity.Items)
	}
	switch msg.String() {
	case "esc":
		m.contextFocused = false
		return m.restoreFocus()
	case "tab", "shift+tab":
		return m.cycleWorkspace(msg.String() == "tab")
	case "up":
		m.contextSelection = max(0, m.contextSelection-1)
	case "down":
		m.contextSelection = min(max(0, count-1), m.contextSelection+1)
	case "enter":
		if count == 0 && m.status.Activity {
			return m.execute("activity", "")
		}
		if count > 0 {
			return m.execute("activity", m.snapshot.Metadata.Activity.Items[min(m.contextSelection, count-1)].Key)
		}
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

// Calls here are reads/local navigation except the explicit pre-existing
// source-bound Settings, Requests and Stop flows.
func (m *Model) toolbarAction(action string) tea.Cmd {
	m.navigator.focused = false
	m.contextFocused = false
	m.activityFocused = false
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
	if receipt := m.status.Receipt; receipt != nil && receipt.Phase != "" {
		parts = append(parts, "Send "+receipt.Phase)
	}
	return strings.Join(parts, " · ")
}
func (m *Model) transcriptTop() int {
	if m.operationLine() != "" {
		return 4
	}
	return 3
}
