package ui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
)

// Browsing is local metadata work. Its cancellation never cancels a send which
// is still waiting for a receipt, and Stop retains its existing independent lane.
func (m *Model) executeBrowser(name, args string) tea.Cmd {
	var previous, promptDone <-chan struct{}
	if m.browserBusy {
		if m.cancelBrowser != nil {
			m.cancelBrowser()
		}
		previous = m.browserDone
	}
	if m.dialog != nil && m.dialog.prompt != nil {
		promptDone = m.dialog.prompt.Done
		m.dialog.prompt.Respond(client.Answer{Cancelled: true})
		m.dialog = nil
	}
	m.browserID++
	id, generation, session := m.browserID, m.generation, m.session
	ctx, cancel := context.WithCancel(m.sessionCtx)
	m.cancelBrowser = cancel
	done := make(chan struct{})
	m.browserDone = done
	m.browserBusy = true
	m.browserPage = true
	m.composer.Blur()
	m.resize()
	return func() tea.Msg {
		defer close(done)
		defer cancel()
		for _, retired := range []<-chan struct{}{previous, promptDone} {
			if retired != nil {
				select {
				case <-retired:
				case <-ctx.Done():
					return actionMsg{generation: generation, id: id, name: name, browser: true, err: ctx.Err()}
				}
			}
		}
		result, err := session.Command(ctx, name, args)
		return actionMsg{generation: generation, id: id, name: name, browser: true, result: result, err: err}
	}
}

type browserState struct {
	query     textinput.Model
	index     int
	hostQuery string
}

func (m *Model) appearanceProfile() string {
	if m.dialog != nil && m.dialog.prompt != nil && m.dialog.prompt.Question.BrowserPage != nil {
		return m.dialog.prompt.Question.BrowserPage.ProfileID
	}
	if m.browserDialog != nil && m.browserDialog.prompt.Question.BrowserPage != nil {
		return m.browserDialog.prompt.Question.BrowserPage.ProfileID
	}
	return m.status.ProfileID
}

func (m *Model) appearanceProfiles() []string {
	if len(m.status.ProfileIDs) > 0 {
		return m.status.ProfileIDs
	}
	if m.dialog != nil && m.dialog.prompt != nil && m.dialog.prompt.Question.BrowserPage != nil {
		var ids []string
		for _, profile := range m.dialog.prompt.Question.BrowserPage.Profiles {
			ids = append(ids, profile.ID)
		}
		return ids
	}
	if m.browserDialog != nil && m.browserDialog.prompt.Question.BrowserPage != nil {
		var ids []string
		for _, profile := range m.browserDialog.prompt.Question.BrowserPage.Profiles {
			ids = append(ids, profile.ID)
		}
		return ids
	}
	return m.status.ProfileIDs
}

func (m *Model) saveBrowserState(d *dialog) {
	if d == nil || d.prompt == nil || d.prompt.Question.BrowserPage == nil || d.kind == client.ChooseMany {
		return
	}
	if m.browserStates == nil {
		m.browserStates = make(map[string]browserState)
	}
	m.browserStates[d.prompt.Question.BrowserPage.ProfileID] = browserState{d.list.FilterInput, d.list.Index(), d.prompt.Question.BrowserPage.Query}
}

func (m *Model) restoreBrowserState(d *dialog) {
	page := d.prompt.Question.BrowserPage
	if page == nil {
		return
	}
	if d.kind == client.ChooseMany {
		for i, raw := range d.list.Items() {
			item := raw.(choiceItem)
			item.checked = slices.Contains(page.ProfileIDs, item.id)
			d.selected[item.id] = item.checked
			d.list.SetItem(i, item)
		}
		return
	}
	if state, ok := m.browserStates[page.ProfileID]; ok && state.hostQuery == page.Query {
		d.list.SetFilterText(state.query.Value())
		d.list.SetFilterState(list.Filtering)
		d.list.FilterInput = state.query
		d.filterQuery = state.query.Value()
		d.list.Select(state.index)
	} else if page.Query != "" {
		d.list.SetFilterText(page.Query)
		d.list.SetFilterState(list.Filtering)
		d.filterQuery = page.Query
	}
	var configured []string
	for _, profile := range page.Profiles {
		configured = append(configured, profile.ID)
	}
	if len(m.status.ProfileIDs) > 0 {
		configured = m.status.ProfileIDs
	}
	m.refreshAppearance(page.ProfileID, configured)
}

