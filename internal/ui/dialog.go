package ui

import (
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
)

type choiceItem struct {
	id, title, description, command, args string
	checked, multiple, disabled           bool
}

func (i choiceItem) Title() string {
	if i.multiple {
		if i.checked {
			return "✓ " + safeText(i.title)
		}
		return "○ " + safeText(i.title)
	}
	return safeText(i.title)
}
func (i choiceItem) Description() string { return safeText(i.description) }
func (i choiceItem) FilterValue() string { return safeText(i.title + " " + i.description) }

type dialog struct {
	serial                    uint64
	prompt                    *client.Prompt
	kind                      client.Kind
	title, description, local string
	list                      list.Model
	selected                  map[string]bool
	details                   bool
	detailText                string
	preservedInitial          string
	filterQuery               string
	finishFocused             bool
}

func (m *Model) newList(title string, items []list.Item) list.Model {
	l := list.New(items, list.NewDefaultDelegate(), m.modalWidth, max(1, m.modalHeight-5))
	l.Title = title
	l.SetShowTitle(false)
	l.SetShowFilter(false)
	l.SetShowStatusBar(false)
	l.SetShowPagination(true)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(true)
	l.DisableQuitKeybindings()
	l.FilterInput.Prompt = "Filter: "
	l.FilterInput.Placeholder = "Type to filter"
	l.FilterInput.CharLimit = 256
	l.FilterInput.SetVirtualCursor(true)
	m.styleList(&l)
	return l
}
func (m *Model) openPrompt(p *client.Prompt) {
	q := p.Question
	d := &dialog{prompt: p, kind: q.Kind, title: q.Title, description: q.Prompt, selected: make(map[string]bool)}
	m.dialogSerial++
	d.serial = m.dialogSerial
	if q.Kind == client.EnterText {
		m.modalInput.SetValue("")
		if len(q.Initial) <= maxInputBytes && strings.Count(q.Initial, "\n") < maxInputLines {
			m.modalInput.SetValue(q.Initial)
		}
		if m.modalInput.Value() != q.Initial {
			d.preservedInitial = q.Initial
			m.modalInput.SetValue("")
			m.modalInput.Blur()
		}
		m.modalInput.CursorEnd()
		m.modalInput.Placeholder = safeText(q.Placeholder)
		if q.Placeholder == "" {
			m.modalInput.Placeholder = "Your answer"
		}
		if d.preservedInitial != "" {
			m.modalInput.Placeholder = "Original answer preserved · F2 reviews it"
		}
	} else {
		items := make([]list.Item, 0, len(q.Choices))
		for _, c := range q.Choices {
			items = append(items, choiceItem{id: c.ID, title: c.Label, description: c.Description, multiple: q.Kind == client.ChooseMany})
		}
		d.list = m.newList(q.Title, items)
		for i, c := range q.Choices {
			if c.ID == q.Initial {
				d.list.Select(i)
				break
			}
		}
	}
	m.dialog = d
	m.detail = ""
	m.searching = false
	m.composer.Blur()
	m.refreshDialogDetails()
	m.resize()
}

func (m *Model) cancelDialog() tea.Cmd {
	if m.dialog != nil && m.dialog.prompt != nil {
		m.dialog.prompt.Respond(client.Answer{Cancelled: true})
		m.notice = "Dismissed locally · no answer sent to T3"
	}
	m.dialog = nil
	m.resize()
	return m.restoreFocus()
}

func (m *Model) chooseDialog() tea.Cmd {
	if m.dialog == nil {
		return nil
	}
	d := m.dialog
	if d.kind == client.EnterText {
		if d.preservedInitial != "" {
			m.problem = "Original answer preserved · F2 review · Ctrl+Y copy · Ctrl+U replace"
			return nil
		}
		if len(m.modalInput.Value()) > 64*1024 {
			m.problem = "Answer exceeds 64 KiB"
			return nil
		}
		answer := client.Answer{Text: m.modalInput.Value()}
		if d.prompt != nil && !d.prompt.Respond(answer) {
			m.notice = "This request was retired; no answer sent"
		}
		m.dialog = nil
		m.resize()
		return m.restoreFocus()
	}
	if d.list.FilterValue() != d.filterQuery {
		d.refilter(false)
		m.refreshDialogDetails()
	}
	selected, ok := d.list.SelectedItem().(choiceItem)
	if !ok {
		return nil
	}
	if selected.disabled {
		m.notice = selected.description
		return nil
	}
	if d.kind == client.ChooseMany {
		d.selected[selected.id] = !d.selected[selected.id]
		selected.checked = d.selected[selected.id]
		for i, item := range d.list.Items() {
			if row, ok := item.(choiceItem); ok && row.id == selected.id {
				// SetItem normally starts an async refilter. Compute through the
				// same synchronous path as typing so it cannot outlive this query.
				d.list.SetItem(i, selected)
				d.refilter(true)
				m.refreshDialogDetails()
				return nil
			}
		}
		return nil
	}
	if d.prompt != nil {
		answer := client.Answer{ChoiceIDs: []string{selected.id}}
		if !d.prompt.Respond(answer) {
			m.notice = "This request was retired; no answer sent"
		}
		m.dialog = nil
		m.resize()
		return m.restoreFocus()
	}
	m.dialog = nil
	m.resize()
	focus := m.restoreFocus()
	if selected.command != "" {
		return tea.Batch(focus, m.dispatch(selected.command, selected.args))
	}
	return focus
}

