// Package ui is T3oxide's direct Bubble Tea interface. The existing adapter is
// the protocol boundary; no local agent, orchestration or transcript database
// lives in this package.
package ui

import (
	"context"
	"errors"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/wire"
)

type sessionMsg struct {
	generation uint64
	event      client.Event
	err        error
}
type sessionEndedMsg struct{ generation uint64 }
type promptMsg struct {
	generation uint64
	prompt     *client.Prompt
}
type promptClosedMsg struct{ generation, id uint64 }
type noticeMsg struct {
	generation uint64
	text       string
}
type actionMsg struct {
	generation, id uint64
	name           string
	result         client.Result
	err            error
	stop           bool
}
type settingMsg struct {
	kind, value string
	err         error
}
type copyResultMsg struct{ err error }

// Model keeps component state on Bubble Tea's event loop. Network operations,
// settings persistence, clipboard helpers and Markdown rendering run in commands.
type Model struct {
	ctx                                        context.Context
	cancel                                     context.CancelFunc
	sessionCtx                                 context.Context
	detach                                     context.CancelFunc
	session                                    *client.Session
	events                                     chan sessionMsg
	generation, revision                       uint64
	settings                                   *Settings
	theme                                      string
	mouse, sidebar                             bool
	width, height, contentWidth, bodyHeight    int
	modalWidth, modalHeight                    int
	composer, modalInput                       textarea.Model
	searchInput                                textinput.Model
	viewport, detailViewport, questionViewport viewport.Model
	status                                     client.Status
	snapshot                                   *wire.Snapshot
	dialog                                     *dialog
	dialogSerial                               uint64
	detailTitle, detail                        string
	searching                                  bool
	searchTerm                                 string
	searchLines                                []int
	searchIndex                                int
	transcript                                 string
	follow, transcriptFocus, focused           bool
	draftEdits                                 uint64
	preservedDraft                             string
	notice, problem                            string
	streamProblem                              string
	actionBusy, stopBusy                       bool
	actionName                                 string
	settingBusy                                bool
	actionID                                   uint64
	cancelAction                               context.CancelFunc
	actionDone                                 <-chan struct{}
	renderer                                   transcriptRenderer
	renderSerial                               uint64
	rendering                                  bool
	pendingRender                              *renderRequest
	closed                                     bool
}

func NewModel(ctx context.Context, session *client.Session, settings *Settings) *Model {
	if ctx == nil {
		ctx = context.Background()
	}
	if settings == nil {
		settings = &Settings{Theme: "violet", Mouse: true}
	}
	ctx, cancel := context.WithCancel(ctx)
	theme, mouse, sidebar := settings.appearance()
	if !ValidTheme(theme) {
		theme = "violet"
	}
	m := &Model{ctx: ctx, cancel: cancel, settings: settings, theme: theme, mouse: mouse, sidebar: sidebar, width: 80, height: 24, follow: true, focused: true, searchIndex: -1}
	m.composer = newTextarea("Message T3 · Ctrl+P commands")
	m.composer.DynamicHeight = true
	m.composer.MinHeight = 1
	m.modalInput = newTextarea("Enter your answer")
	m.searchInput = textinput.New()
	m.searchInput.Prompt = "Find in this page: "
	m.searchInput.CharLimit = 256
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(15))
	m.detailViewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(15))
	m.questionViewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(3))
	m.viewport.SoftWrap = false
	m.viewport.FillHeight = true
	m.detailViewport.SoftWrap = true
	m.questionViewport.SoftWrap = true
	m.setSession(session)
	m.applyTheme()
	m.resize()
	m.restoreFocus()
	return m
}

func newTextarea(placeholder string) textarea.Model {
	t := textarea.New()
	t.Placeholder = placeholder
	t.Prompt = "› "
	t.ShowLineNumbers = false
	t.EndOfBufferCharacter = ' '
	// Enforce the byte/line budget ourselves, rejecting the entire edit. The
	// component's rune/visual-row limits silently truncate pasted human text.
	t.CharLimit = 0
	t.MaxHeight = 10000
	t.MaxContentHeight = 0
	t.MaxWidth = 10000
	t.SetHeight(3)
	t.SetVirtualCursor(true)
	t.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "shift+enter", "ctrl+j"), key.WithHelp("alt+enter", "newline"))
	return t
}

// Run restores terminal modes through Bubble Tea on normal exit, cancellation,
// or failure. Manager.Close remains the CLI's owner of durable final cleanup.
func Run(ctx context.Context, session *client.Session, settings *Settings) error {
	m := NewModel(ctx, session, settings)
	defer m.Close()
	_, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
		return nil
	}
	return err
}

