package ui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

// Codex's inspected composer keeps suggestions above the editor and remembers
// Escape dismissal for the current slash token. Here Bubbles owns filtering,
// selection and pagination; only that small composer integration is adapted.
type commandCompletion struct {
	list             list.Model
	token, dismissed string
	active           bool
}

func (m *Model) refreshCompletion() {
	text := m.currentDraft()
	c := &m.completion
	if m.preservedDraft != "" || !strings.HasPrefix(text, "/") || strings.ContainsAny(text, "\r\n") {
		*c = commandCompletion{}
		return
	}
	token, _, _ := strings.Cut(strings.TrimPrefix(text, "/"), " ")
	if token == c.dismissed && c.dismissed != "" || token == "" && c.dismissed == "/" {
		c.active = false
		return
	}
	if token != c.token {
		c.dismissed = ""
	}
	if c.active && token == c.token {
		return
	}
	var items []list.Item
	for _, spec := range m.commandCatalog() {
		if !strings.HasPrefix(spec.name, token) {
			continue
		}
		title := "/" + spec.name
		if spec.arguments != "" {
			title += " " + spec.arguments
		}
		items = append(items, choiceItem{title: title, command: spec.name})
	}
	c.token, c.active = token, len(items) > 0
	if !c.active {
		return
	}
	d := list.NewDefaultDelegate()
	d.ShowDescription = false
	d.SetSpacing(0)
	c.list = list.New(items, d, max(1, m.contentWidth-4), 4)
	c.list.SetShowTitle(false)
	c.list.SetShowStatusBar(false)
	c.list.SetShowFilter(false)
	c.list.SetShowHelp(false)
	c.list.SetShowPagination(false)
	c.list.DisableQuitKeybindings()
	c.list.SetFilterText(token)
}

func (m *Model) completionVisible() bool {
	return m.width >= 32 && m.height >= 12 && m.completion.active && m.detail == "" && m.dialog == nil && !m.searching && !m.transcriptFocus && !m.navigator.focused && !m.contextFocused && !m.activityFocused && m.preservedDraft == ""
}

func (m *Model) dismissCompletion() {
	if m.completion.active {
		m.completion.dismissed = m.completion.token
		if m.completion.token == "" {
			m.completion.dismissed = "/"
		}
		m.completion.active = false
	}
}

func (m *Model) completionKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	m.refreshCompletion()
	if !m.completionVisible() {
		return false, nil
	}
	switch {
	case key.Matches(msg, m.composer.KeyMap.LinePrevious):
		m.completion.list.CursorUp()
		return true, nil
	case key.Matches(msg, m.composer.KeyMap.LineNext):
		m.completion.list.CursorDown()
		return true, nil
	}
	switch msg.String() {
	case "esc":
		m.dismissCompletion()
		return true, nil
	case "tab":
		m.acceptCompletion()
		return true, nil
	case "enter":
		// Exact command names execute only through normal validation. A partial
		// suggestion is accepted into the draft first, never executed implicitly.
		if !m.isSlashCommand(m.currentDraft()) {
			m.acceptCompletion()
			return true, nil
		}
	}
	return false, nil
}

func (m *Model) acceptCompletion() {
	item, ok := m.completion.list.SelectedItem().(choiceItem)
	if !ok {
		return
	}
	old := m.composer.Value()
	_, suffix, hasSuffix := strings.Cut(old, " ")
	text := "/" + item.command + " "
	if hasSuffix {
		text += suffix
	}
	m.composer.SetValue(text)
	m.composer.CursorEnd()
	m.composerChanged(old)
	m.completion.token = item.command
	m.completion.dismissed = item.command
	m.completion.active = false
}

func (m *Model) completionView(width int) string {
	if !m.completionVisible() {
		return ""
	}
	l := m.completion.list
	l.SetSize(max(1, width-4), min(len(l.VisibleItems()), min(4, max(1, m.height/6))))
	m.styleList(&l)
	keys := contextHelp{short: []key.Binding{bind("Tab", "complete", "tab"), m.keyMap().Literal}}
	return framed("Commands", l.View()+"\n"+m.bindingHelp(width-4, keys), width-4, colors(m.theme), true)
}