func (m *Model) finishMultiple() tea.Cmd {
	if m.dialog == nil || m.dialog.kind != client.ChooseMany || m.dialog.prompt == nil {
		return nil
	}
	d := m.dialog
	answer := client.Answer{}
	for _, choice := range d.prompt.Question.Choices {
		if d.selected[choice.ID] {
			answer.ChoiceIDs = append(answer.ChoiceIDs, choice.ID)
		}
	}
	if !d.prompt.Respond(answer) {
		m.notice = "This request was retired; no answer sent"
	}
	m.dialog = nil
	m.resize()
	return m.restoreFocus()
}

func (m *Model) openPalette() tea.Cmd {
	if m.dialog != nil && m.dialog.local == "palette" {
		return m.restoreFocus()
	}
	if m.dialog != nil && m.dialog.prompt != nil {
		m.openDetail("Request controls", m.helpText())
		return nil
	}
	items := m.paletteItems()
	d := &dialog{kind: client.ChooseOne, title: "Commands", local: "palette", selected: make(map[string]bool)}
	m.dialogSerial++
	d.serial = m.dialogSerial
	d.list = m.newList("Commands", items)
	d.list.SetFilterText("")
	d.list.SetFilterState(list.Filtering)
	m.dialog = d
	m.detail = ""
	m.searching = false
	m.composer.Blur()
	m.questionViewport.SetContent(d.description)
	m.questionViewport.GotoTop()
	m.refreshDialogDetails()
	m.resize()
	return m.restoreFocus()
}

func (m *Model) openThemes() tea.Cmd {
	items := []list.Item{
		choiceItem{title: "Terminal", description: "Your terminal colors · light or dark", command: "theme", args: "terminal"},
		choiceItem{title: "Electric violet", command: "theme", args: "violet"},
		choiceItem{title: "Electric cyan", command: "theme", args: "cyan"},
		choiceItem{title: "Acid lime", command: "theme", args: "lime"},
	}
	d := &dialog{kind: client.ChooseOne, title: "Theme", local: "theme"}
	m.dialogSerial++
	d.serial = m.dialogSerial
	d.list = m.newList("Theme", items)
	m.dialog = d
	m.composer.Blur()
	m.questionViewport.SetContent(d.description)
	m.questionViewport.GotoTop()
	m.refreshDialogDetails()
	m.resize()
	return nil
}