// Close is safe after Program.Run, and cancellation is safe from another
// goroutine. It never interprets local context cancellation as remote Stop.
func (m *Model) Close() { m.cancel() }

func (m *Model) setSession(s *client.Session) {
	if m.detach != nil {
		m.detach()
	}
	if m.cancelAction != nil {
		m.cancelAction()
		m.cancelAction = nil
	}
	m.actionDone = nil
	if m.dialog != nil && m.dialog.prompt != nil {
		m.dialog.prompt.Respond(client.Answer{Cancelled: true})
	}
	m.generation++
	m.revision = 0
	m.session = s
	m.sessionCtx, m.detach = context.WithCancel(m.ctx)
	m.events = make(chan sessionMsg, 1)
	m.snapshot = nil
	m.status = client.Status{State: "connecting", Delivery: "auto"}
	m.dialog = nil
	m.detail, m.detailTitle = "", ""
	m.problem, m.notice, m.streamProblem = "", "", ""
	m.actionBusy, m.stopBusy = false, false
	m.actionName = ""
	m.draftEdits = 0
	m.preservedDraft = ""
	m.transcript = ""
	m.viewport.SetContent("")
	m.viewport.GotoTop()
	m.follow = true
	m.transcriptFocus = false
	m.searchTerm = ""
	m.searching = false
	m.searchInput.SetValue("")
	m.searchLines = nil
	m.searchIndex = -1
	m.viewport.StyleLineFunc = nil
	m.pendingRender = nil
	if s != nil {
		m.restoreDraft(s.Draft())
	} else {
		m.composer.SetValue("")
	}
	m.composer.CursorEnd()
	m.resize()
	m.restoreFocus()
}

func (m *Model) Init() tea.Cmd {
	if m.session == nil {
		return nil
	}
	return m.listen()
}

func (m *Model) listen() tea.Cmd {
	ctx, session, events, generation := m.sessionCtx, m.session, m.events, m.generation
	return tea.Batch(
		func() tea.Msg {
			defer close(events)
			for event, err := range session.Events(ctx) {
				select {
				case events <- sessionMsg{generation, event, err}:
				case <-ctx.Done():
					return nil
				}
			}
			return nil
		},
		waitEvent(ctx, events, generation),
		waitPrompt(ctx, session, generation),
		waitNotice(ctx, session, generation),
	)
}
func waitEvent(ctx context.Context, events <-chan sessionMsg, generation uint64) tea.Cmd {
	return func() tea.Msg {
		select {
		case event, ok := <-events:
			if !ok {
				return sessionEndedMsg{generation}
			}
			return event
		case <-ctx.Done():
			return nil
		}
	}
}
func waitPrompt(ctx context.Context, s *client.Session, generation uint64) tea.Cmd {
	return func() tea.Msg {
		select {
		case p, ok := <-s.Prompts():
			if !ok || p == nil {
				return nil
			}
			return promptMsg{generation, p}
		case <-ctx.Done():
			return nil
		}
	}
}
func waitNotice(ctx context.Context, s *client.Session, generation uint64) tea.Cmd {
	return func() tea.Msg {
		select {
		case n, ok := <-s.Notices():
			if !ok {
				return nil
			}
			return noticeMsg{generation, n}
		case <-ctx.Done():
			return nil
		}
	}
}

func (m *Model) requestRender(reset, follow bool) tea.Cmd {
	if m.snapshot == nil {
		return nil
	}
	m.renderSerial++
	req := &renderRequest{generation: m.generation, revision: m.revision, serial: m.renderSerial, snapshot: m.snapshot, width: m.contentWidth, theme: m.theme, reset: reset, follow: follow}
	if m.pendingRender != nil {
		req.reset = req.reset || m.pendingRender.reset
	}
	m.pendingRender = req
	return m.startRender()
}
func (m *Model) startRender() tea.Cmd {
	if m.rendering || m.pendingRender == nil {
		return nil
	}
	req := *m.pendingRender
	m.pendingRender = nil
	m.rendering = true
	ctx := m.ctx
	return func() tea.Msg {
		content, err := m.renderer.render(ctx, req)
		return renderedMsg{req, content, err}
	}
}

