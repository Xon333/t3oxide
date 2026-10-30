package ui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/wire"
)

const requestIdleDelay = 1500 * time.Millisecond
const maxRequestSuppressions = 128

type requestIdentity struct{ environment, thread, id, revision string }

// Presentation state is bounded and attachment-local. It contains identities,
// never answers, and never changes the native request or its admission rules.
type requestAttentionState struct {
	now              func() time.Time
	lastInput        time.Time
	timer            bool
	token            uint64
	offers           []requestIdentity
	suppressed       map[requestIdentity]bool
	order            []requestIdentity
	livePage         bool
	historical       bool
	active           requestIdentity
	opening          requestIdentity
	visible          requestIdentity
	promptID         uint64
	automatic, armed bool
	returnFocus      *requestFocusReturn
	pending          *client.Prompt
}

type requestFocusReturn struct {
	workspaceReturn
	searching bool
	inputKind string
}

type requestIdleMsg struct {
	generation, token uint64
	at                time.Time
}

func (m *Model) attentionTime() time.Time {
	if m.attention.now != nil {
		return m.attention.now()
	}
	return time.Now()
}

func (m *Model) retireRequestAttention() {
	if m.attention.pending != nil {
		m.attention.pending.Respond(client.Answer{Cancelled: true})
	}
	clock, token := m.attention.now, m.attention.token+1
	m.attention = requestAttentionState{now: clock, token: token}
}

func (m *Model) noteRequestInput() {
	// All input restarts the idle interval, including editor navigation. A
	// paste arrives as one message and keeps the exact editor's normal owner.
	m.attention.lastInput = m.attentionTime()
}

func (m *Model) observeRequestAttention(snapshot *wire.Snapshot) {
	a := &m.attention
	if !m.status.Connected || !m.status.Writable {
		a.offers = nil
		a.livePage = false
		m.retireVisibleRequest()
		return
	}
	// Status-only events retain the last exact live offer. A closed lane or
	// a historical/replaced page must not leave a deferred form eligible.
	if snapshot == nil {
		if m.status.Historical {
			a.livePage, a.historical = false, true
		}
		if m.status.Requests == 0 {
			a.offers = nil
			m.retireVisibleRequest()
		}
		return
	}
	if snapshot.Metadata.Historical && snapshot.Metadata.ThreadID == m.status.ThreadID {
		// Client keeps verified live request offers independently of history.
		// History disables automatic attention, while explicit Requests still
		// uses that client's exact live offer and pre-dispatch revalidation.
		a.livePage, a.historical = false, true
		m.retireVisibleRequest()
		return
	}
	if !snapshot.Metadata.Live || snapshot.Metadata.ThreadID != m.status.ThreadID {
		a.offers = nil
		a.livePage, a.historical = false, false
		m.retireVisibleRequest()
		return
	}
	a.livePage, a.historical = true, false
	a.offers = nil
	for _, offer := range snapshot.Metadata.Requests {
		id, _ := offer["id"].(string)
		revision, _ := offer["revision"].(string)
		supported, _ := offer["supported"].(bool)
		if id == "" || revision == "" || !supported {
			continue
		}
		a.offers = append(a.offers, requestIdentity{m.status.EnvironmentID, m.status.ThreadID, id, revision})
		if len(a.offers) == 32 {
			break
		}
	}
	if a.active.id != "" && !m.requestAvailable(a.active) {
		m.retireVisibleRequest()
	}
	if len(a.offers) == 0 {
		m.invalidateRequestTimer()
	}
}

func (m *Model) requestStillOffered(key requestIdentity) bool {
	for _, offered := range m.attention.offers {
		if offered == key {
			return true
		}
	}
	return false
}

func requestKey(question client.Question) requestIdentity {
	return requestIdentity{question.EnvironmentID, question.ThreadID, question.RequestID, question.RequestRevision}
}

func (m *Model) requestAvailable(key requestIdentity) bool {
	return m.requestSourceAvailable(key) && m.requestStillOffered(key)
}

func (m *Model) requestSourceAvailable(key requestIdentity) bool {
	return !m.closed && m.session != nil && m.status.Connected && m.status.Writable && m.status.Requests > 0 &&
		key.id != "" && key.revision != "" && key.environment == m.status.EnvironmentID && key.thread == m.status.ThreadID
}

