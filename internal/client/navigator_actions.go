package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
	"strings"
	"time"
)

type ThreadPreview struct {
	ProfileID, EnvironmentID, ThreadID, ObservedAt string
	Available                                      bool
	Unavailable                                    string
	Snapshot                                       *wire.Snapshot
}

func (a *Session) navigatorOffered(page NavigatorPage, thread string) (config.Environment, Thread, error) {
	env, err := a.navigatorEnvironment(page.ProfileID)
	if err != nil {
		return env, Thread{}, err
	}
	if env.EnvironmentID != page.EnvironmentID {
		return env, Thread{}, errors.New("navigation source changed; refresh before acting")
	}
	for _, row := range page.Page.Threads {
		if row.ID == thread {
			return env, row, nil
		}
	}
	return env, Thread{}, errors.New("conversation was not offered in this navigation page")
}

// NavigatorSearch is explicit server content/title search. Ordinary local
// filter typing never invokes it and unsupported source capability is visible.
func (a *Session) NavigatorSearch(ctx context.Context, profile, query string) (NavigatorPage, error) {
	query = strings.TrimSpace(query)
	if len(query) < 2 || len(query) > 200 {
		return NavigatorPage{}, errors.New("server search requires 2–200 bytes")
	}
	env, err := a.navigatorEnvironment(profile)
	if err != nil {
		return NavigatorPage{}, err
	}
	page, tree, err := a.browserMetadataRead(ctx, env, query, true)
	if err != nil {
		return NavigatorPage{}, err
	}
	return NavigatorPage{ProfileID: profile, EnvironmentID: env.EnvironmentID, Label: env.Label, Query: query, Page: page, Navigation: a.manager.navigation(env.EnvironmentID), Tree: tree}, nil
}

