package client

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Xon333/t3oxide/internal/wire"
)

// Send submits exactly one human-authored message. The UI invokes it off its
// event loop; T3 alone chooses delivery and owns the durable queue.
func (a *Session) Send(ctx context.Context, message string) (err error) {
	defer func() { a.rememberError(err) }()
	if strings.TrimSpace(message) == "" || len(message) > 64*1024 {
		return errors.New("message must contain text and be at most 64 KiB")
	}
	if err := a.flushDraft(); err != nil {
		return err
	}
	a.mu.Lock()
	sessionCtx, thread, mode := a.ctx, a.thread, a.delivery
	a.mu.Unlock()
	if sessionCtx == nil || ctx == nil {
		return errors.New("select a live conversation first")
	}
	fields := map[string]any{"threadId": thread, "message": message, "mode": mode}
	a.mu.Lock()
	env := a.environment
	enabled := a.features["messageModelOverride"]
	a.mu.Unlock()
	if env != nil {
		if override := a.manager.State.Override(env.EnvironmentID, thread); override != nil {
			if !enabled {
				return errors.New("this host cannot submit the staged model override; adopt its bundled adapter or /override clear")
			}
			fields["modelSelection"] = override.Selection
			fields["modelOverrideId"] = override.ID
		}
	}
	_, err = a.perform(ctx, "send", fields, "", false)
	return err
}

// Stop is an independent operation lane. It is never modeled as canceling
// Events(ctx): that would only detach the client, leaving the remote run alive.
func (a *Session) Stop(ctx context.Context) (err error) {
	defer func() { a.rememberError(err) }()
	a.mu.Lock()
	run, thread := a.activeRun, a.thread
	a.mu.Unlock()
	if run == "" || thread == "" {
		return errors.New("no reviewed active turn; use /latest first")
	}
	_, err = a.perform(ctx, "stop", map[string]any{"threadId": thread, "runId": run}, "", true)
	return err
}

