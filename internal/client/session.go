// Session state and lifecycle for the existing server-owned T3 conversation.
package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"math"
	"sync"
	"time"

	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
)

// Manager owns only the on-demand client connections. T3 keeps running when it
// closes. A new Session is lazy: discarded picker results own no processes.
type Manager struct {
	Environments []config.Environment
	State        *config.State
	mu           sync.Mutex
	// Serialize draft persistence and acknowledgment across attachments. The
	// canonical draft remains in drafts so a retiring listener cannot restore
	// text that a newer attachment changed or an acknowledgment accepted.
	draftPersistence sync.Mutex
	sessions         map[*Session]struct{}
	inFlight         map[string]bool
	drafts           map[draftIdentity]string
	operations       sync.WaitGroup
	listeners        sync.WaitGroup
	closed           bool
	nav              map[string]NavigationState
}

type draftIdentity struct{ env, thread string }

func NewManager(environments []config.Environment, state *config.State) *Manager {
	return &Manager{Environments: append([]config.Environment(nil), environments...), State: state,
		sessions: make(map[*Session]struct{}), inFlight: make(map[string]bool), drafts: make(map[draftIdentity]string)}
}

func (m *Manager) NewSession(environment, thread string) (*Session, error) {
	broker := newPromptBroker()
	a := &Session{manager: m, asker: broker, prompts: broker, notices: make(chan string, 8), wake: make(chan struct{}, 1),
		reconnect: make(chan struct{}, 1), thread: thread, delivery: "auto", connectionState: "select environment",
		seenRequests: make(map[string]bool)}
	if environment != "" {
		for _, env := range m.Environments {
			if env.ID == environment {
				copy := env
				a.environment = &copy
				break
			}
		}
		if a.environment == nil {
			return nil, fmt.Errorf("unknown environment %q", environment)
		}
	}
	if thread != "" && a.environment == nil {
		return nil, errors.New("--thread requires --env")
	}
	if a.environment != nil && thread != "" {
		a.store.Select(thread)
		a.draft = m.draft(a.environment.EnvironmentID, thread)
	}
	return a, nil
}

func (m *Manager) draft(env, thread string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if text, ok := m.drafts[draftIdentity{env, thread}]; ok {
		return text
	}
	return m.State.Draft(env, thread)
}

func (m *Manager) Close() error {
	m.mu.Lock()
	m.closed = true
	agents := make([]*Session, 0, len(m.sessions))
	for a := range m.sessions {
		agents = append(agents, a)
	}
	m.mu.Unlock()
	var errs []error
	for _, a := range agents {
		a.Close()
		if err := a.flushDraft(); err != nil {
			errs = append(errs, err)
		}
	}
	done := make(chan struct{})
	go func() { m.operations.Wait(); m.listeners.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		errs = append(errs, errors.New("operation shutdown incomplete; saved recovery records retained"))
	}
	m.draftPersistence.Lock()
	defer m.draftPersistence.Unlock()
	m.mu.Lock()
	drafts := make(map[draftIdentity]string, len(m.drafts))
	for key, text := range m.drafts {
		drafts[key] = text
	}
	m.mu.Unlock()
	for key, text := range drafts {
		if err := m.State.SetDraft(key.env, key.thread, text); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

type Thread struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Project     string        `json:"project"`
	Status      string        `json:"status"`
	Model       string        `json:"model"`
	Lineage     *wire.Lineage `json:"lineage,omitempty"`
	ParentTitle string        `json:"parentTitle,omitempty"`
	ChildCount  int           `json:"childCount,omitempty"`
	Depth       int           `json:"depth,omitempty"`
	Expanded    bool          `json:"expanded,omitempty"`
	ContextOnly bool          `json:"contextOnly,omitempty"`
}

// Session is one attachment to a selected T3 environment and conversation.
// Its operations may run concurrently, but only Events owns the read stream.
type Session struct {
	manager                    *Manager
	mu                         sync.Mutex
	environment                *config.Environment
	thread                     string
	conn                       *wire.Connection
	ctx                        context.Context
	cancel                     context.CancelFunc
	ready, writable            bool
	helloVerified              bool
	features                   map[string]bool
	started                    bool
	connectionState            string
	store                      wire.Store
	retained                   *wire.Snapshot
	cachedHistory              bool
	threads                    []Thread
	moreThreads                bool
	delivery                   string
	lastAction                 string
	lastError                  string
	draft                      string
	draftDirty                 bool
	draftVersion               uint64
	draftError                 error
	restoreDraft               bool
	acknowledgedDraft          *string
	navigating                 bool
	scrollGoal                 string
	uiRevision                 uint64
	requestCancel              context.CancelFunc
	requestID, requestRevision string
	requestGeneration          uint64
	activeRun                  string
	seenRequests               map[string]bool
	asker                      prompter
	prompts                    *promptBroker
	notices                    chan string
	wake                       chan struct{}
	reconnect                  chan struct{}
}

// Prompts and Notices are bounded channels. A UI must retire each prompt when
// its Done channel closes and ignore any event from an old attachment.
func (a *Session) Prompts() <-chan *Prompt { return a.prompts.requests }
func (a *Session) Notices() <-chan string  { return a.notices }

func (a *Session) Close() {
	a.mu.Lock()
	cancel, conn, requestCancel := a.cancel, a.conn, a.requestCancel
	a.ready, a.writable, a.helloVerified = false, false, false
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if requestCancel != nil {
		requestCancel()
	}
	if conn != nil {
		conn.Close()
		ctx, done := context.WithTimeout(context.Background(), 3*time.Second)
		_ = conn.WaitClosed(ctx)
		done()
	}
}

func (a *Session) signal() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}
func (a *Session) notice(s string) {
	select {
	case a.notices <- s:
	default:
		select {
		case <-a.notices:
		default:
		}
		select {
		case a.notices <- s:
		default:
		}
	}
}