// NavigatorPreview owns a short, separately canceled read-only helper. It
// never borrows the foreground mutation connection or starts a subscription.
func (a *Session) NavigatorPreview(ctx context.Context, page NavigatorPage, thread string) (ThreadPreview, error) {
	env, _, err := a.navigatorOffered(page, thread)
	preview := ThreadPreview{ProfileID: page.ProfileID, EnvironmentID: page.EnvironmentID, ThreadID: thread}
	if err != nil {
		return preview, err
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	conn, err := wire.Start(ctx, env)
	if err != nil {
		return preview, err
	}
	defer closeNativeConnection(conn)
	hello, err := conn.Request(ctx, "hello", nil, "")
	if err != nil {
		return preview, err
	}
	if stringValue(hello["environmentId"]) != env.EnvironmentID || numberValue(hello["protocol"]) != 1 || numberValue(hello["orchestrationProtocol"]) != 2 {
		return preview, errors.New("preview helper identity or protocol mismatch")
	}
	features, _ := hello["features"].(map[string]any)
	if !boolValue(features["threadPreview"]) {
		preview.Unavailable = "Read-only preview unavailable on this adapter; metadata only. Enter opens the exact conversation."
		return preview, nil
	}
	result, err := conn.Request(ctx, "thread-preview", map[string]any{"threadId": thread}, "")
	if err != nil {
		return preview, err
	}
	if err = ctx.Err(); err != nil {
		return preview, err
	}
	if stringValue(result["threadId"]) != thread || !boolValue(result["readOnly"]) {
		return preview, errors.New("preview identity/read-only contract mismatch")
	}
	available, ok := result["available"].(bool)
	if !ok {
		return preview, errors.New("preview availability is unreported")
	}
	preview.Available = available
	preview.ObservedAt = stringValue(result["observedAt"])
	if _, err = time.Parse(time.RFC3339Nano, preview.ObservedAt); err != nil {
		return preview, errors.New("preview observation time is invalid")
	}
	if !available {
		preview.Unavailable = stringValue(result["unavailable"])
		if len(preview.Unavailable) > 4096 {
			return preview, errors.New("preview limitation exceeds bound")
		}
		return preview, nil
	}
	raw, err := json.Marshal(result["view"])
	if err != nil {
		return preview, err
	}
	snapshot, err := wire.DecodeSnapshot(raw)
	if err != nil {
		return preview, err
	}
	if snapshot.Metadata.ThreadID != thread || snapshot.Metadata.Live || snapshot.Metadata.Historical || snapshot.Metadata.State != "synchronizing" || len(snapshot.Metadata.Requests) != 0 || snapshot.Metadata.ActiveRunID != nil {
		return preview, errors.New("preview attempted to acquire control authority")
	}
	preview.Snapshot = snapshot
	return preview, nil
}

// NavigatorPin is an explicit native mutation against the displayed revision.
// Selection/preview never calls it. It uses the existing durable normal lane
// and the adapter's fresh revision check, without changing the attachment.
func (a *Session) NavigatorPin(ctx context.Context, page NavigatorPage, thread string) (Result, error) {
	env, row, err := a.navigatorOffered(page, thread)
	if err != nil {
		return Result{}, err
	}
	if row.Inbox == nil || row.Inbox.NativeRevision == "" {
		return Result{}, errors.New("native pin revision unavailable; refresh or use T3")
	}
	action := "pin"
	if row.Inbox.PinnedAt != "" {
		action = "unpin"
	}
	return a.navigatorDisposition(ctx, env, row, action)
}

// NavigatorReview uses the same revision-bound durable lane as native pinning.
// The adapter rejects hiding active work or unanswered requests.
func (a *Session) NavigatorReview(ctx context.Context, page NavigatorPage, thread, action string) (Result, error) {
	env, row, err := a.navigatorOffered(page, thread)
	if err != nil {
		return Result{}, err
	}
	if action != "settle" && action != "unsettle" {
		return Result{}, errors.New("review action was not offered")
	}
	if row.Inbox == nil || row.Inbox.NativeRevision == "" {
		return Result{}, errors.New("native review revision unavailable; refresh before acting")
	}
	if action == "settle" && (row.Inbox.PendingRequestCount > 0 || row.Inbox.PendingChildRequest || row.Inbox.QueuedCount > 0 || row.Inbox.Working != nil && *row.Inbox.Working) {
		return Result{}, errors.New("answer pending requests and finish native work before hiding this conversation")
	}
	return a.navigatorDisposition(ctx, env, row, action)
}

func (a *Session) navigatorDisposition(ctx context.Context, env config.Environment, row Thread, action string) (Result, error) {
	thread := row.ID
	conn, features, err := nativeConnectionCapabilities(ctx, env)
	if err != nil {
		return Result{}, err
	}
	defer closeNativeConnection(conn)
	capability := "threadPinning"
	if action == "settle" || action == "unsettle" {
		capability = "nativeWorkflows"
	}
	if !boolValue(features[capability]) {
		return Result{}, errors.New("native thread action unavailable on this host")
	}
	fields := map[string]any{"threadId": thread, "action": action, "expectedRevision": row.Inbox.NativeRevision}
	key := env.EnvironmentID + "/normal"
	a.manager.mu.Lock()
	if a.manager.closed || a.manager.inFlight[key] || a.manager.State.Pending(env.EnvironmentID, false) != nil {
		a.manager.mu.Unlock()
		return Result{}, errors.New("a saved or pending action needs review before changing visibility")
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
	id := wire.NewID()
	record := map[string]any{"environmentId": env.EnvironmentID, "id": id, "op": "thread-action", "threadId": thread, "action": action, "expectedRevision": row.Inbox.NativeRevision}
	if err = a.manager.State.SavePending(env.EnvironmentID, false, record); err != nil {
		return Result{}, fmt.Errorf("nothing sent: preserve thread action identity: %w", err)
	}
	reply, err := conn.Request(ctx, "thread-action", fields, id)
	if err != nil {
		if !wire.IsUncertain(err) {
			if clear := a.manager.State.RejectPending(env.EnvironmentID, false, id); clear != nil {
				err = errors.Join(err, clear)
			}
		}
		return Result{}, err
	}
	if stringValue(reply["threadId"]) != thread || stringValue(reply["action"]) != action {
		return Result{}, errors.New("thread action acknowledgement identity mismatch; original action remains saved")
	}
	if err = a.manager.State.Acknowledge(env.EnvironmentID, false, id, thread, ""); err != nil {
		return Result{}, err
	}
	text := "Pinned"
	if action == "unpin" {
		text = "Unpinned"
	}
	if action == "settle" {
		text = "Reviewed"
	}
	if action == "unsettle" {
		text = "Restored"
	}
	return Result{Text: text}, nil
}
