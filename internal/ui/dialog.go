package ui

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
)

// Filtering owns the query editor, but the current result remains selected.
// Reuse the native delegate's rendering with its public applied-filter state;
// the real list, cursor, editor and synchronous filtering remain untouched.
type compactChoiceDelegate struct{ list.DefaultDelegate }

func (d compactChoiceDelegate) Render(w io.Writer, model list.Model, index int, item list.Item) {
	if model.FilterState() == list.Filtering {
		selected := model.Index()
		model.SetFilterState(list.FilterApplied)
		model.Select(selected)
	}
	d.DefaultDelegate.Render(w, model, index, item)
}

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
func (i choiceItem) FilterValue() string {
	aliases := ""
	if i.command == "help" {
		aliases = " keys"
	}
	if i.command != "" {
		// Internal search fields keep exact command/title matches ahead of
		// incidental description matches. The native delegate renders the
		// separate Title and Description methods, never this search value.
		return safeText(strings.TrimSpace(i.command+" "+i.args+aliases)) + "\t" + safeText(i.title) + "\t" + safeText(i.description)
	}
	return safeText(i.command + " " + i.args + aliases + " " + i.title + " " + i.description)
}

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
	actionFocus               int // 0 is the native list/editor; 1..N visible page controls
	surface                   client.Surface
	launch                    *launchForm
	resultsFocused            bool
	settingsForm              *modelSettingsForm
	requestForm               *requestForm
	paletteSource             *dialog
	paletteReturn             *paletteReturn
	formSource                *dialog
	browserSource             *dialog // exact suspended source for local More
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
	// A new client prompt supersedes any local browser overlay. Never leave
	// an old More picker capable of answering a replacement prompt.
	m.browserDialog = nil
	q := p.Question
	d := &dialog{prompt: p, kind: q.Kind, title: q.Title, description: q.Prompt, selected: make(map[string]bool), surface: q.Surface}
	m.dialogSerial++
	d.serial = m.dialogSerial
	if q.Surface == client.SurfaceBrowser {
		m.browserPage = true
		if q.BrowserPage != nil {
			d.description = q.BrowserPage.Error
			snapshot := "Snapshot"
			if q.BrowserPage.More {
				snapshot += " · more available outside this page"
			}
			if q.BrowserPage.ObservedAt != "" {
				snapshot += " · observed " + q.BrowserPage.ObservedAt
			}
			if q.BrowserPage.Lifecycle != "" {
				snapshot += " · " + q.BrowserPage.Lifecycle
			}
			if q.BrowserPage.Cursor > 0 {
				snapshot += fmt.Sprintf(" · offset %d", q.BrowserPage.Cursor)
			}
			if d.description != "" {
				snapshot += " · " + d.description
			}
			d.description = snapshot
			if q.BrowserPage.More {
				if d.description != "" {
					d.description += " · "
				}
				d.description += "More conversations outside this page · Next page or Search"
			}
		}
	}
	if q.Surface == client.SurfaceLaunch && q.LaunchReview != nil {
		m.launchPage = true
		d.launch = newLaunchForm(q.LaunchReview)
	}
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
		if q.SingleLine && strings.ContainsAny(q.Initial, "\r\n\t") {
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
			m.modalInput.Placeholder = "Original answer preserved · " + m.actionBinding("info").Help().Key + " Info"
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
		if q.Surface == client.SurfaceBrowser {
			m.restoreBrowserState(d)
		}
	}
	m.dialog = d
	if q.ModelSettings != nil {
		m.initModelSettings(q.ModelSettings)
	}
	if len(q.RequestQuestions) > 0 || q.ConfirmChoice {
		m.initRequestForm(q)
	}
	if d.launch != nil {
		m.initLaunchEditors()
	}
	m.detail = ""
	m.searching = false
	m.composer.Blur()
	m.refreshDialogDetails()
	m.resize()
}

func (m *Model) cancelDialog() tea.Cmd {
	if err := m.storeTextDraft(); err != nil {
		m.problem = "Draft save: " + safeText(err.Error())
		return nil
	}
	if m.dialog != nil && m.dialog.formSource != nil {
		return m.restoreSettingsForm()
	}
	if m.dialog != nil && m.dialog.local == "palette" && (m.dialog.paletteSource != nil || m.dialog.paletteReturn != nil) {
		return m.restorePaletteSource()
	}
	if m.dialog != nil && (m.dialog.settingsForm != nil || m.dialog.requestForm != nil) {
		return m.cancelForm()
	}
	if len(m.localBack) > 0 && m.browserDialog == nil {
		if m.dialog != nil && m.dialog.prompt != nil {
			m.dialog.prompt.Respond(client.Answer{Cancelled: true})
		}
		return m.popLocalSurface()
	}
	if m.dialog != nil && m.dialog.prompt == nil && m.browserDialog != nil {
		if m.dialog.local == "browser-more" && !m.browserMoreSourceLive() {
			return m.retireBrowserMore()
		}
		m.dialog, m.browserDialog = m.browserDialog, nil
		m.resize()
		return m.restoreFocus()
	}
	if m.dialog != nil && m.dialog.prompt != nil {
		answer := client.Answer{Cancelled: true}
		if m.dialog.launch != nil {
			input := m.launchInput()
			answer.Launch = &input
		}
		m.dialog.prompt.Respond(answer)
	}
	m.dialog = nil
	if !m.actionBusy && !m.browserBusy {
		m.browserPage = false
		m.launchPage = false
	}
	m.resize()
	oldTheme := m.theme
	m.refreshAppearance(m.status.ProfileID, m.status.ProfileIDs)
	if oldTheme != m.theme {
		return tea.Batch(m.restoreFocus(), m.requestRender(false, false))
	}
	return m.restoreFocus()
}