func (m *Model) invalidateRequestTimer() {
	m.attention.timer = false
	m.attention.token++
}

func (m *Model) retireVisibleRequest() {
	a := &m.attention
	active := a.active.id != "" || a.opening.id != "" || a.visible.id != "" || a.pending != nil
	if a.pending != nil {
		a.pending.Respond(client.Answer{Cancelled: true})
		a.pending = nil
	}
	if m.dialog != nil && m.dialog.prompt != nil && a.promptID == m.dialog.prompt.ID && requestKey(m.dialog.prompt.Question) == a.active {
		m.dialog.prompt.Respond(client.Answer{Cancelled: true})
		m.dialog = nil
	}
	if active {
		if m.actionName == "requests" && m.cancelAction != nil {
			m.cancelAction()
		}
		m.notice = "Request changed or disconnected · reopen Requests when available"
		m.restoreRequestReturn()
		m.resize()
		m.restoreFocus()
	}
	a.active, a.visible, a.opening = requestIdentity{}, requestIdentity{}, requestIdentity{}
	a.promptID, a.automatic, a.armed = 0, false, false
	m.invalidateRequestTimer()
}

func (m *Model) requestEditorFocused() bool {
	return m.focused && (m.composer.Focused() || m.modalInput.Focused() || m.launchTitle.Focused() || m.searchInput.Focused() || m.navigator.list.FilterInput.Focused() || m.agentbox.searching)
}

func (m *Model) maintainRequestAttention() tea.Cmd {
	a := &m.attention
	if a.pending != nil {
		if !m.requestAvailable(requestKey(a.pending.Question)) || a.automatic && !a.livePage {
			m.retireVisibleRequest()
			return nil
		}
		select {
		case <-a.pending.Done:
			m.requestPromptClosed(a.pending.ID)
			return nil
		default:
		}
		if m.dialog != nil || m.detail != "" || m.editorBusy || m.browserPage || m.launchPage {
			return nil
		}
		if a.automatic && m.requestEditorFocused() && m.attentionTime().Sub(a.lastInput) < requestIdleDelay {
			return m.scheduleRequestIdle()
		}
		prompt := a.pending
		a.pending = nil
		m.rememberRequestReturn()
		m.openPrompt(prompt)
		m.requestPromptOpened(prompt)
		return m.restoreFocus()
	}
	if m.closed || m.session == nil || !a.livePage || m.status.Historical || m.status.ThreadID == "" || !m.status.Connected || !m.status.Writable || m.status.Requests == 0 || a.opening.id != "" || a.visible.id != "" || m.dialog != nil || m.detail != "" || m.editorBusy || m.actionBusy || m.browserBusy || m.launchPage || m.browserPage {
		return nil
	}
	var next requestIdentity
	for _, offer := range a.offers {
		if !a.suppressed[offer] && m.requestAvailable(offer) {
			next = offer
			break
		}
	}
	if next.id == "" {
		return nil
	}
	remaining := requestIdleDelay - m.attentionTime().Sub(a.lastInput)
	if m.requestEditorFocused() && remaining > 0 {
		return m.scheduleRequestIdle()
	}
	a.active, a.opening, a.automatic, a.armed = next, next, true, false
	session := m.session
	return m.navigatorRun("requests", func(ctx context.Context) (client.Result, error) {
		return client.Result{}, session.ReviewRequest(ctx, next.id, next.revision)
	})
}

func (m *Model) scheduleRequestIdle() tea.Cmd {
	a := &m.attention
	if a.timer {
		return nil
	}
	a.timer = true
	a.token++
	token, generation := a.token, m.generation
	remaining := max(time.Millisecond, requestIdleDelay-m.attentionTime().Sub(a.lastInput))
	return tea.Tick(remaining, func(now time.Time) tea.Msg { return requestIdleMsg{generation, token, now} })
}

func (m *Model) deferAutomaticRequestPrompt(prompt *client.Prompt) bool {
	if prompt.Question.RequestID == "" {
		return false
	}
	a, key := &m.attention, requestKey(prompt.Question)
	available := m.requestAvailable(key)
	if !a.automatic && a.historical {
		available = m.requestSourceAvailable(key)
	}
	if !available || a.automatic && !a.livePage || a.active.id != "" && a.active != key {
		prompt.Respond(client.Answer{Cancelled: true})
		if key == a.active {
			m.retireVisibleRequest()
		}
		m.notice = "Request changed or disconnected · reopen Requests when available"
		return true
	}
	a.active, a.promptID = key, prompt.ID
	if a.automatic && (m.dialog != nil || m.detail != "" || m.editorBusy || m.browserPage || m.launchPage || m.requestEditorFocused() && m.attentionTime().Sub(a.lastInput) < requestIdleDelay) {
		a.pending = prompt
		return true
	}
	m.rememberRequestReturn()
	return false
}