func (a *Session) Events(parent context.Context) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		ctx, cancel := context.WithCancel(parent)
		a.manager.mu.Lock()
		if a.manager.closed {
			a.manager.mu.Unlock()
			cancel()
			return
		}
		a.mu.Lock()
		if a.started {
			a.mu.Unlock()
			a.manager.mu.Unlock()
			cancel()
			yield(Event{}, errors.New("session already attached; create a new session to reconnect"))
			return
		}
		a.started = true
		a.mu.Unlock()
		a.manager.sessions[a] = struct{}{}
		a.manager.listeners.Add(1)
		a.mu.Lock()
		a.ctx, a.cancel = ctx, cancel
		a.mu.Unlock()
		a.manager.mu.Unlock()
		_ = a.Draft()
		a.mu.Lock()
		a.restoreDraft = a.thread != ""
		a.mu.Unlock()
		defer func() {
			cancel()
			a.mu.Lock()
			conn := a.conn
			a.conn = nil
			a.ready, a.writable, a.helloVerified = false, false, false
			a.mu.Unlock()
			if conn != nil {
				conn.Close()
				wait, done := context.WithTimeout(context.Background(), 3*time.Second)
				_ = conn.WaitClosed(wait)
				done()
			}
			if err := a.flushDraft(); err != nil {
				a.notice("Draft save failed: " + err.Error())
			}
			a.manager.mu.Lock()
			delete(a.manager.sessions, a)
			a.manager.mu.Unlock()
			a.manager.listeners.Done()
		}()
		if !yield(a.renderEvent(), nil) {
			return
		}
		a.mu.Lock()
		needsEnvironment := a.environment == nil
		a.mu.Unlock()
		if needsEnvironment {
			env, err := a.chooseEnvironment(ctx)
			if err != nil {
				if ctx.Err() == nil {
					a.notice(err.Error())
				}
				<-ctx.Done()
				return
			}
			a.mu.Lock()
			a.environment = &env
			a.mu.Unlock()
		}
		delay := time.Second
		for ctx.Err() == nil {
			a.mu.Lock()
			env := *a.environment
			a.connectionState = "connecting"
			a.ready, a.writable, a.helloVerified = false, false, false
			a.mu.Unlock()
			if !yield(a.renderEvent(), nil) {
				return
			}
			conn, err := wire.Start(ctx, env)
			if err == nil {
				a.mu.Lock()
				if snapshot := a.store.Snapshot(); snapshot != nil {
					a.retained = snapshot
				}
				a.cachedHistory = a.retained != nil && a.retained.Metadata.Historical
				a.store.Reset()
				a.store.Select(a.thread)
				a.conn = conn
				a.connectionState = "synchronizing"
				a.mu.Unlock()
				connectionCtx, cancelConnection := context.WithCancel(ctx)
				setup := make(chan error, 1)
				go func() { setup <- a.prepare(connectionCtx, conn) }()
				var synchronized bool
				synchronized, err = a.consume(connectionCtx, conn, setup, yield)
				if synchronized {
					delay = time.Second
				}
				cancelConnection()
				conn.Close()
				wait, done := context.WithTimeout(context.Background(), 3*time.Second)
				_ = conn.WaitClosed(wait)
				done()
				a.mu.Lock()
				if a.conn == conn {
					a.conn = nil
				}
				a.ready, a.writable, a.helloVerified = false, false, false
				a.connectionState = "reconnecting"
				a.mu.Unlock()
			}
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				a.notice("Connection: " + err.Error() + ". Pending operations remain saved; /retry is explicit.")
			}
			if !yield(a.renderEvent(), nil) {
				return
			}
			var fault *wire.Fault
			blocked := errors.As(err, &fault) && (fault.Code == "identity_mismatch" || fault.Code == "incompatible" || fault.Code == "authorization")
			if blocked {
				a.mu.Lock()
				a.connectionState = "blocked · /reconnect after fixing configuration"
				a.mu.Unlock()
				if !yield(a.renderEvent(), nil) {
					return
				}
				select {
				case <-ctx.Done():
					return
				case <-a.reconnect:
				}
			} else {
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-a.reconnect:
					timer.Stop()
				case <-timer.C:
				}
				if delay < 16*time.Second {
					delay *= 2
				}
			}
		}
	}
}