func (m *Model) browserActions() []string {
	if m.dialog == nil || m.dialog.prompt == nil || m.dialog.prompt.Question.BrowserPage == nil {
		return nil
	}
	return m.dialog.prompt.Question.BrowserPage.Actions
}

func actionLabel(action string) string {
	if action == "" {
		return ""
	}
	if id, ok := strings.CutPrefix(action, "profile:"); ok {
		return id
	}
	switch action {
	case "environments":
		return "Environments"
	case "new":
		return "New"
	case "settings":
		return "Settings"
	case "resume":
		return "Resume"
	case "refresh":
		return "Refresh"
	case "types":
		return "Agents / threads"
	}
	return strings.ToUpper(action[:1]) + action[1:]
}

func (m *Model) browserKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	d, actions := m.dialog, m.browserActions()
	switch msg.String() {
	case "esc", "ctrl+l", "f2":
		return false, nil
	case "tab", "shift+tab":
		delta := 1
		if msg.String() == "shift+tab" {
			delta = -1
		}
		d.actionFocus = (d.actionFocus + delta + len(actions) + 1) % (len(actions) + 1)
		return true, m.restoreFocus()
	case "enter", "space", " ":
		if d.actionFocus > 0 {
			return true, m.browserAction(actions[d.actionFocus-1])
		}
	case "left", "shift+left", "right", "shift+right":
		if d.actionFocus > 0 {
			delta := 1
			if strings.Contains(msg.String(), "left") {
				delta = -1
			}
			d.actionFocus = 1 + (d.actionFocus-1+delta+len(actions))%len(actions)
			return true, nil
		}
	}
	return d.actionFocus > 0, nil
}

func (m *Model) browserAction(action string) tea.Cmd {
	if !slices.Contains(m.browserActions(), action) {
		return nil
	}
	if action == "settings" {
		m.browserDialog = m.dialog
		d := &dialog{title: "Settings", local: "browser-settings", kind: client.ChooseOne}
		m.dialogSerial++
		d.serial = m.dialogSerial
		d.list = m.newList(d.title, []list.Item{choiceItem{title: "Appearance", command: "theme"}, choiceItem{title: "Conversation model / effort / tier", command: "browser-settings"}})
		m.dialog = d
		m.resize()
		return m.restoreFocus()
	}
	return m.navigationAction(action)
}

type launchForm struct {
	review                           *client.LaunchReview
	focus                            int // message, title, then exact offered controls
	preservedTitle, preservedMessage string
}

// Retain only canonical input and its client-owned source binding. Shutdown
// must not inspect editors which may still be owned by the event loop.
type launchCheckpoint struct {
	input client.LaunchInput
	store func(client.LaunchInput) error
}

func (m *Model) captureLaunchDraft() {
	if m.dialog == nil || m.dialog.launch == nil {
		return
	}
	store := m.dialog.launch.review.StoreDraft
	if store == nil {
		return
	}
	checkpoint := &launchCheckpoint{input: m.launchInput(), store: store}
	m.launchDraftMu.Lock()
	m.launchDraft = checkpoint
	m.launchDraftMu.Unlock()
}

func (m *Model) clearLaunchCheckpoint() {
	m.launchDraftMu.Lock()
	m.launchDraft = nil
	m.launchDraftMu.Unlock()
}

func (m *Model) persistLaunchCheckpoint() error {
	m.launchDraftMu.Lock()
	defer m.launchDraftMu.Unlock()
	if m.launchDraft == nil {
		return nil
	}
	if err := m.launchDraft.store(m.launchDraft.input); err != nil {
		return err
	}
	m.launchDraft = nil
	return nil
}

func (m *Model) storeLaunchDraft() error {
	m.captureLaunchDraft()
	return m.persistLaunchCheckpoint()
}

func newLaunchForm(review *client.LaunchReview) *launchForm { return &launchForm{review: review} }

func (m *Model) initLaunchEditors() {
	f := m.dialog.launch
	m.modalInput.SetValue(f.review.Input.Message)
	if m.modalInput.Value() != f.review.Input.Message {
		f.preservedMessage = f.review.Input.Message
		m.modalInput.SetValue("")
	}
	m.modalInput.CursorEnd()
	m.modalInput.Placeholder = "First message"
	m.launchTitle.SetValue(f.review.Input.Title)
	if m.launchTitle.Value() != f.review.Input.Title || strings.ContainsAny(f.review.Input.Title, "\n\r\t") {
		f.preservedTitle = f.review.Input.Title
		m.launchTitle.SetValue("")
	}
	m.launchTitle.Placeholder = "New conversation"
	m.launchTitle.CursorEnd()
	m.captureLaunchDraft()
}

