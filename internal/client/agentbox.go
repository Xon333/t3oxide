package client

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
)

// InboxTarget retains native identities separately. A row is a real native T3
// thread; it is never assigned a client-created task or run identity.
type InboxTarget struct {
	ProfileID, EnvironmentID, ProjectID, ThreadID, Revision string
}

func (t InboxTarget) Key() string {
	return config.InboxIdentity(t.EnvironmentID, t.ProjectID, t.ThreadID)
}

type InboxThread struct {
	Target      InboxTarget
	Environment string
	Thread      Thread
}

type InboxSource struct {
	ProfileID, EnvironmentID, Label, Error string
	Page                                   ThreadPage
	NativeWorkflows                        bool
}

type InboxPage struct {
	Sources []InboxSource
	Rows    []InboxThread
}

// InboxRead reads one bounded native metadata page per configured environment,
// with at most three helpers in flight. It does not subscribe any transcript,
// alter Developer navigation, retry writes, or dispatch pending work.
func (a *Session) InboxRead(ctx context.Context, query string, cursors map[string]int) (InboxPage, error) {
	return a.InboxReadProgress(ctx, query, cursors, nil)
}

func (a *Session) InboxSources() []InboxSource {
	if a == nil || a.manager == nil {
		return nil
	}
	var sources []InboxSource
	for _, env := range a.manager.Environments {
		sources = append(sources, InboxSource{ProfileID: env.ID, EnvironmentID: env.EnvironmentID, Label: env.Label})
	}
	return sources
}

// InboxReadProgress publishes each independent source as soon as it completes.
// publish may be called concurrently and must return promptly or respect ctx.
func (a *Session) InboxReadProgress(ctx context.Context, query string, cursors map[string]int, publish func(InboxSource)) (InboxPage, error) {
	return a.inboxReadProgress(ctx, query, cursors, publish, false)
}

// InboxSearchProgress uses the same bounded, progressive metadata admission
// lane for an explicit native message search. It never subscribes a result or
// changes an attachment, and an incomplete native result stays incomplete.
func (a *Session) InboxSearchProgress(ctx context.Context, query string, publish func(InboxSource)) (InboxPage, error) {
	query = strings.TrimSpace(query)
	if len(query) < 2 || len(query) > 200 {
		return InboxPage{}, errors.New("server search requires 2–200 bytes")
	}
	return a.inboxReadProgress(ctx, query, nil, publish, true)
}

func (a *Session) inboxReadProgress(ctx context.Context, query string, cursors map[string]int, publish func(InboxSource), serverSearch bool) (InboxPage, error) {
	if ctx == nil || a == nil || a.manager == nil {
		return InboxPage{}, errors.New("inbox needs a configured client")
	}
	if len(query) > 256 {
		return InboxPage{}, errors.New("inbox search exceeds 256 bytes")
	}
	environments := append([]config.Environment(nil), a.manager.Environments...)
	page := InboxPage{Sources: make([]InboxSource, len(environments))}
	gate := make(chan struct{}, 3)
	var work sync.WaitGroup
	for i, env := range environments {
		work.Add(1)
		go func(index int, env config.Environment) {
			defer work.Done()
			source := InboxSource{ProfileID: env.ID, EnvironmentID: env.EnvironmentID, Label: env.Label}
			defer func() {
				page.Sources[index] = source
				if publish != nil && ctx.Err() == nil {
					publish(source)
				}
			}()
			select {
			case gate <- struct{}{}:
				defer func() { <-gate }()
			case <-ctx.Done():
				source.Error = ctx.Err().Error()
				return
			}
			cursor := cursors[env.ID]
			if cursor < 0 || cursor > 1000000 {
				source.Error = "invalid inbox page cursor"
				return
			}
			result, enabled, err := a.inboxMetadataRead(ctx, env, query, cursor, serverSearch)
			source.Page, source.NativeWorkflows = result, enabled
			if err != nil {
				source.Error = err.Error()
			}
		}(i, env)
	}
	work.Wait()
	if err := ctx.Err(); err != nil {
		return InboxPage{}, err
	}
	return inboxPageFromSources(page.Sources), nil
}