func (a *Session) prepare(ctx context.Context, conn *wire.Connection) error {
	hello, err := conn.Request(ctx, "hello", nil, "")
	if err != nil {
		return err
	}
	a.mu.Lock()
	if a.conn != conn || ctx.Err() != nil {
		a.mu.Unlock()
		return context.Canceled
	}
	if stringValue(hello["environmentId"]) != a.environment.EnvironmentID || numberValue(hello["protocol"]) != 1 || numberValue(hello["orchestrationProtocol"]) != 2 {
		a.mu.Unlock()
		return &wire.Fault{Code: "identity_mismatch", Message: "adapter handshake identity or protocol mismatch"}
	}
	a.ready, a.writable, a.helloVerified = true, boolValue(hello["writesEnabled"]), true
	a.features = make(map[string]bool)
	if features, ok := hello["features"].(map[string]any); ok {
		for k, v := range features {
			a.features[k] = boolValue(v)
		}
	}
	a.connectionState = "connected"
	thread, historical := a.thread, a.cachedHistory
	a.mu.Unlock()
	a.signal()
	threads, err := a.List(ctx, "")
	if err != nil {
		return err
	}
	if thread == "" {
		thread, err = a.chooseThreadFrom(ctx, threads)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			a.notice("Choose a conversation with /threads or /switch.")
			return nil
		}
		a.mu.Lock()
		environmentID := a.environment.EnvironmentID
		a.mu.Unlock()
		draft := a.manager.draft(environmentID, thread)
		a.mu.Lock()
		if a.conn != conn || ctx.Err() != nil {
			a.mu.Unlock()
			return context.Canceled
		}
		a.thread = thread
		a.store.Select(thread)
		a.draft = draft
		a.restoreDraft = true
		a.mu.Unlock()
		a.signal()
	}
	if historical {
		a.notice("Earlier history retained. Live detail stays paused; /latest returns to the live conversation.")
		return nil
	}
	a.mu.Lock()
	a.scrollGoal = "live"
	a.mu.Unlock()
	_, err = conn.Request(ctx, "watch", map[string]any{"threadId": thread}, "")
	return err
}