func (m *Model) requestIdle(msg requestIdleMsg) {
	if msg.generation != m.generation || msg.token != m.attention.token {
		return
	}
	m.attention.timer = false
}

func (m *Model) requestPromptOpened(prompt *client.Prompt) {
	q := prompt.Question
	if q.RequestID == "" {
		return
	}
	a := &m.attention
	a.visible = requestKey(q)
	a.active, a.promptID = a.visible, prompt.ID
	if a.opening != a.visible {
		a.automatic = false
		a.armed = true
	}
	a.opening = requestIdentity{}
	m.invalidateRequestTimer()
	if a.automatic && m.dialog != nil && m.dialog.requestForm != nil {
		// Initial focus is the heading. Queued Enter/Send from the composer
		// cannot select or submit a native permission. Tab/arrows/click enter
		// the ordinary form controls; no extra confirmation is introduced.
		m.dialog.requestForm.focus = -1
		m.modalInput.Blur()
	}
}

func (m *Model) suppressVisibleRequest() {
	a := &m.attention
	m.suppressRequest(a.visible)
	a.visible, a.opening = requestIdentity{}, requestIdentity{}
	a.automatic = false
	m.invalidateRequestTimer()
}

func (m *Model) suppressRequest(identity requestIdentity) {
	if identity.id == "" {
		return
	}
	a := &m.attention
	if a.suppressed == nil {
		a.suppressed = map[requestIdentity]bool{}
	}
	if !a.suppressed[identity] {
		if len(a.order) >= maxRequestSuppressions {
			delete(a.suppressed, a.order[0])
			a.order = a.order[1:]
		}
		a.order = append(a.order, identity)
		a.suppressed[identity] = true
	}
}

func (m *Model) rememberRequestReturn() {
	if m.attention.returnFocus != nil {
		return
	}
	inputKind := ""
	switch {
	case m.searching && m.searchInput.Focused():
		inputKind = "search"
	case m.mode == "agentbox" && m.agentbox.searching:
		inputKind = "agentbox-search"
	case m.navigator.focused && m.navigator.list.FilterInput.Focused():
		inputKind = "navigator"
	case m.composer.Focused():
		inputKind = "composer"
	}
	m.attention.returnFocus = &requestFocusReturn{workspaceReturn: workspaceReturn{navigation: m.navigator.focused, reading: m.transcriptFocus, context: m.contextFocused, activity: m.activityFocused, anchor: m.readingAnchor, offset: m.viewport.YOffset(), follow: m.follow, contextSelection: m.contextSelection, contextKey: m.contextFocusKey, activityKey: m.activityFocusKey}, searching: m.searching, inputKind: inputKind}
}

func (m *Model) restoreRequestReturn() {
	if r := m.attention.returnFocus; r != nil {
		m.navigator.focused, m.transcriptFocus, m.contextFocused, m.activityFocused = r.navigation, r.reading, r.context, r.activity
		m.contextSelection, m.contextFocusKey, m.activityFocusKey = r.contextSelection, r.contextKey, r.activityKey
		m.searching = r.searching
		m.follow, m.readingAnchor = r.follow, r.anchor
		m.resize()
		m.viewport.SetYOffset(r.offset)
		m.anchorOffset = m.viewport.YOffset()
		m.attention.returnFocus = nil
	}
}

// A client reader may fail or be retired before producing its prompt. Finish
// the presentation lane on the matching action result so that another offered
// request remains reachable. The same revision is never retried automatically.
func (m *Model) requestActionFinished() {
	a := &m.attention
	m.suppressRequest(a.active)
	if a.pending != nil {
		a.pending.Respond(client.Answer{Cancelled: true})
		a.pending = nil
	}
	if m.dialog != nil && m.dialog.prompt != nil && m.dialog.prompt.ID == a.promptID && requestKey(m.dialog.prompt.Question) == a.active {
		m.dialog.prompt.Respond(client.Answer{Cancelled: true})
		m.dialog = nil
	}
	a.active, a.visible, a.opening = requestIdentity{}, requestIdentity{}, requestIdentity{}
	a.promptID, a.automatic, a.armed = 0, false, false
	m.invalidateRequestTimer()
	m.restoreRequestReturn()
}