func MergeInboxSource(page InboxPage, source InboxSource) InboxPage {
	sources := append([]InboxSource(nil), page.Sources...)
	found := false
	for i := range sources {
		if sources[i].ProfileID == source.ProfileID {
			sources[i] = source
			found = true
			break
		}
	}
	if !found {
		sources = append(sources, source)
	}
	return inboxPageFromSources(sources)
}

func inboxPageFromSources(sources []InboxSource) InboxPage {
	page := InboxPage{Sources: sources}
	for _, source := range page.Sources {
		if source.Error != "" {
			continue
		}
		for _, row := range source.Page.Threads {
			// Root/fork tasks are deliberate; children are inspected through the
			// established Developer navigation, not silently expanded here.
			if row.Lineage.Type() == "subagent" || row.ContextOnly {
				continue
			}
			// Source results are immutable while other hosts finish. The UI may
			// apply an exact receipt to its own row; never share that mutable
			// optional projection with an in-flight reader's source page.
			if row.Inbox != nil {
				metadata := *row.Inbox
				metadata.PullRequests = append([]wire.PullRequestRef(nil), metadata.PullRequests...)
				if metadata.LatestMessage != nil {
					message := *metadata.LatestMessage
					metadata.LatestMessage = &message
				}
				row.Inbox = &metadata
			}
			target := InboxTarget{ProfileID: source.ProfileID, EnvironmentID: source.EnvironmentID, ProjectID: row.ProjectID, ThreadID: row.ID}
			if row.Inbox != nil {
				target.Revision = row.Inbox.NativeRevision
			}
			page.Rows = append(page.Rows, InboxThread{Target: target, Environment: source.Label, Thread: row})
		}
	}
	sort.SliceStable(page.Rows, func(i, j int) bool {
		left, right := page.Rows[i], page.Rows[j]
		if left.Thread.UpdatedAt != right.Thread.UpdatedAt {
			return left.Thread.UpdatedAt > right.Thread.UpdatedAt
		}
		return left.Target.Key() < right.Target.Key()
	})
	return page
}

func (a *Session) inboxMetadata(parent context.Context, env config.Environment, query string, cursor int) (ThreadPage, bool, error) {
	return a.inboxMetadataRead(parent, env, query, cursor, false)
}

func (a *Session) inboxMetadataRead(parent context.Context, env config.Environment, query string, cursor int, serverSearch bool) (ThreadPage, bool, error) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	a.mu.Lock()
	conn := a.conn
	reuse := a.environment != nil && a.environment.ID == env.ID && a.helloVerified && a.ready && conn != nil
	enabled, navigation := a.features["nativeWorkflows"], a.features["threadNavigation"]
	searchEnabled := a.features["serverSearch"]
	a.mu.Unlock()
	if !reuse {
		var err error
		conn, err = wire.Start(ctx, env)
		if err != nil {
			return ThreadPage{}, false, err
		}
		defer func() {
			conn.Close()
			wait, done := context.WithTimeout(context.Background(), 3*time.Second)
			defer done()
			_ = conn.WaitClosed(wait)
		}()
		hello, err := conn.Request(ctx, "hello", nil, "")
		if err != nil {
			return ThreadPage{}, false, err
		}
		if stringValue(hello["environmentId"]) != env.EnvironmentID || numberValue(hello["protocol"]) != 1 || numberValue(hello["orchestrationProtocol"]) != 2 {
			return ThreadPage{}, false, &wire.Fault{Code: "identity_mismatch", Message: "inbox helper identity or protocol mismatch"}
		}
		features, _ := hello["features"].(map[string]any)
		enabled = boolValue(features["nativeWorkflows"])
		navigation = boolValue(features["threadNavigation"])
		searchEnabled = boolValue(features["serverSearch"])
	}
	n := NavigationState{Filter: "all", Expanded: map[string]bool{}, Lifecycle: "all", Cursor: cursor}
	fields := listFields(query, n, navigation)
	if serverSearch {
		if !searchEnabled {
			return ThreadPage{}, enabled, errors.New("native server conversation search is unavailable on this source")
		}
		fields["serverSearch"], fields["limit"] = true, 50
	}
	result, err := conn.Request(ctx, "list", fields, "")
	if err != nil {
		return ThreadPage{}, enabled, err
	}
	page, err := DecodeThreadPage(result)
	return page, enabled, err
}