func (a *Session) consume(ctx context.Context, conn *wire.Connection, setup <-chan error, yield func(Event, error) bool) (bool, error) {
	synchronized := false
	ticker := time.NewTicker(350 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return synchronized, ctx.Err()
		case <-a.reconnect:
			return synchronized, nil
		case <-conn.Done():
			return synchronized, conn.Err()
		case err := <-setup:
			setup = nil
			if err != nil {
				return synchronized, err
			}
			synchronized = true
			if !yield(a.renderEvent(), nil) {
				return synchronized, context.Canceled
			}
		case <-ticker.C:
			if err := a.flushDraft(); err != nil {
				a.notice("Draft save failed; sending is blocked: " + err.Error())
			}
		case <-a.wake:
			if !yield(a.renderEvent(), nil) {
				return synchronized, context.Canceled
			}
		case packet, ok := <-conn.Events():
			if !ok {
				return synchronized, conn.Err()
			}
			if err := a.applyPacket(packet); err != nil {
				a.notice("Conversation update rejected: " + err.Error() + ". Acquiring a fresh bounded snapshot.")
				a.mu.Lock()
				historical := a.cachedHistory || a.scrollGoal == "historical" || (a.retained != nil && a.retained.Metadata.Historical)
				a.mu.Unlock()
				if !historical {
					go func() {
						if err := a.Navigate(ctx, false); err != nil && ctx.Err() == nil {
							a.notice(err.Error())
						}
					}()
				}
			}
			if !yield(a.renderEvent(), nil) {
				return synchronized, context.Canceled
			}
		}
	}
}

func (a *Session) applyPacket(packet wire.Packet) error {
	if packet.Event == "view" || packet.Event == "delta" {
		changed, err := a.store.Apply(packet.Event, packet.Data)
		if err != nil {
			return err
		}
		if changed {
			s := a.store.Snapshot()
			a.mu.Lock()
			a.retained = s
			a.cachedHistory = false
			if !s.Metadata.Historical {
				if s.Metadata.ActiveRunID != nil {
					a.activeRun = *s.Metadata.ActiveRunID
				} else if s.Metadata.Live {
					a.activeRun = ""
				}
			}
			if a.requestCancel != nil && !containsRequest(s, a.requestID, a.requestRevision) {
				a.requestCancel()
				a.requestCancel = nil
			}
			currentRequests := make(map[string]bool, len(s.Metadata.Requests))
			for _, request := range s.Metadata.Requests {
				key := stringValue(request["id"]) + "/" + stringValue(request["revision"])
				currentRequests[key] = true
				if !a.seenRequests[key] {
					a.seenRequests[key] = true
					a.notice("T3 awaits your input. /requests to review; Ctrl+S stops the active turn with queue hold.")
				}
			}
			a.seenRequests = currentRequests
			a.mu.Unlock()
		}
	} else if packet.Event == "status" {
		var status map[string]any
		if err := json.Unmarshal(packet.Data, &status); err != nil {
			return err
		}
		a.mu.Lock()
		a.connectionState = stringValue(status["state"])
		if a.connectionState == "connected" {
			a.ready = a.helloVerified
			a.writable = a.helloVerified && boolValue(status["writesEnabled"])
			if !a.helloVerified {
				a.connectionState = "synchronizing identity"
			}
		} else if a.connectionState != "unavailable" {
			a.ready, a.writable = false, false
		}
		a.mu.Unlock()
		if message := stringValue(status["message"]); message != "" {
			a.notice(message)
		}
	}
	return nil
}