func (m *Model) requestPromptClosed(id uint64) bool {
	a := &m.attention
	if a.promptID != id || a.active.id == "" {
		return false
	}
	m.suppressRequest(a.active)
	a.pending = nil
	if m.dialog != nil && m.dialog.prompt != nil && m.dialog.prompt.ID == id && requestKey(m.dialog.prompt.Question) == a.active {
		m.dialog = nil
	}
	a.active, a.visible, a.opening = requestIdentity{}, requestIdentity{}, requestIdentity{}
	a.promptID, a.automatic, a.armed = 0, false, false
	m.invalidateRequestTimer()
	m.restoreRequestReturn()
	return true
}

// Explicit Requests promotes an already admitted read instead of dispatching
// another one or waiting for typing idle. It does not choose or submit an answer.
func (m *Model) explicitRequestPresentation() (bool, tea.Cmd) {
	a := &m.attention
	if !a.automatic || a.active.id == "" {
		return false, nil
	}
	if !m.requestAvailable(a.active) {
		m.retireVisibleRequest()
		return true, m.restoreFocus()
	}
	a.automatic, a.armed = false, true
	m.invalidateRequestTimer()
	if a.pending != nil && m.dialog == nil && m.detail == "" && !m.editorBusy && !m.browserPage && !m.launchPage {
		prompt := a.pending
		a.pending = nil
		m.rememberRequestReturn()
		m.openPrompt(prompt)
		m.requestPromptOpened(prompt)
	}
	return true, m.restoreFocus()
}

// Text that was already on its way to an editor when an automatic form arrived
// keeps that editor as owner. Defer the unchanged prompt again and route only
// editor input, never Send or a request action, through the normal component.
func (m *Model) resumeUnarmedRequestInput(message tea.Msg) (bool, tea.Cmd) {
	a := &m.attention
	if !a.automatic || a.armed || a.returnFocus == nil || a.returnFocus.inputKind == "" || m.dialog == nil || m.dialog.prompt == nil || m.dialog.requestForm == nil || m.dialog.prompt.ID != a.promptID || m.detail != "" {
		return false, nil
	}
	kind := a.returnFocus.inputKind
	switch msg := message.(type) {
	case tea.PasteMsg, clipboardPasteMsg:
	case tea.KeyPressMsg:
		printable := msg.Text != "" && msg.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModSuper) == 0
		editing := msg.Mod == 0 && (msg.Code == tea.KeyBackspace || msg.Code == tea.KeyDelete || msg.Code == tea.KeyEnter && kind == "composer")
		if !printable && !editing {
			return false, nil
		}
	default:
		return false, nil
	}
	a.pending, a.opening, a.visible = m.dialog.prompt, a.active, requestIdentity{}
	m.dialog = nil
	m.restoreRequestReturn()
	m.restoreFocus()
	m.noteRequestInput()
	m.notice = "Request pending · finish typing or open Requests"
	if kind == "agentbox-search" {
		return true, m.updateComponent(kind, m.agentbox.serial, message)
	}
	id := uint64(0)
	if kind == "navigator" {
		id = m.navigator.serial
	}
	return true, m.updateComponent(kind, id, message)
}

func (m *Model) guardAutomaticRequestKey(msg tea.KeyPressMsg) bool {
	a := &m.attention
	if !a.automatic || a.armed {
		return false
	}
	switch msg.String() {
	case "esc", "ctrl+c":
		return false
	case "tab", "shift+tab", "up", "down", "left", "right":
		a.armed = true
		m.dialog.requestForm.focus = 0
		if len(m.dialog.requestForm.questions[m.dialog.requestForm.index].Options) == 0 {
			m.dialog.requestForm.focus = 1
		}
		m.restoreFocus()
		return true
	default:
		m.notice = "Request pending · Tab or click to choose · Esc closes"
		return true
	}
}

func (m *Model) requestCue() string {
	if m.status.Requests == 0 {
		return ""
	}
	return fmt.Sprintf("%d request(s) · Ctrl+R Requests", m.status.Requests)
}