func (a *Session) InboxOpen(page InboxPage, key string) (*Session, error) {
	for _, row := range page.Rows {
		if row.Target.Key() != key {
			continue
		}
		env, err := a.navigatorEnvironment(row.Target.ProfileID)
		if err != nil {
			return nil, err
		}
		if env.EnvironmentID != row.Target.EnvironmentID {
			return nil, errors.New("inbox environment identity changed")
		}
		return a.Target(row.Target.ProfileID, row.Target.ThreadID)
	}
	return nil, errors.New("thread was not offered in this inbox page")
}

func (a *Session) validateInboxAttachment(target InboxTarget) (*wire.Connection, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.environment == nil || a.environment.ID != target.ProfileID || a.environment.EnvironmentID != target.EnvironmentID || a.thread != target.ThreadID {
		return nil, errors.New("open this exact inbox thread before acting")
	}
	if !a.features["nativeWorkflows"] {
		return nil, errors.New("this host does not advertise native inbox/review workflows")
	}
	if a.conn == nil || !a.helloVerified || !a.ready {
		return nil, errors.New("inbox thread is reconnecting; refresh before acting")
	}
	s := a.store.Snapshot()
	if s == nil || s.Metadata.ProjectID == nil || *s.Metadata.ProjectID != target.ProjectID || s.Metadata.ThreadID != target.ThreadID {
		return nil, errors.New("inbox project/thread binding changed")
	}
	return a.conn, nil
}

type InboxActionReceipt struct {
	ThreadID, Action, NativeRevision string
	Sequence                         int64
}

// AgentBoxAction uses the ordinary durable operation lane. Formal approval
// answers are deliberately absent: they must go through ReviewRequests.
func (a *Session) AgentBoxAction(ctx context.Context, target InboxTarget, action, until string) (InboxActionReceipt, error) {
	if _, err := a.validateInboxAttachment(target); err != nil {
		return InboxActionReceipt{}, err
	}
	if target.Revision == "" {
		return InboxActionReceipt{}, errors.New("native inbox revision is unavailable; refresh before acting")
	}
	switch action {
	case "settle", "unsettle", "snooze", "unsnooze", "pin", "unpin":
	default:
		return InboxActionReceipt{}, errors.New("inbox action was not offered")
	}
	if action == "settle" {
		s := a.snapshot()
		if s == nil || len(s.Metadata.Requests) > 0 || s.Metadata.ActiveRunID != nil {
			return InboxActionReceipt{}, errors.New("review requires finished work with no pending formal request")
		}
	}
	fields := map[string]any{"threadId": target.ThreadID, "action": action, "expectedRevision": target.Revision}
	if action == "snooze" {
		if _, err := time.Parse(time.RFC3339Nano, until); err != nil {
			return InboxActionReceipt{}, errors.New("Later needs an exact timestamp")
		}
		fields["until"] = until
	}
	reply, err := a.perform(ctx, "thread-action", fields, "", false)
	if err != nil {
		return InboxActionReceipt{}, err
	}
	receipt := InboxActionReceipt{ThreadID: stringValue(reply["threadId"]), Action: stringValue(reply["action"]), NativeRevision: stringValue(reply["nativeRevision"]), Sequence: numberValue(reply["sequence"])}
	if receipt.ThreadID != target.ThreadID || receipt.Action != action {
		return InboxActionReceipt{}, errors.New("native inbox acknowledgement does not match the requested action")
	}
	return receipt, nil
}