func (m *Model) chooseDialog() tea.Cmd {
	if m.dialog == nil {
		return nil
	}
	d := m.dialog
	if d.settingsForm != nil {
		return m.applyModelSettings()
	}
	if d.requestForm != nil {
		return m.advanceRequest(false)
	}
	if d.launch != nil {
		return m.launchAction("create")
	}
	if d.kind == client.EnterText {
		if d.preservedInitial != "" {
			m.problem = "Original answer preserved · " + m.actionBinding("info").Help().Key + " Info · " + m.actionBinding("copy").Help().Key + " copy · Ctrl+U replace"
			return nil
		}
		if len(m.modalInput.Value()) > 64*1024 {
			m.problem = "Answer exceeds 64 KiB"
			return nil
		}
		if err := m.storeTextDraft(); err != nil {
			m.problem = "Draft save: " + safeText(err.Error())
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
	if d.formSource != nil {
		return m.chooseModelSetting(selected)
	}
	if d.local == "browser-more" {
		if !m.browserMoreSourceLive() {
			return m.retireBrowserMore()
		}
		if selected.command == "browser-more-help" {
			m.openDetail("Help", m.helpText())
			return nil
		}
		if selected.command != "browser-more-action" || !slices.Contains(d.browserSource.prompt.Question.BrowserPage.Actions, selected.id) {
			m.notice = "This browser action is no longer offered"
			return nil
		}
		m.dialog, m.browserDialog = d.browserSource, nil
		m.resize()
		return m.browserAction(selected.id)
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
		m.saveBrowserState(d)
		answer := client.Answer{ChoiceIDs: []string{selected.id}}
		if !d.prompt.Respond(answer) {
			m.notice = "This request was retired; no answer sent"
		}
		m.dialog = nil
		m.resize()
		return m.restoreFocus()
	}
	if d.local == "palette" && (d.paletteSource != nil || d.paletteReturn != nil) {
		// Copy consumes the inspected source. Other deliberate actions leave
		// the suspended local inspection/search; Esc alone restores it.
		if selected.command != "copy" {
			d.paletteReturn = nil
		}
		m.restorePaletteSource()
		return m.dispatch(selected.command, selected.args)
	}
	if m.browserDialog == nil && d.local != "theme" && d.local != "recall" && d.local != "" && d.local != "palette" && !strings.HasPrefix(d.local, "navigator-") && !strings.HasPrefix(d.local, "inbox-") && d.local != "context" && selected.command != "" {
		m.pushLocalSurface(d)
	}
	m.dialog = nil
	m.resize()
	focus := m.restoreFocus()
	if selected.command != "" {
		if m.browserDialog != nil && selected.command == "browser-settings" {
			m.dialog, m.browserDialog = m.browserDialog, nil
			return m.navigationAction("settings")
		}
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
	if d.surface == client.SurfaceBrowser && len(answer.ChoiceIDs) == 0 {
		m.notice = "Select at least one environment"
		return nil
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
	source := m.dialog
	items := m.paletteItems()
	d := &dialog{kind: client.ChooseOne, title: "Commands", local: "palette", selected: make(map[string]bool)}
	m.dialogSerial++
	d.serial = m.dialogSerial
	d.paletteSource = source
	if m.detail != "" || m.searching {
		d.paletteReturn = &paletteReturn{generation: m.generation, searching: m.searching, detail: m.detail, title: m.detailTitle, copy: m.detailCopy, viewport: m.detailViewport, isActivity: m.detailIsActivity, detailGeneration: m.detailGeneration}
	}
	if m.navigator.focused && m.navigator.profile != m.status.ProfileID && m.status.ThreadID != "" {
		d.title = "Commands · " + m.attachedTargetStatus()
	}
	d.list = m.newList("Commands", items)
	d.list.Filter = commandFilter
	d.list.SetFilterText("")
	d.list.SetFilterState(list.Filtering)
	m.dialog = d
	m.detail, m.detailTitle, m.detailCopy = "", "", ""
	m.detailIsActivity, m.detailGeneration = false, 0
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
		choiceItem{title: "Environment default", command: "theme", args: "default"},
		choiceItem{title: "Terminal", description: "Your terminal colors · light or dark", command: "theme", args: "terminal"},
		choiceItem{title: "Electric violet", command: "theme", args: "violet"},
		choiceItem{title: "Electric cyan", command: "theme", args: "cyan"},
		choiceItem{title: "Acid lime", command: "theme", args: "lime"},
	}
	d := &dialog{kind: client.ChooseOne, title: "Theme · " + m.appearanceProfile(), local: "theme"}
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
	if key.Matches(msg, m.actionBinding("info")) {
		return m.dispatch("info", "")
	}
	if key.Matches(msg, m.actionBinding("dismiss")) {
		if m.dialog.details {
			return m.dispatch("info", "")
		}
		return m.cancelDialog()
	}
	if m.dialog.settingsForm != nil && !m.dialog.details {
		return m.modelSettingsKey(msg)
	}
	if m.dialog.requestForm != nil && !m.dialog.details {
		return m.requestFormKey(msg)
	}
	if m.dialog.launch != nil && !m.dialog.details {
		return m.launchKey(msg)
	}
	if m.dialog.details {
		switch msg.String() {
		case "esc", "enter":
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
	if m.dialog.kind == client.EnterText {
		switch msg.String() {
		case "tab", "shift+tab":
			delta := 1
			if msg.String() == "shift+tab" {
				delta = -1
			}
			m.dialog.actionFocus = (m.dialog.actionFocus + delta + 3) % 3
			return m.restoreFocus()
		case "enter":
			if m.dialog.actionFocus == 2 {
				return m.cancelDialog()
			}
			return m.chooseDialog()
		}
		if m.dialog.actionFocus > 0 {
			switch msg.String() {
			case "left", "up":
				m.dialog.actionFocus = 1
			case "right", "down":
				m.dialog.actionFocus = 2
			}
			return nil
		}
	}
	if m.dialog.surface == client.SurfaceBrowser && m.dialog.kind != client.ChooseMany && key.Matches(msg, m.actionBinding("browser.search")) {
		return m.browserAction("search")
	}
	if m.dialog.prompt != nil && m.dialog.prompt.Question.Navigation && key.Matches(msg, m.actionBinding("browser.search")) {
		return m.navigationAction("search")
	}
	if m.dialog.surface == client.SurfaceBrowser && m.dialog.kind != client.ChooseMany {
		if handled, cmd := m.browserKey(msg); handled {
			return cmd
		}
	}
	if m.dialog.prompt != nil && m.dialog.prompt.Question.Navigation && (m.dialog.resultsFocused || m.dialog.list.FilterState() != list.Filtering) {
		if key.Matches(msg, m.actionBinding("browser.filter")) {
			return m.navigationAction("filter")
		}
		action := ""
		switch msg.String() {
		case "space", " ":
			action = "toggle"
		case "right":
			action = "expand"
		case "left":
			action = "collapse"
		case "alt+up":
			action = "parent"
		}
		if action != "" {
			return m.navigationAction(action)
		}
	}
	switch msg.String() {
	case "ctrl+j", "alt+enter", "shift+enter":
		if m.dialog.prompt != nil && m.dialog.prompt.Question.SingleLine {
			return nil
		}
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
		m.resize()
	}
	return m.componentCmd("list", d.serial, cmd)
}

// The compact list may elide an option at narrow widths. Keep the complete
// question and selected label/description in a Bubbles viewport, available with
// Info at every size, before any correlated answer is submitted.
func (m *Model) refreshDialogDetails() {
	if m.dialog == nil {
		return
	}
	d := m.dialog
	if d.launch != nil {
		m.refreshLaunchDetails()
		return
	}
	text := safeText(d.title) + "\n\n" + safeText(d.description)
	if d.settingsForm != nil {
		text += "\n\n" + m.modelSettingsDetails()
	}
	if d.requestForm != nil {
		f := d.requestForm
		for i, q := range f.questions {
			requirement := "Optional"
			if q.Required {
				requirement = "Required"
			}
			text += fmt.Sprintf("\n\nQuestion %d/%d: %s · %s\n%s", i+1, len(f.questions), safeText(q.Header), requirement, safeText(q.Question))
			for _, option := range q.Options {
				text += "\n" + safeText(option.Label) + " · " + safeText(option.Description)
			}
			if answer, ok := f.answers[q.ID]; ok {
				text += "\nAnswer: " + safeText(fmt.Sprint(answer))
			}
		}
	}
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
	m.detailIsActivity = false
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
	m.resize()
	return m.restoreFocus()
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
	if m.dialog == nil || m.dialog.prompt == nil || (!m.dialog.prompt.Question.Navigation && m.dialog.surface != client.SurfaceBrowser) {
		return nil
	}
	if m.dialog.surface == client.SurfaceBrowser && !slices.Contains(m.browserActions(), action) {
		return nil
	}
	answer := client.Answer{Action: action}
	if action == "search" {
		answer.Text = m.dialog.list.FilterValue()
	}
	m.saveBrowserState(m.dialog)
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