func (a *Session) snapshot() *wire.Snapshot {
	if s := a.store.Snapshot(); s != nil {
		return s
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.retained
}

func (a *Session) renderEvent() Event {
	s := a.store.Snapshot()
	current := s != nil
	a.mu.Lock()
	defer a.mu.Unlock()
	if s == nil {
		s = a.retained
	}
	a.uiRevision++
	event := Event{Revision: a.uiRevision, Snapshot: s, Status: a.statusLocked(s), Error: a.lastError}
	// A reconnect keeps the old page readable, but that cached page cannot
	// complete the new watch/navigation request or consume its scroll goal.
	if current && a.scrollGoal == s.Metadata.State {
		event.ResetScroll = true
		event.Follow = a.scrollGoal == "live"
		a.scrollGoal = ""
	}
	if a.restoreDraft {
		value := a.draft
		event.Draft = &value
		a.restoreDraft = false
	}
	if a.acknowledgedDraft != nil {
		value := *a.acknowledgedDraft
		event.DraftAccepted = &value
		a.acknowledgedDraft = nil
	}
	return event
}

func (a *Session) Status() Status {
	s := a.snapshot()
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.statusLocked(s)
}

func (a *Session) statusLocked(s *wire.Snapshot) Status {
	contextLine := "Choose environment"
	if a.environment != nil {
		contextLine = a.environment.Label
	}
	state := a.connectionState
	model := ""
	if s != nil {
		contextLine += " / " + s.Metadata.Title
		model = s.Metadata.Model
		if s.Metadata.ModelSelection != nil {
			model = s.Metadata.ModelSelection.Summary()
		}
		switch s.Metadata.Lineage.Type() {
		case "subagent":
			contextLine += " [SUBAGENT]"
		case "fork":
			contextLine += " [FORK]"
		}
		if a.ready {
			if s.Metadata.Historical {
				state = "earlier · live paused"
			} else {
				state = s.Metadata.Status
				if !s.Metadata.Live {
					state = s.Metadata.State
				}
			}
		}
		if len(s.Metadata.Requests) > 0 {
			state += fmt.Sprintf(" · awaiting input (%d)", len(s.Metadata.Requests))
		}
	}
	if a.environment != nil {
		if a.ready && !a.writable {
			state += " · read-only"
		}
		if p := a.manager.State.Pending(a.environment.EnvironmentID, false); p != nil {
			state += " · saved " + stringValue(p["op"]) + " /retry"
		}
		if a.manager.State.Pending(a.environment.EnvironmentID, true) != nil {
			state += " · Stop pending /retry stop"
		}
	}
	if a.draftError != nil {
		state += " · draft save failed"
	}
	if a.navigating {
		state = "synchronizing"
	}
	state += " · " + a.delivery
	if a.lastAction != "" {
		state += " · " + a.lastAction
	}
	status := Status{Context: contextLine, ModelName: model, State: state, ThreadID: a.thread, Delivery: a.delivery, Connected: a.ready, Writable: a.ready && a.writable}
	status.ModelCatalog = a.ready && a.features["modelCatalog"]
	status.ModelControls = status.Writable && a.features["modelSelection"]
	status.ThreadNavigation = a.ready && a.features["threadNavigation"]
	status.RuntimeDetails = s != nil && a.features["runtimeDetails"]
	if s != nil && s.Metadata.Lineage != nil && s.Metadata.Lineage.ParentThreadID != nil {
		status.ParentAvailable = *s.Metadata.Lineage.ParentThreadID != s.Metadata.ThreadID
	}
	if a.environment != nil {
		status.ProfileID = a.environment.ID
		status.EnvironmentID = a.environment.EnvironmentID
		if o := a.manager.State.Override(a.environment.EnvironmentID, a.thread); o != nil {
			status.NextModel = o.Selection.Summary()
		}
	}
	if s != nil {
		status.Requests = len(s.Metadata.Requests)
	}
	return status
}

func (a *Session) Draft() string {
	a.manager.mu.Lock()
	defer a.manager.mu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.environment != nil && a.thread != "" {
		key := draftIdentity{a.environment.EnvironmentID, a.thread}
		if value, ok := a.manager.drafts[key]; ok {
			a.draft = value
		} else {
			a.draft = a.manager.State.Draft(key.env, key.thread)
		}
	}
	return a.draft
}

// acceptDraft publishes the exact acknowledged text to the current attachment,
// including when the connection that sent it has since been detached. Both the
// session and terminal UI compare before clearing so newer typing survives.
func (m *Manager) acceptDraft(env, thread, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := draftIdentity{env, thread}
	if m.drafts[key] == message {
		m.drafts[key] = ""
	}
	for current := range m.sessions {
		current.mu.Lock()
		if current.environment != nil && current.environment.EnvironmentID == env && current.thread == thread {
			if current.draft == message {
				current.draft = ""
				current.draftDirty = false
				current.draftVersion++
			}
			accepted := message
			current.acknowledgedDraft = &accepted
			current.signal()
		}
		current.mu.Unlock()
	}
}

func (a *Session) rememberError(err error) {
	a.mu.Lock()
	if err != nil && !errors.Is(err, context.Canceled) {
		a.lastError = err.Error()
	} else if err == nil {
		a.lastError = ""
	}
	a.mu.Unlock()
	a.signal()
}
func (a *Session) SetDraft(text string) error {
	if len(text) > 64*1024 {
		return errors.New("draft exceeds 64 KiB")
	}
	a.manager.mu.Lock()
	defer a.manager.mu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.draft != text {
		a.draft = text
		a.draftDirty = true
		a.draftVersion++
		if a.environment != nil && a.thread != "" {
			a.manager.drafts[draftIdentity{a.environment.EnvironmentID, a.thread}] = text
		}
	}
	return a.draftError
}

func (a *Session) flushDraft() error {
	a.manager.draftPersistence.Lock()
	defer a.manager.draftPersistence.Unlock()
	a.manager.mu.Lock()
	a.mu.Lock()
	if !a.draftDirty || a.environment == nil || a.thread == "" {
		err := a.draftError
		a.mu.Unlock()
		a.manager.mu.Unlock()
		return err
	}
	text, version, env, thread := a.draft, a.draftVersion, a.environment.EnvironmentID, a.thread
	if current, ok := a.manager.drafts[draftIdentity{env, thread}]; ok {
		text = current
	}
	a.mu.Unlock()
	a.manager.mu.Unlock()
	err := a.manager.State.SetDraft(env, thread, text)
	a.mu.Lock()
	a.draftError = err
	if err == nil && version == a.draftVersion {
		a.draftDirty = false
	}
	a.mu.Unlock()
	return err
}

func stringValue(v any) string { s, _ := v.(string); return s }
func boolValue(v any) bool     { b, _ := v.(bool); return b }
func numberValue(v any) int64 {
	switch n := v.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) || math.Trunc(n) != n || n < 0 || n > float64(wire.MaxInteger) {
			return -1
		}
		return int64(n)
	case int64:
		if n < 0 || n > wire.MaxInteger {
			return -1
		}
		return n
	case int:
		if n < 0 || int64(n) > wire.MaxInteger {
			return -1
		}
		return int64(n)
	case json.Number:
		i, err := n.Int64()
		if err != nil || i < 0 || i > wire.MaxInteger {
			return -1
		}
		return i
	}
	return -1
}
func containsRequest(s *wire.Snapshot, id, revision string) bool {
	if s == nil || !s.Metadata.Live {
		return false
	}
	for _, r := range s.Metadata.Requests {
		if stringValue(r["id"]) == id && stringValue(r["revision"]) == revision {
			return true
		}
	}
	return false
}

