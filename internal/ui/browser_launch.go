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
	if m.browserDialog != nil {
		m.dialog, m.browserDialog = m.browserDialog, nil
		m.detail, m.detailTitle, m.detailCopy = "", "", ""
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
	identity  string
	results   bool
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
	identity := ""
	if selected, ok := d.list.SelectedItem().(choiceItem); ok {
		identity = selected.id
	}
	m.browserStates[d.prompt.Question.BrowserPage.ProfileID] = browserState{query: d.list.FilterInput, index: d.list.Index(), hostQuery: d.prompt.Question.BrowserPage.Query, identity: identity, results: d.resultsFocused}
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
		d.resultsFocused = state.results
		// Reordering must never turn a remembered row number into another target.
		for i, item := range d.list.VisibleItems() {
			if item.(choiceItem).id == state.identity {
				d.list.Select(i)
				break
			}
		}
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

type browserControl struct {
	action, label string
	local         bool
}

const browserMore = "browser-more"

// Rendering and traversal share this order. Unrecognized offered IDs remain
// available in More; only this local control is synthesized.
func (m *Model) visibleBrowserControls() []browserControl {
	if m.dialog == nil || m.dialog.prompt == nil || m.dialog.prompt.Question.BrowserPage == nil {
		return nil
	}
	page := m.dialog.prompt.Question.BrowserPage
	var controls []browserControl
	for _, action := range page.Actions {
		if id, ok := strings.CutPrefix(action, "profile:"); ok {
			label := id
			for _, profile := range page.Profiles {
				if profile.ID == id {
					label = profile.Label
					break
				}
			}
			if page.ProfileID == id {
				label = "● " + label
			}
			controls = append(controls, browserControl{action: action, label: label})
		}
	}
	for _, action := range []string{"environments", "new", "settings", "resume"} {
		if slices.Contains(page.Actions, action) {
			controls = append(controls, browserControl{action: action, label: actionLabel(action)})
		}
	}
	return append(controls, browserControl{action: browserMore, label: "More", local: true})
}

func primaryBrowserAction(action string) bool {
	return strings.HasPrefix(action, "profile:") || slices.Contains([]string{"environments", "new", "settings", "resume"}, action)
}

func (m *Model) openBrowserMore() tea.Cmd {
	source := m.dialog
	if source == nil || source.prompt == nil || source.prompt.Question.BrowserPage == nil {
		return nil
	}
	var items []list.Item
	seen := map[string]bool{}
	selected, _ := source.list.SelectedItem().(choiceItem)
	for _, action := range m.browserActions() {
		canonical := action
		if action == "types" {
			canonical = "filter"
		}
		if seen[canonical] {
			continue
		}
		seen[canonical] = true
		label := strings.TrimSpace(selected.title)
		if action == "toggle" && !strings.HasPrefix(label, "▸ ") && !strings.HasPrefix(label, "▾ ") {
			continue
		}
		if action == "expand" && !strings.HasPrefix(label, "▸ ") {
			continue
		}
		if action == "collapse" && !strings.HasPrefix(label, "▾ ") {
			continue
		}
		if action == "parent" && !strings.Contains(selected.description, " · parent: ") {
			continue
		}
		if !primaryBrowserAction(action) {
			items = append(items, choiceItem{id: action, title: actionLabel(action), command: "browser-more-action"})
		}
	}
	items = append(items, choiceItem{title: "Help", command: "browser-more-help"})
	m.dialogSerial++
	d := &dialog{serial: m.dialogSerial, title: "More", local: "browser-more", kind: client.ChooseOne, browserSource: source}
	d.list = m.newList(d.title, items)
	m.browserDialog, m.dialog = source, d
	m.resize()
	return m.restoreFocus()
}

func (m *Model) browserMoreSourceLive() bool {
	d := m.dialog
	if d == nil || d.browserSource == nil || m.browserDialog != d.browserSource || d.browserSource.prompt == nil || d.browserSource.prompt.Question.BrowserPage == nil {
		return false
	}
	select {
	case <-d.browserSource.prompt.Done:
		return false
	default:
		return true
	}
}

func (m *Model) retireBrowserMore() tea.Cmd {
	source, suspended := m.dialog.browserSource, m.browserDialog
	m.dialog, m.browserDialog = nil, nil
	m.notice = "This browser was retired; no action sent"
	if suspended != nil && suspended != source {
		// A replaced source is authoritative; never answer or discard it on
		// behalf of the old menu, even if a stale activation arrives.
		m.dialog = suspended
		m.resize()
		return m.restoreFocus()
	}
	return m.cancelDialog()
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
	case "types", "filter":
		return "Thread type"
	case "search":
		return "Search conversations"
	case "lifecycle":
		return "Lifecycle"
	case "next-page":
		return "Next page"
	case "previous-page":
		return "Previous page"
	}
	return strings.ToUpper(action[:1]) + action[1:]
}

func (m *Model) browserKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	d, controls := m.dialog, m.visibleBrowserControls()
	switch msg.String() {
	case "esc", "ctrl+l", "f2":
		return false, nil
	case "tab", "shift+tab":
		if msg.String() == "tab" && d.actionFocus == 0 && d.list.FilterState() == list.Filtering && !d.resultsFocused {
			d.resultsFocused = true
			d.list.FilterInput.Blur()
			m.resize()
			return true, nil
		}
		if msg.String() == "shift+tab" && d.actionFocus == 0 && d.resultsFocused {
			d.resultsFocused = false
			return true, m.restoreFocus()
		}
		delta := 1
		if msg.String() == "shift+tab" {
			delta = -1
		}
		d.actionFocus = (d.actionFocus + delta + len(controls) + 1) % (len(controls) + 1)
		m.resize()
		return true, m.restoreFocus()
	case "enter", "space", " ":
		if d.actionFocus > 0 {
			if d.actionFocus > len(controls) {
				return true, nil
			}
			control := controls[d.actionFocus-1]
			if control.local {
				return true, m.openBrowserMore()
			}
			return true, m.browserAction(control.action)
		}
	case "left", "shift+left", "right", "shift+right":
		if d.actionFocus > 0 && len(controls) > 0 {
			delta := 1
			if strings.Contains(msg.String(), "left") {
				delta = -1
			}
			d.actionFocus = 1 + (d.actionFocus-1+delta+len(controls))%len(controls)
			m.resize()
			return true, nil
		}
	}
	if d.actionFocus == 0 && d.resultsFocused && msg.Text != "" && msg.Text != " " {
		d.resultsFocused = false
		m.restoreFocus()
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
	m.modalInput.Placeholder = ""
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
			if i := slices.Index(actions, "create"); i >= 0 {
				f.focus = i + 2
			}
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
	var actions []string
	for _, action := range []string{"create", "cancel", "model", "effort", "tier"} {
		if slices.Contains(m.dialog.launch.review.Actions, action) {
			actions = append(actions, action)
		}
	}
	return append(actions, "editor")
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