func (m *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	var commands []tea.Cmd
	switch msg := message.(type) {
	case componentMsg:
		if msg.generation != m.generation {
			return m, nil
		}
		return m, m.updateComponent(msg.kind, msg.id, msg.message)
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, msg.Width), max(1, msg.Height)
		oldWidth := m.contentWidth
		m.resize()
		if oldWidth != m.contentWidth {
			return m, m.requestRender(false, false)
		}
		return m, nil
	case tea.FocusMsg:
		m.focused = true
		return m, m.restoreFocus()
	case tea.BlurMsg:
		m.focused = false
		m.composer.Blur()
		m.modalInput.Blur()
		m.searchInput.Blur()
		if m.dialog != nil {
			m.dialog.list.FilterInput.Blur()
		}
		return m, nil
	case sessionMsg:
		if msg.generation != m.generation {
			return m, nil
		}
		commands = append(commands, waitEvent(m.sessionCtx, m.events, m.generation))
		if msg.err != nil {
			m.streamProblem = safeText(msg.err.Error())
		}
		event := msg.event
		if event.Revision <= m.revision {
			return m, tea.Batch(commands...)
		}
		m.revision = event.Revision
		m.status = event.Status
		m.streamProblem = safeText(event.Error)
		if event.Draft != nil && m.draftEdits == 0 {
			m.restoreDraft(*event.Draft)
		}
		if event.DraftAccepted != nil && m.currentDraft() == *event.DraftAccepted {
			m.preservedDraft = ""
			m.composer.SetValue("")
			m.draftEdits++
		}
		if event.Snapshot != nil {
			m.snapshot = event.Snapshot
			if event.ResetScroll {
				m.follow = event.Follow
			}
			// Adapter revisions restart with each on-demand helper. Equal numeric
			// revisions cannot establish snapshot identity after reconnect. The
			// asynchronous keyed cache reuses unchanged rows and coalesces bursts.
			commands = append(commands, m.requestRender(event.ResetScroll, event.Follow))
		}
		m.resize()
		return m, tea.Batch(commands...)
	case sessionEndedMsg:
		if msg.generation == m.generation && m.ctx.Err() == nil {
			m.status.Connected = false
			m.status.State = "detached · /reconnect"
		}
		return m, nil
	case renderedMsg:
		m.rendering = false
		if msg.request.generation == m.generation && msg.request.theme == m.theme && msg.request.width == m.contentWidth && (m.pendingRender == nil || m.pendingRender.serial <= msg.request.serial) {
			if msg.err != nil && !errors.Is(msg.err, context.Canceled) {
				m.problem = "Render: " + msg.err.Error()
			}
			if msg.err == nil {
				wasBottom := m.viewport.AtBottom()
				m.transcript = msg.content
				m.viewport.SetContent(msg.content)
				if msg.request.reset {
					if msg.request.follow {
						m.viewport.GotoBottom()
					} else {
						m.viewport.GotoTop()
					}
				} else if m.follow && wasBottom {
					m.viewport.GotoBottom()
				}
				m.refreshSearch(false)
			}
		} else if m.pendingRender != nil && msg.request.reset && msg.request.generation == m.generation {
			m.pendingRender.reset = true
		}
		return m, m.startRender()
	case promptMsg:
		if msg.generation != m.generation {
			msg.prompt.Respond(client.Answer{Cancelled: true})
			return m, nil
		}
		select {
		case <-msg.prompt.Done:
			return m, waitPrompt(m.sessionCtx, m.session, m.generation)
		default:
		}
		m.openPrompt(msg.prompt)
		generation, id, done := m.generation, msg.prompt.ID, msg.prompt.Done
		commands = append(commands, waitPrompt(m.sessionCtx, m.session, m.generation), func() tea.Msg {
			select {
			case <-done:
				return promptClosedMsg{generation, id}
			case <-m.ctx.Done():
				return nil
			}
		})
		commands = append(commands, m.restoreFocus())
		return m, tea.Batch(commands...)
	case promptClosedMsg:
		if msg.generation == m.generation && m.dialog != nil && m.dialog.prompt != nil && m.dialog.prompt.ID == msg.id {
			m.dialog = nil
			m.resize()
			return m, m.restoreFocus()
		}
		return m, nil
	case noticeMsg:
		if msg.generation != m.generation {
			return m, nil
		}
		m.notice = safeText(msg.text)
		return m, waitNotice(m.sessionCtx, m.session, m.generation)
	case actionMsg:
		if msg.generation != m.generation {
			return m, nil
		}
		if msg.stop {
			m.stopBusy = false
		} else {
			if msg.id != m.actionID {
				return m, nil
			}
			m.actionBusy = false
			m.actionName = ""
			m.cancelAction = nil
			m.actionDone = nil
		}
		m.finishCommandNotice(msg.name, msg.result, msg.err)
		if msg.err != nil && !errors.Is(msg.err, context.Canceled) {
			m.problem = safeText(msg.err.Error())
		}
		if msg.err == nil {
			m.problem = ""
			if msg.result.Detail != "" {
				m.openDetail(msg.result.DetailTitle, msg.result.Detail)
			}
			if msg.result.Next != nil {
				m.setSession(msg.result.Next)
				return m, m.listen()
			}
		}
		return m, nil
	case settingMsg:
		m.settingBusy = false
		if msg.err != nil {
			m.problem = "Settings were not saved: " + safeText(msg.err.Error())
			return m, nil
		}
		m.problem = ""
		switch msg.kind {
		case "theme":
			m.theme = msg.value
			m.applyTheme()
			m.notice = "Theme · " + msg.value
			return m, m.requestRender(false, false)
		case "mouse":
			m.mouse = msg.value == "on"
			m.notice = "Mouse capture " + msg.value
			m.resize()
		case "sidebar":
			m.sidebar = msg.value == "on"
			m.notice = "Sidebar " + msg.value
			m.resize()
			return m, m.requestRender(false, false)
		}
		return m, nil
	case copyResultMsg:
		if msg.err != nil {
			m.notice = "OSC 52 copy sent; desktop clipboard: " + safeText(msg.err.Error())
		} else {
			m.notice = "Copied to clipboard"
		}
		return m, nil
	case tea.KeyPressMsg:
		return m, m.handleKey(msg)
	case tea.MouseWheelMsg:
		if m.detail != "" {
			var cmd tea.Cmd
			m.detailViewport, cmd = m.detailViewport.Update(msg)
			return m, cmd
		}
		if m.dialog != nil {
			if m.dialog.details {
				var cmd tea.Cmd
				m.questionViewport, cmd = m.questionViewport.Update(msg)
				return m, cmd
			}
			kind := "list"
			if m.dialog.kind == client.EnterText {
				kind = "modal"
			}
			return m, m.updateComponent(kind, m.dialog.serial, msg)
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		m.follow = m.viewport.AtBottom()
		return m, cmd
	}
	if m.detail != "" {
		return m, nil
	}
	if m.dialog != nil {
		kind := "list"
		if m.dialog.kind == client.EnterText {
			kind = "modal"
		}
		return m, m.updateComponent(kind, m.dialog.serial, message)
	}
	if m.searching {
		return m, m.updateComponent("search", 0, message)
	}
	if m.detail != "" || m.transcriptFocus {
		return m, nil
	}
	return m, m.updateComponent("composer", 0, message)
}