// ThreadReview reads actual T3 working tree and branch changes. It never reads arbitrary
// host paths, invokes a provider, or treats assistant text as a Git result.
func (a *Session) ThreadReview(ctx context.Context, target InboxTarget) (wire.ThreadReview, error) {
	conn, err := a.validateInboxAttachment(target)
	if err != nil {
		return wire.ThreadReview{}, err
	}
	fields := map[string]any{"threadId": target.ThreadID}
	if target.Revision != "" {
		fields["revision"] = target.Revision
	}
	result, err := conn.Request(ctx, "thread-review", fields, "")
	if err != nil {
		return wire.ThreadReview{}, err
	}
	review, err := wire.DecodeThreadReview(result)
	if err != nil {
		return review, err
	}
	if review.ThreadID != target.ThreadID || review.ProjectID != target.ProjectID {
		return review, errors.New("native review belongs to another source")
	}
	if target.Revision != "" && review.Revision != target.Revision {
		return review, errors.New("native review changed; refresh the inbox before reviewing")
	}
	if _, err = a.validateInboxAttachment(target); err != nil {
		return review, err
	}
	return review, nil
}

func (a *Session) InboxPreferences() (config.InboxPreferences, error) {
	if a == nil || a.manager == nil || a.manager.State == nil {
		return config.InboxPreferences{}, errors.New("client presentation storage unavailable")
	}
	return a.manager.State.InboxPreferences()
}
func (a *Session) SaveInboxPreferences(expected uint64, next config.InboxPreferences) (config.InboxPreferences, error) {
	if a == nil || a.manager == nil || a.manager.State == nil {
		return config.InboxPreferences{}, errors.New("client presentation storage unavailable")
	}
	return a.manager.State.SaveInboxPreferences(expected, next)
}

// InboxBucket describes native lifecycle only. A missing optional projection
// stays inspectable as Unknown; it is never an empty-success inbox.
func InboxBucket(row InboxThread, now time.Time) string {
	v := row.Thread.Inbox
	if v == nil {
		return "unknown"
	}
	if v.PendingRequestCount > 0 || v.PendingChildRequest {
		return "needs"
	}
	status := strings.ToLower(row.Thread.Status)
	if v.Working != nil && *v.Working {
		return "working"
	}
	if v.Working == nil {
		// Older adapters omit derived membership. Retain their explicit run
		// state fallback without overriding a newer native false projection.
		switch status {
		case "preparing", "starting", "running", "active", "responding", "waiting":
			return "working"
		}
	}
	if status == "queued" {
		return "queued"
	}
	if v.QueuedCount > 0 {
		return "queued"
	}
	if v.SnoozedUntil != "" {
		if until, err := time.Parse(time.RFC3339Nano, v.SnoozedUntil); err == nil && until.After(now) {
			return "later"
		}
	}
	if v.Settled {
		return "done"
	}
	return "needs"
}

// InboxRank compares presentation priority only. It never drives dispatch or
// interrupts a run. Equal ranks preserve updated order with an exact ID tie.
func InboxRank(rows []InboxThread, prefs config.InboxPreferences) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		pa, pb := InboxPriority(prefs, a.Target), InboxPriority(prefs, b.Target)
		if (pa == 3) != (pb == 3) {
			return pa == 3
		}
		ra := prefs.Projects[config.InboxIdentity(a.Target.EnvironmentID, a.Target.ProjectID, "")]
		rb := prefs.Projects[config.InboxIdentity(b.Target.EnvironmentID, b.Target.ProjectID, "")]
		if ra != rb {
			if ra == 0 {
				return false
			}
			if rb == 0 {
				return true
			}
			return ra < rb
		}
		if pa != pb {
			return pa > pb
		}
		if a.Thread.UpdatedAt != b.Thread.UpdatedAt {
			return a.Thread.UpdatedAt > b.Thread.UpdatedAt
		}
		return a.Target.Key() < b.Target.Key()
	})
}

func InboxPriority(prefs config.InboxPreferences, target InboxTarget) int {
	if priority, exists := prefs.Tasks[target.Key()]; exists {
		return priority
	}
	return 1
}

func InboxSummary(row InboxThread) string {
	if row.Thread.Inbox == nil {
		return "Native inbox metadata is unavailable; open the conversation for exact state."
	}
	v := row.Thread.Inbox
	if v.PendingRequestCount > 0 {
		return "A formal request needs review in this conversation."
	}
	if v.LatestMessage != nil && v.LatestMessage.Role == "assistant" && strings.TrimSpace(v.LatestMessage.Text) != "" {
		return v.LatestMessage.Text
	}
	return "No assistant outcome is reported in this metadata page. Open the conversation to inspect its history."
}