func (m *Model) dialogKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.dialog == nil {
		return nil
	}
	if m.dialog.details {
		switch msg.String() {
		case "esc", "f2", "enter":
			m.dialog.details = false
			m.questionViewport.SetContent(safeText(m.dialog.description))
			m.resize()
			m.questionViewport.GotoTop()
			return m.restoreFocus()
		case "up", "ctrl+up", "k":
			m.questionViewport.ScrollUp(1)
		case "down", "ctrl+down", "j":
			m.questionViewport.ScrollDown(1)
		case "pgup":
			m.questionViewport.ScrollUp(m.questionViewport.Height())
		case "pgdown":
			m.questionViewport.ScrollDown(m.questionViewport.Height())
		case "home", "g":
			m.questionViewport.GotoTop()
		case "end", "G":
			m.questionViewport.GotoBottom()
		}
		return nil
	}
	if m.dialog.prompt != nil && m.dialog.prompt.Question.Navigation && m.dialog.list.FilterState() != list.Filtering {
		action := ""
		switch msg.String() {
		case "space", " ":
			action = "toggle"
		case "right":
			action = "expand"
		case "left":
			action = "collapse"
		case "f6":
			action = "filter"
		case "ctrl+f":
			action = "search"
		case "alt+up":
			action = "parent"
		}
		if action != "" {
			return m.navigationAction(action)
		}
	}
	switch msg.String() {
	case "tab", "shift+tab":
		if m.dialog.kind == client.ChooseMany {
			m.dialog.finishFocused = !m.dialog.finishFocused
			cmd := m.restoreFocus()
			if m.dialog.finishFocused {
				m.dialog.list.FilterInput.Blur()
			}
			return cmd
		}
	case "ctrl+u":
		if m.dialog.preservedInitial != "" {
			m.dialog.preservedInitial = ""
			m.modalInput.SetValue("")
			m.modalInput.Placeholder = "Type the replacement answer"
			m.problem = ""
			m.notice = "Original answer cleared locally; type a replacement before Enter"
			m.refreshDialogDetails()
			return m.restoreFocus()
		}
	case "f2":
		m.dialog.details = true
		m.questionViewport.SetContent(m.dialog.detailText)
		m.resize()
		m.questionViewport.GotoTop()
		return m.restoreFocus()
	case "esc":
		return m.cancelDialog()
	case "ctrl+up":
		m.questionViewport.ScrollUp(1)
		return nil
	case "ctrl+down":
		m.questionViewport.ScrollDown(1)
		return nil
	case "ctrl+enter":
		if m.dialog.kind == client.ChooseMany {
			return m.finishMultiple()
		}
		return m.chooseDialog()
	case "enter":
		if m.dialog.kind == client.ChooseMany && m.dialog.finishFocused {
			return m.finishMultiple()
		}
		return m.chooseDialog()
	case "space", " ":
		if m.dialog.kind == client.ChooseMany && !m.dialog.finishFocused && m.dialog.list.FilterState() != list.Filtering {
			return m.chooseDialog()
		}
	}
	if m.dialog.finishFocused {
		return nil
	}
	if m.dialog.kind == client.EnterText {
		return m.updateComponent("modal", m.dialog.serial, msg)
	}
	// List navigation/filtering is provided by Bubbles, with one-step selection
	// and arrow/page navigation while the query editor owns focus.
	return m.updateComponent("list", m.dialog.serial, msg)
}

// refilter uses Bubbles' native synchronous filter API. In v2.2.1 an async
// FilterMatchesMsg contains neither the query nor an identity, so accepting it
// after another keystroke can silently select a result from an older query.
// Source: charmbracelet/bubbles@490948109eb4731927ba2f4cd464d8175a9630d7/list/list.go.
func (d *dialog) refilter(keepSelection bool) {
	state, input, index := d.list.FilterState(), d.list.FilterInput, d.list.Index()
	d.filterQuery = input.Value()
	if state == list.Unfiltered {
		return
	}
	d.list.SetFilterText(d.filterQuery)
	if state == list.Filtering {
		d.list.SetFilterState(list.Filtering)
	}
	// SetFilterText/SetFilterState move the input cursor to its end. Restore
	// the real editor state so editing in the middle of a query stays natural.
	d.list.FilterInput = input
	if keepSelection {
		d.list.Select(max(0, min(index, len(d.list.VisibleItems())-1)))
	}
}

// updateDialogList is the list component boundary used by updateComponent.
// Component commands still carry the originating attachment/modal identity;
// filtering itself stays on the event loop, with no stale result to commit.
func (m *Model) updateDialogList(message tea.Msg) tea.Cmd {
	d := m.dialog
	if d == nil || d.kind == client.EnterText || d.details || d.finishFocused || m.detail != "" || !m.focused {
		return nil
	}
	if _, stale := message.(list.FilterMatchesMsg); stale {
		return nil
	}
	if msg, ok := message.(tea.KeyPressMsg); ok {
		if d.list.FilterState() != list.Filtering {
			if msg.String() == "/" {
				d.list.SetFilterState(list.Filtering)
				d.refilter(false)
				m.refreshDialogDetails()
				return m.componentCmd("list", d.serial, d.list.FilterInput.Focus())
			}
			// Every printable letter starts a query, including j/k. Arrow keys
			// navigate consistently before and during filtering.
			if msg.Text != "" && msg.Text != " " {
				d.list.SetFilterState(list.Filtering)
				d.refilter(false)
			}
		}
		if d.list.FilterState() == list.Filtering {
			if d.list.FilterValue() != d.filterQuery {
				d.refilter(false)
			}
			switch msg.String() {
			case "up", "ctrl+p":
				d.list.CursorUp()
			case "down", "ctrl+n":
				d.list.CursorDown()
			case "pgup":
				d.list.PrevPage()
			case "pgdown":
				d.list.NextPage()
			default:
				return m.updateDialogFilter(message)
			}
			m.refreshDialogDetails()
			return nil
		}
	}
	if _, paste := message.(tea.PasteMsg); paste && d.list.FilterState() != list.Filtering {
		d.list.SetFilterState(list.Filtering)
		d.refilter(false)
	}
	if d.list.FilterState() == list.Filtering {
		return m.updateDialogFilter(message)
	}
	var cmd tea.Cmd
	d.list, cmd = d.list.Update(message)
	m.refreshDialogDetails()
	return m.componentCmd("list", d.serial, cmd)
}