func (m *Model) composerChanged(old string) {
	value := m.composer.Value()
	if len(value) > maxInputBytes || strings.Count(value, "\n") >= maxInputLines {
		m.composer.SetValue(old)
		m.problem = "Draft limit is 64 KiB / 9,999 lines; this input was not added"
		value = m.composer.Value()
	}
	if value != old {
		m.draftEdits++
		if m.session != nil {
			if err := m.session.SetDraft(value); err != nil {
				m.problem = "Draft save: " + safeText(err.Error())
			}
		}
		m.resize()
	}
}

func (m *Model) currentDraft() string {
	if m.preservedDraft != "" {
		return m.preservedDraft
	}
	return m.composer.Value()
}

// Legacy drafts can contain more lines than Bubbles supports, or characters its
// textarea normalizes (for example literal tabs). Never silently replace that
// canonical human text with a truncated/normalized editor value. Keep it intact
// until the user copies/reviews it or explicitly clears it.
func (m *Model) restoreDraft(text string) {
	m.preservedDraft = ""
	m.composer.Placeholder = "Message T3 · Ctrl+P commands"
	if len(text) <= maxInputBytes && strings.Count(text, "\n") < maxInputLines {
		m.composer.SetValue(text)
		if m.composer.Value() == text {
			m.composer.CursorEnd()
			return
		}
	}
	m.preservedDraft = text
	m.composer.SetValue("")
	m.composer.Placeholder = "Saved draft preserved · F2 reviews it"
	m.composer.Blur()
	m.problem = "Saved draft preserved unchanged · F2 review · Ctrl+Y copy · Ctrl+C clear"
}