func (m *Model) launchInput() client.LaunchInput {
	f := m.dialog.launch
	title, message := m.launchTitle.Value(), m.modalInput.Value()
	if f.preservedTitle != "" {
		title = f.preservedTitle
	}
	if f.preservedMessage != "" {
		message = f.preservedMessage
	}
	return client.LaunchInput{Title: title, Message: message}
}

func (m *Model) focusLaunch() tea.Cmd {
	m.launchTitle.Blur()
	m.modalInput.Blur()
	f := m.dialog.launch
	if f.focus == 0 && f.preservedMessage == "" {
		return m.componentCmd("modal", m.dialog.serial, m.modalInput.Focus())
	}
	if f.focus == 1 && f.preservedTitle == "" {
		return m.componentCmd("launch-title", m.dialog.serial, m.launchTitle.Focus())
	}
	return nil
}

func (m *Model) launchAction(action string) tea.Cmd {
	f := m.dialog.launch
	if !slices.Contains(f.review.Actions, action) {
		return nil
	}
	if err := m.storeLaunchDraft(); err != nil {
		m.problem = "Launch draft save: " + safeText(err.Error())
		return nil
	}
	input := m.launchInput()
	if !m.dialog.prompt.Respond(client.Answer{Action: action, Launch: &input, Cancelled: action == "cancel"}) {
		m.notice = "This review was retired"
	}
	m.dialog = nil
	m.resize()
	return m.restoreFocus()
}

func (m *Model) launchKey(msg tea.KeyPressMsg) tea.Cmd {
	f := m.dialog.launch
	actions := m.launchActions()
	switch msg.String() {
	case "ctrl+u":
		if f.focus == 0 && f.preservedMessage != "" {
			f.preservedMessage = ""
			m.modalInput.SetValue("")
			return m.restoreFocus()
		}
		if f.focus == 1 && f.preservedTitle != "" {
			f.preservedTitle = ""
			m.launchTitle.SetValue("")
			return m.restoreFocus()
		}
	case "esc":
		return m.cancelDialog()
	case "tab", "shift+tab":
		delta := 1
		if msg.String() == "shift+tab" {
			delta = -1
		}
		f.focus = (f.focus + delta + len(actions) + 2) % (len(actions) + 2)
		return m.restoreFocus()
	case "f2", "ctrl+l":
		m.refreshLaunchDetails()
		m.dialog.details = true
		m.questionViewport.SetContent(m.dialog.detailText)
		m.resize()
		m.questionViewport.GotoTop()
		return m.restoreFocus()
	case "enter":
		if f.focus >= 2 {
			if actions[f.focus-2] == "editor" {
				return m.editLaunch()
			}
			return m.launchAction(actions[f.focus-2])
		}
		if f.focus == 1 {
			f.focus = 2
			return m.restoreFocus()
		}
		msg = tea.KeyPressMsg(tea.Key{Code: 'j', Mod: tea.ModCtrl})
	}
	if f.focus == 0 {
		return m.updateComponent("modal", m.dialog.serial, msg)
	}
	if f.focus == 1 {
		return m.updateComponent("launch-title", m.dialog.serial, msg)
	}
	return nil
}

func (m *Model) launchActions() []string {
	actions := []string{"editor"}
	for _, action := range []string{"model", "effort", "tier", "create", "cancel"} {
		if slices.Contains(m.dialog.launch.review.Actions, action) {
			actions = append(actions, action)
		}
	}
	return actions
}

func (m *Model) refreshLaunchDetails() {
	f := m.dialog.launch
	input := m.launchInput()
	m.dialog.detailText = fmt.Sprintf("New conversation\n%s\n\nTitle\n%s\n\nFirst message\n%s\n\n%s\n%s", launchLocation(f.review.Location), safeText(input.Title), safeText(input.Message), safeText(f.review.Summary), safeText(f.review.Unavailable))
}

func launchLocation(location client.Location) string {
	var fields []string
	for _, value := range []string{location.Environment, location.ProjectID, location.Workspace, location.Title} {
		if value != "" {
			fields = append(fields, safeText(value))
		}
	}
	return strings.Join(fields, " → ")
}