func (m *Model) updateDialogFilter(message tea.Msg) tea.Cmd {
	d := m.dialog
	if !d.list.FilterInput.Focused() {
		return nil
	}
	previous := d.list.FilterValue()
	var cmd tea.Cmd
	d.list.FilterInput, cmd = d.list.FilterInput.Update(message)
	if d.list.FilterValue() != previous {
		d.refilter(false)
		m.refreshDialogDetails()
	}
	return m.componentCmd("list", d.serial, cmd)
}

// The compact list may elide an option at narrow widths. Keep the complete
// question and selected label/description in a Bubbles viewport, available with
// F2 at every size, before any correlated answer is submitted.
func (m *Model) refreshDialogDetails() {
	if m.dialog == nil {
		return
	}
	d := m.dialog
	text := safeText(d.title) + "\n\n" + safeText(d.description)
	if d.preservedInitial != "" {
		text += "\n\nPreserved initial answer:\n" + safeText(d.preservedInitial)
	}
	if d.kind != client.EnterText {
		if item, ok := d.list.SelectedItem().(choiceItem); ok {
			text += "\n\nSelected: " + safeText(item.title)
			if item.description != "" {
				text += "\n" + safeText(item.description)
			}
		}
	}
	if d.kind == client.ChooseMany && d.prompt != nil {
		text += "\n\nSelected answers:"
		for _, choice := range d.prompt.Question.Choices {
			if d.selected[choice.ID] {
				text += "\n" + safeText(choice.Label) + "\nID: " + safeText(choice.ID)
				if choice.Description != "" {
					text += "\n" + safeText(choice.Description)
				}
			}
		}
	}
	if text != d.detailText {
		d.detailText = text
		if d.details {
			m.questionViewport.SetContent(text)
			m.questionViewport.GotoTop()
		} else {
			m.questionViewport.SetContent(safeText(d.description))
		}
	}
}

func (m *Model) openDetail(title, text string) {
	m.detailTitle = safeText(title)
	m.detail = safeText(text)
	m.detailCopy = redactFeedback(text)
	m.detailViewport.SetContent(m.detail)
	m.detailViewport.GotoTop()
	m.composer.Blur()
	m.resize()
	m.restoreFocus()
}

func (m *Model) beginSearch() tea.Cmd {
	m.searching = true
	m.searchInput.SetValue(m.searchTerm)
	m.searchInput.CursorEnd()
	m.composer.Blur()
	m.resize()
	return m.componentCmd("search", 0, m.searchInput.Focus())
}
func (m *Model) refreshSearch(move bool) {
	if m.searchTerm == "" {
		m.searchLines = nil
		m.searchIndex = -1
		m.viewport.StyleLineFunc = nil
		return
	}
	term := strings.ToLower(m.searchTerm)
	m.searchLines = nil
	for index, line := range strings.Split(safeText(m.transcript), "\n") {
		if strings.Contains(strings.ToLower(line), term) {
			m.searchLines = append(m.searchLines, index)
		}
	}
	if len(m.searchLines) == 0 {
		m.searchIndex = -1
		m.notice = "No match in this page"
		m.viewport.StyleLineFunc = nil
		return
	}
	if m.searchIndex < 0 || m.searchIndex >= len(m.searchLines) {
		m.searchIndex = 0
	}
	if move {
		m.viewport.SetYOffset(max(0, m.searchLines[m.searchIndex]-2))
		m.follow = false
	}
	m.highlightSearchLine()
}

func (m *Model) navigationAction(action string) tea.Cmd {
	if m.dialog == nil || m.dialog.prompt == nil || !m.dialog.prompt.Question.Navigation {
		return nil
	}
	answer := client.Answer{Action: action}
	if item, ok := m.dialog.list.SelectedItem().(choiceItem); ok {
		answer.ChoiceIDs = []string{item.id}
	}
	if !m.dialog.prompt.Respond(answer) {
		m.notice = "This navigator was retired"
	}
	m.dialog = nil
	m.resize()
	return m.restoreFocus()
}