func (m *Model) restoreFocus() tea.Cmd {
	m.composer.Blur()
	m.modalInput.Blur()
	m.searchInput.Blur()
	if m.dialog != nil {
		m.dialog.list.FilterInput.Blur()
	}
	if !m.focused {
		return nil
	}
	if m.detail != "" {
		return nil
	}
	if m.dialog != nil {
		if m.dialog.details {
			return nil
		}
		if m.dialog.kind == client.EnterText && m.dialog.preservedInitial == "" {
			return m.componentCmd("modal", m.dialog.serial, m.modalInput.Focus())
		}
		if m.dialog.kind != client.EnterText && m.dialog.list.FilterState() == list.Filtering {
			return m.componentCmd("list", m.dialog.serial, m.dialog.list.FilterInput.Focus())
		}
		return nil
	}
	if m.searching {
		return m.componentCmd("search", 0, m.searchInput.Focus())
	}
	if m.transcriptFocus || m.detail != "" || m.preservedDraft != "" {
		return nil
	}
	return m.componentCmd("composer", 0, m.composer.Focus())
}

func (m *Model) quit() tea.Cmd {
	m.closed = true
	m.cancel()
	return tea.Quit
}

func (m *Model) execute(name, args string) tea.Cmd {
	if m.session == nil {
		m.problem = "No session selected"
		return nil
	}
	navigating := name == "switch" || name == "env" || name == "threads" || name == "filter" || name == "parent" || name == "reconnect"
	var previousAction, previousPrompt <-chan struct{}
	if m.actionBusy {
		if !navigating {
			m.notice = "An action is pending; Stop remains available"
			return nil
		}
		if m.cancelAction != nil {
			m.cancelAction()
		}
		previousAction = m.actionDone
	}
	if navigating && m.dialog != nil && m.dialog.prompt != nil {
		previousPrompt = m.dialog.prompt.Done
		m.dialog.prompt.Respond(client.Answer{Cancelled: true})
		m.dialog = nil
		m.resize()
		m.restoreFocus()
	}
	m.actionID++
	id, generation, session := m.actionID, m.generation, m.session
	ctx, cancel := context.WithCancel(m.sessionCtx)
	m.cancelAction = cancel
	done := make(chan struct{})
	m.actionDone = done
	m.actionBusy = true
	m.actionName = name
	m.beginCommandNotice(name)
	return func() tea.Msg {
		defer close(done)
		defer cancel()
		// A replacement picker waits off the UI loop for the canceled caller
		// to retire its exact request. Stop never waits on this ordinary lane.
		for _, retired := range []<-chan struct{}{previousAction, previousPrompt} {
			if retired != nil {
				select {
				case <-retired:
				case <-ctx.Done():
					return actionMsg{generation: generation, id: id, name: name, err: ctx.Err()}
				}
			}
		}
		result, err := session.Command(ctx, name, args)
		return actionMsg{generation: generation, id: id, name: name, result: result, err: err}
	}
}

func (m *Model) send() tea.Cmd {
	if m.preservedDraft != "" {
		m.problem = "Saved draft needs review · F2 review · Ctrl+Y copy · Ctrl+C clear"
		return nil
	}
	text := m.composer.Value()
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if strings.HasPrefix(text, "/") && !strings.Contains(text, "\n") {
		return m.slash(text)
	}
	if m.session == nil {
		m.problem = "Choose a conversation first"
		return nil
	}
	if m.actionBusy {
		m.notice = "Awaiting the current action; your draft is preserved"
		return nil
	}
	m.actionID++
	id, generation, session := m.actionID, m.generation, m.session
	ctx, cancel := context.WithCancel(m.sessionCtx)
	m.cancelAction = cancel
	done := make(chan struct{})
	m.actionDone = done
	m.actionBusy = true
	m.actionName = "send"
	m.beginCommandNotice("send")
	return func() tea.Msg {
		defer close(done)
		defer cancel()
		err := session.Send(ctx, text)
		return actionMsg{generation: generation, id: id, name: "send", err: err, result: client.Result{Text: "T3 acknowledged the message"}}
	}
}

func (m *Model) stop() tea.Cmd {
	if m.session == nil {
		m.problem = "No conversation selected"
		return nil
	}
	if m.stopBusy {
		m.notice = "Stop is already awaiting acknowledgment"
		return nil
	}
	m.stopBusy = true
	m.beginCommandNotice("stop")
	session, ctx, generation := m.session, m.sessionCtx, m.generation
	return func() tea.Msg {
		err := session.Stop(ctx)
		return actionMsg{generation: generation, stop: true, name: "stop", err: err, result: client.Result{Text: "Stop accepted · T3 queue held"}}
	}
}