// Target saves the outgoing draft before creating a lazy new attachment.
// Call it from a command goroutine, never from the rendering path.
func (a *Session) Target(envID, threadID string) (*Session, error) {
	if err := a.flushDraft(); err != nil {
		return nil, fmt.Errorf("save the current draft before switching: %w", err)
	}
	return a.manager.NewSession(envID, threadID)
}

func (a *Session) chooseEnvironment(ctx context.Context) (config.Environment, error) {
	if len(a.manager.Environments) == 1 {
		return a.manager.Environments[0], nil
	}
	choices := make([]Choice, 0, len(a.manager.Environments))
	for _, env := range a.manager.Environments {
		description := "local stdio"
		if env.SSHDestination != nil {
			description = "SSH · " + *env.SSHDestination
		}
		choices = append(choices, Choice{ID: env.ID, Label: env.Label, Description: description})
	}
	answer, err := a.asker.Ask(ctx, Question{Kind: ChooseOne, Title: "Environments", Prompt: "Where is the T3 conversation?", Choices: choices})
	if err != nil {
		return config.Environment{}, err
	}
	if !answer.Cancelled && len(answer.ChoiceIDs) == 1 {
		for _, env := range a.manager.Environments {
			if env.ID == answer.ChoiceIDs[0] {
				return env, nil
			}
		}
	}
	return config.Environment{}, errors.New("environment selection dismissed")
}