func (a *Session) perform(ctx context.Context, op string, fields map[string]any, savedID string, stopLane bool) (map[string]any, error) {
	// A retained page is for uninterrupted reading across reconnects. Only
	// Stop may use its reviewed run before the new helper supplies a view;
	// ordinary mutations need the current helper's authoritative snapshot.
	s := a.store.Snapshot()
	if s == nil && op == "stop" {
		s = a.snapshot()
	}
	a.mu.Lock()
	if a.environment == nil || a.conn == nil || !a.helloVerified || !a.ready || !a.writable || a.thread == "" {
		a.mu.Unlock()
		return nil, errors.New("writes unavailable: select a connected, writable conversation")
	}
	conn, env, thread, sessionCtx := a.conn, *a.environment, a.thread, a.ctx
	if s == nil || stringValue(fields["threadId"]) != thread || (op != "stop" && (!s.Metadata.Live || s.Metadata.Historical || a.cachedHistory || a.store.NeedsResnapshot() || a.navigating)) {
		a.mu.Unlock()
		return nil, errors.New("conversation is not synchronized; /latest before this action")
	}
	if a.draftError != nil && op != "stop" {
		err := a.draftError
		a.mu.Unlock()
		return nil, fmt.Errorf("save draft before sending: %w", err)
	}
	a.mu.Unlock()
	if sessionCtx == nil || ctx == nil {
		return nil, errors.New("conversation is detached")
	}
	if err := sessionCtx.Err(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	requestCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stopCancel := context.AfterFunc(sessionCtx, cancel)
	defer stopCancel()
	id := savedID
	if id == "" {
		id = wire.NewID()
	}
	key := env.EnvironmentID + "/normal"
	if stopLane {
		key = env.EnvironmentID + "/stop"
	}
	a.manager.mu.Lock()
	if a.manager.closed {
		a.manager.mu.Unlock()
		return nil, errors.New("client is shutting down; no new operation sent")
	}
	if a.manager.inFlight[key] {
		a.manager.mu.Unlock()
		return nil, errors.New("an operation in this lane is still awaiting acknowledgement")
	}
	previous := a.manager.State.Pending(env.EnvironmentID, stopLane)
	if op == "stop" && stopLane {
		if legacy := a.manager.State.Pending(env.EnvironmentID, false); legacy != nil && stringValue(legacy["op"]) == "stop" {
			a.manager.mu.Unlock()
			return nil, errors.New("an older saved Stop is in the regular lane; use /retry before another Stop")
		}
	}
	if previous != nil && (savedID == "" || stringValue(previous["id"]) != savedID) {
		a.manager.mu.Unlock()
		return nil, errors.New("a saved operation needs explicit /retry or /resolve before another action")
	}
	a.manager.inFlight[key] = true
	a.manager.operations.Add(1)
	a.manager.mu.Unlock()
	defer func() {
		a.manager.mu.Lock()
		delete(a.manager.inFlight, key)
		a.manager.mu.Unlock()
		a.manager.operations.Done()
		a.signal()
	}()
	record := make(map[string]any, len(fields)+3)
	for k, v := range fields {
		record[k] = v
	}
	record["id"], record["op"], record["environmentId"] = id, op, env.EnvironmentID
	if err := a.manager.State.SavePending(env.EnvironmentID, stopLane, record); err != nil {
		return nil, fmt.Errorf("nothing sent: save operation identity: %w", err)
	}
	a.mu.Lock()
	a.lastAction = op + " awaiting acknowledgement"
	if op == "send" {
		a.beginReceiptLocked(id, thread, s)
	}
	a.mu.Unlock()
	a.signal()
	result, err := conn.Request(requestCtx, op, fields, id)
	if err != nil {
		// A negative reply to a retry cannot disprove an earlier acceptance.
		// Only a definitely rejected first attempt may release the local lane.
		if savedID == "" && !wire.IsUncertain(err) {
			if clearErr := a.manager.State.ClearPending(env.EnvironmentID, stopLane, id); clearErr != nil {
				err = errors.Join(err, clearErr)
			}
		}
		a.mu.Lock()
		a.lastAction = "action unconfirmed"
		if op == "send" && a.receipt != nil && a.receipt.OperationID == id && (a.receipt.Phase == "sending" || a.receipt.Phase == "acknowledged") {
			a.receipt.Phase = "uncertain"
		}
		if savedID == "" && !wire.IsUncertain(err) {
			a.lastAction = "action rejected"
			if op == "send" && a.receipt != nil && a.receipt.OperationID == id {
				a.receipt.Phase = "rejected"
			}
		}
		a.mu.Unlock()
		if savedID != "" || wire.IsUncertain(err) {
			return nil, fmt.Errorf("%w; operation %s is saved for explicit same-ID retry", err, id)
		}
		return nil, err
	}
	if (op == "launch" && stringValue(result["threadId"]) == "") || (op != "launch" && numberValue(result["sequence"]) < 0) {
		return nil, errors.New("unrecognized acknowledgement; operation remains saved for explicit retry")
	}
	message := ""
	if op == "send" {
		message = stringValue(fields["message"])
	} else if op == "launch" {
		message = stringValue(fields["message"])
	}
	a.manager.draftPersistence.Lock()
	ackErr := a.manager.State.Acknowledge(env.EnvironmentID, stopLane, id, thread, message)
	if ackErr != nil {
		a.manager.draftPersistence.Unlock()
		return nil, fmt.Errorf("T3 acknowledged, but local recovery record could not be cleared; retry retains the same ID: %w", ackErr)
	}
	if op == "send" {
		a.mu.Lock()
		if a.draft == message {
			a.draft = ""
			a.draftDirty = false
			a.draftVersion++
		}
		a.mu.Unlock()
		a.manager.acceptDraft(env.EnvironmentID, thread, message)
	}
	a.manager.draftPersistence.Unlock()
	currentSnapshot := a.store.Snapshot()
	a.mu.Lock()
	a.lastAction = op + " accepted"
	if op == "send" {
		a.acknowledgeReceiptLocked(id, numberValue(result["sequence"]), currentSnapshot)
		if a.receipt != nil && (a.receipt.Phase == "responding" || receiptTerminal(a.receipt.Phase)) {
			a.lastAction = ""
		}
	}
	if op == "stop" {
		a.lastAction = "Stop accepted · queue held"
	}
	a.mu.Unlock()
	return result, nil
}

// Retry uses the original operation record byte-for-byte semantically. It never
// derives a new run/request target from today's controls.
func (a *Session) Retry(ctx context.Context, stop bool) (map[string]any, error) {
	a.mu.Lock()
	env, thread := a.environment, a.thread
	a.mu.Unlock()
	if env == nil {
		return nil, errors.New("select an environment")
	}
	record := a.manager.State.Pending(env.EnvironmentID, stop)
	if record == nil {
		return nil, errors.New("no saved operation in this lane")
	}
	if stringValue(record["threadId"]) != thread {
		return nil, fmt.Errorf("saved operation belongs to thread %s; select it before retrying", stringValue(record["threadId"]))
	}
	id, op := stringValue(record["id"]), stringValue(record["op"])
	delete(record, "id")
	delete(record, "op")
	delete(record, "environmentId")
	return a.perform(ctx, op, record, id, stop)
}

func (a *Session) Resolve(ctx context.Context, stop bool) error {
	a.mu.Lock()
	env := a.environment
	a.mu.Unlock()
	if env == nil {
		return errors.New("select an environment")
	}
	record := a.manager.State.Pending(env.EnvironmentID, stop)
	if record == nil {
		return errors.New("no saved operation in this lane")
	}
	id := stringValue(record["id"])
	answer, err := a.asker.Ask(ctx, Question{Kind: ChooseOne, Title: "Resolve saved action", Prompt: fmt.Sprintf("First inspect the conversation in full T3. Clear only local retry record %s? This does not undo, resend, or cancel server work. Your draft is retained.", id), Choices: []Choice{{ID: "keep", Label: "Keep saved action"}, {ID: "clear", Label: "I reviewed T3 — clear local record"}}})
	if err != nil {
		return err
	}
	if answer.Cancelled || len(answer.ChoiceIDs) != 1 || answer.ChoiceIDs[0] != "clear" {
		return nil
	}
	key := env.EnvironmentID + "/normal"
	if stop {
		key = env.EnvironmentID + "/stop"
	}
	a.manager.mu.Lock()
	defer a.manager.mu.Unlock()
	if a.manager.inFlight[key] {
		return errors.New("operation still in flight; wait for its acknowledgement")
	}
	return a.manager.State.ClearPending(env.EnvironmentID, stop, id)
}

func (a *Session) List(ctx context.Context, query string) ([]Thread, error) {
	a.mu.Lock()
	conn := a.conn
	env := a.environment
	navigation := a.features["threadNavigation"]
	a.mu.Unlock()
	if conn == nil {
		return nil, errors.New("adapter is not connected")
	}
	fields := map[string]any{"query": query}
	if navigation && env != nil {
		n := a.manager.navigation(env.EnvironmentID)
		expanded := []string{}
		for id, open := range n.Expanded {
			if open {
				expanded = append(expanded, id)
			}
		}
		sort.Strings(expanded)
		fields["grouped"] = true
		fields["typeFilter"] = n.Filter
		fields["expanded"] = expanded
	}
	result, err := conn.Request(ctx, "list", fields, "")
	if err != nil {
		return nil, err
	}
	threads, more, err := DecodeThreads(result)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	if a.conn == conn {
		a.threads = threads
		a.moreThreads = more
	}
	a.mu.Unlock()
	return threads, nil
}

func (a *Session) filteredTarget(ctx context.Context, query string) (*Session, error) {
	a.mu.Lock()
	env := a.environment
	a.mu.Unlock()
	if ctx == nil || env == nil {
		return nil, errors.New("select an environment")
	}
	threads, err := a.List(ctx, query)
	if err != nil {
		return nil, err
	}
	id, err := a.chooseThreadFrom(ctx, threads, query)
	if err != nil {
		return nil, err
	}
	return a.Target(env.ID, id)
}

// Navigate preserves replacement pagination: Earlier pauses detail updates;
// Latest deliberately reacquires a bounded live snapshot. No page is appended.
func (a *Session) Navigate(ctx context.Context, earlier bool) error {
	s := a.snapshot()
	a.mu.Lock()
	if a.conn == nil || !a.ready || a.thread == "" {
		a.mu.Unlock()
		return errors.New("select a connected conversation")
	}
	if a.navigating {
		a.mu.Unlock()
		return errors.New("navigation is already synchronizing")
	}
	conn, thread := a.conn, a.thread
	op := "watch"
	fields := map[string]any{"threadId": thread}
	goal := "live"
	if earlier {
		if a.cachedHistory {
			a.mu.Unlock()
			return errors.New("cached earlier page survives reconnect; use /latest before requesting another earlier page")
		}
		if s == nil || s.Metadata.HistoryCursor == nil || a.store.NeedsResnapshot() {
			a.mu.Unlock()
			return errors.New("no earlier page is available")
		}
		op = "history"
		fields = map[string]any{"cursor": *s.Metadata.HistoryCursor}
		goal = "historical"
	} else if a.cachedHistory {
		a.store.Reset()
		a.store.Select(thread)
		a.cachedHistory = false
	}
	a.navigating = true
	a.scrollGoal = goal
	if a.requestCancel != nil {
		a.requestCancel()
		a.requestCancel = nil
	}
	a.mu.Unlock()
	a.signal()
	defer func() { a.mu.Lock(); a.navigating = false; a.mu.Unlock(); a.signal() }()
	_, err := conn.Request(ctx, op, fields, "")
	return err
}

func (a *Session) SetDelivery(mode string) error {
	if mode != "auto" && mode != "queue" && mode != "steer" {
		return errors.New("use /delivery auto, /delivery queue, or /delivery steer")
	}
	a.mu.Lock()
	a.delivery = mode
	a.mu.Unlock()
	a.signal()
	return nil
}
