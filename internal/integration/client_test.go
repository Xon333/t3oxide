package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
)

type appStream struct {
	agent  *client.Session
	events chan client.Event
	errors chan error
	done   chan struct{}
	cancel context.CancelFunc
}

func newAppManager(t *testing.T, f *fixture) (*client.Manager, *config.State) {
	t.Helper()
	state, err := config.OpenState(filepath.Join(t.TempDir(), "go-drafts.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := state.Close(); err != nil {
			t.Error(err)
		}
	})
	m := client.NewManager([]config.Environment{f.profile}, state)
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
	return m, state
}

func streamSession(t *testing.T, a *client.Session) *appStream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	s := &appStream{agent: a, events: make(chan client.Event, 128), errors: make(chan error, 1), done: make(chan struct{}), cancel: cancel}
	go func() {
		defer close(s.done)
		defer close(s.events)
		for event, err := range a.Events(ctx) {
			if err != nil {
				select {
				case s.errors <- err:
				default:
				}
				continue
			}
			select {
			case s.events <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	t.Cleanup(func() { s.stop(t) })
	return s
}

func (s *appStream) stop(t *testing.T) {
	t.Helper()
	s.cancel()
	s.agent.Close()
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		t.Error("Session Events did not finish after local detach")
	}
}

func (s *appStream) until(t *testing.T, test func(client.Event) bool) client.Event {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case event, ok := <-s.events:
			if !ok {
				t.Fatal("Session Events ended before expected transcript")
			}
			if test(event) {
				return event
			}
		case err := <-s.errors:
			t.Fatal(err)
		case <-deadline.C:
			t.Fatalf("timed out waiting for actual app transcript: %#v", s.agent.Status())
		}
	}
}

func containsMessage(event client.Event, key, text string) bool {
	if event.Snapshot == nil {
		return false
	}
	for _, msg := range event.Snapshot.Items {
		if msg.Key == key && strings.Contains(msg.Text, text) {
			return true
		}
	}
	return false
}

func awaitStatus(t *testing.T, a *client.Session, predicate func(client.Status) bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if predicate(a.Status()) {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("unexpected app status: %#v", a.Status())
}

func TestClientSessionDraftIsolationSessionDetachAndAuthoritativeRendering(t *testing.T) {
	f := newFixture(t)
	m, state := newAppManager(t, f)
	a, err := m.NewSession("fixture", "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	stream := streamSession(t, a)
	first := stream.until(t, func(e client.Event) bool {
		return containsMessage(e, "thread-a/first-thread-a", "Initial **Markdown**")
	})
	if first.Snapshot.Items[0].Kind != "assistant_message" {
		t.Fatal("assistant kind did not reach the terminal client")
	}
	if err := a.SetDraft("Alpha unsent draft"); err != nil {
		t.Fatal(err)
	}
	f.update("thread-a", "Authoritative replacement", 0)
	replacement := stream.until(t, func(e client.Event) bool {
		return containsMessage(e, "thread-a/first-thread-a", "Authoritative replacement")
	})
	if len(replacement.Snapshot.Items) != 1 || replacement.Revision <= first.Revision {
		t.Fatal("updated rows appended or revision did not advance")
	}
	target, err := a.Target("fixture", "thread-b")
	if err != nil {
		t.Fatal(err)
	}
	stream.stop(t)
	if got := state.Draft("env-a", "thread-a"); got != "Alpha unsent draft" {
		t.Fatalf("detach lost Alpha draft: %q", got)
	}
	b := target
	if b.Draft() != "" {
		t.Fatal("Alpha draft leaked into Beta")
	}
	beta := streamSession(t, b)
	beta.until(t, func(e client.Event) bool {
		return containsMessage(e, "thread-b/first-thread-b", "Initial **Markdown**")
	})
	if err := b.SetDraft("Beta draft"); err != nil {
		t.Fatal(err)
	}
	f.update("thread-a", "Old session update", 0)
	f.update("thread-b", "Beta update", 0)
	view := beta.until(t, func(e client.Event) bool { return containsMessage(e, "thread-b/first-thread-b", "Beta update") })
	for _, msg := range view.Snapshot.Items {
		if strings.Contains(msg.Text, "Old session") {
			t.Fatal("detached session update leaked into new conversation")
		}
	}
	beta.stop(t)
	back, err := m.NewSession("fixture", "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	if back.Draft() != "Alpha unsent draft" || state.Draft("env-a", "thread-b") != "Beta draft" {
		t.Fatal("per-thread draft recovery failed")
	}
	f.mu.Lock()
	sockets := len(f.sockets)
	f.mu.Unlock()
	if sockets != 0 {
		t.Fatalf("detached sessions left %d upstream sockets", sockets)
	}
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("session detach caused remote mutation")
	}
}

func TestClientEnvironmentSwitchIsolatesIdenticalThreadIDsAndDrafts(t *testing.T) {
	first := newFixture(t)
	second := newEnvironmentFixture(t, "env-b", "second", "Second environment")
	state, err := config.OpenState(filepath.Join(t.TempDir(), "drafts.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := state.Close(); err != nil {
			t.Error(err)
		}
	})
	m := client.NewManager([]config.Environment{first.profile, second.profile}, state)
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
	a, err := m.NewSession("fixture", "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	alpha := streamSession(t, a)
	alpha.until(t, func(e client.Event) bool { return containsMessage(e, "thread-a/first-thread-a", "Initial") })
	if err := a.SetDraft("First environment draft"); err != nil {
		t.Fatal(err)
	}
	b, err := a.Target("second", "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	alpha.stop(t)
	if b.Draft() != "" {
		t.Fatal("draft crossed environment identity despite identical thread ID")
	}
	beta := streamSession(t, b)
	beta.until(t, func(e client.Event) bool {
		return e.Status.EnvironmentID == "env-b" && containsMessage(e, "thread-a/first-thread-a", "Initial")
	})
	if err := b.SetDraft("Second environment draft"); err != nil {
		t.Fatal(err)
	}
	first.update("thread-a", "Retired environment data", 0)
	second.update("thread-a", "Current environment data", 0)
	event := beta.until(t, func(e client.Event) bool {
		return containsMessage(e, "thread-a/first-thread-a", "Current environment data")
	})
	if event.Status.EnvironmentID != "env-b" || event.Status.ProfileID != "second" {
		t.Fatalf("environment context mismatch: %#v", event.Status)
	}
	for _, item := range event.Snapshot.Items {
		if strings.Contains(item.Text, "Retired environment") {
			t.Fatal("retired environment update reached current view")
		}
	}
	beta.stop(t)
	if state.Draft("env-a", "thread-a") != "First environment draft" || state.Draft("env-b", "thread-a") != "Second environment draft" {
		t.Fatal("environment draft persistence was not isolated")
	}
	for _, f := range []*fixture{first, second} {
		if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
			t.Fatal("environment switch mutated remote work")
		}
	}
}

func TestClientSessionPersistsUncertainSendAndIndependentStopUntilExplicitRetry(t *testing.T) {
	f := newFixture(t)
	m, state := newAppManager(t, f)
	a, err := m.NewSession("fixture", "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	stream := streamSession(t, a)
	stream.until(t, func(e client.Event) bool { return containsMessage(e, "thread-a/first-thread-a", "Initial") })
	awaitStatus(t, a, func(status client.Status) bool { return strings.Contains(status.State, "running") })
	if err := a.SetDraft("Original user draft"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.dropMessages = true
	f.mu.Unlock()
	send := make(chan error, 1)
	go func() { send <- a.Send(context.Background(), "Original user draft") }()
	id := f.waitMessage(t, "Original user draft")
	if pending := state.Pending("env-a", false); pending == nil || pending["id"] != id || pending["message"] != "Original user draft" {
		t.Fatal("operation was dispatched before durable local identity")
	}
	if err := a.SetDraft("New text typed while acknowledgement is pending"); err != nil {
		t.Fatal(err)
	}
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	if err := a.Stop(stopCtx); err != nil {
		t.Fatalf("app Stop blocked behind pending send: %v", err)
	}
	stopCancel()
	f.closeSockets()
	select {
	case err := <-send:
		if err == nil {
			t.Fatal("lost send ACK was reported successful")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lost send ACK did not settle")
	}
	if state.Pending("env-a", false) == nil {
		t.Fatal("uncertain action recovery record was cleared")
	}
	stream.until(t, func(e client.Event) bool {
		return containsMessage(e, "thread-a/"+id+"-reply", "Response after Original user draft")
	})
	awaitStatus(t, a, func(status client.Status) bool { return strings.Contains(status.State, "interrupted") })
	if effects, dispatches := f.counts(); effects != 2 || dispatches != 2 {
		t.Fatalf("app reconnect automatically retried a write: %d/%d", effects, dispatches)
	}
	f.mu.Lock()
	f.dropMessages = false
	f.mu.Unlock()
	retryCtx, retryCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer retryCancel()
	if _, err := a.Command(retryCtx, "retry", ""); err != nil {
		t.Fatalf("explicit same-ID retry: %v", err)
	}
	if pending := state.Pending("env-a", false); pending != nil {
		t.Fatalf("acknowledged retry did not clear local recovery: %#v", pending)
	}
	if a.Draft() != "New text typed while acknowledgement is pending" {
		t.Fatal("old successful send erased a newer draft")
	}
	stream.stop(t)
	if state.Draft("env-a", "thread-a") != a.Draft() {
		t.Fatal("newer draft was not durable after detach")
	}
	if effects, dispatches := f.counts(); effects != 2 || dispatches != 3 {
		t.Fatalf("explicit retry duplicated a write: %d/%d", effects, dispatches)
	}
}

func TestClientSessionEarlierAndLatestUseReplacementPages(t *testing.T) {
	f := newFixture(t)
	m, _ := newAppManager(t, f)
	a, err := m.NewSession("fixture", "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	stream := streamSession(t, a)
	stream.until(t, func(e client.Event) bool { return containsMessage(e, "thread-a/first-thread-a", "Initial") })
	awaitStatus(t, a, func(status client.Status) bool { return strings.Contains(status.State, "running") })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.Navigate(ctx, true); err != nil {
		t.Fatal(err)
	}
	page := stream.until(t, func(e client.Event) bool { return containsMessage(e, "thread-a/old-thread-a", "Earlier page") })
	if !page.ResetScroll || page.Follow {
		t.Fatal("Earlier did not request top-of-page scroll without live following")
	}
	if len(page.Snapshot.Items) != 1 {
		t.Fatal("Earlier appended to Latest instead of replacing it")
	}
	if err := a.Send(context.Background(), "must be refused in history"); err == nil {
		t.Fatal("history accepted a prompt")
	}
	f.update("thread-a", "Current text after earlier page", 0)
	if err := a.Navigate(ctx, false); err != nil {
		t.Fatal(err)
	}
	latest := stream.until(t, func(e client.Event) bool {
		return containsMessage(e, "thread-a/first-thread-a", "Current text after earlier page")
	})
	if !latest.ResetScroll || !latest.Follow {
		t.Fatal("Latest did not resume live following")
	}
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("history UI triggered a write")
	}
}

func TestClientSessionStopFromHistoryKeepsReviewedRunIdentity(t *testing.T) {
	for _, changed := range []bool{false, true} {
		name := "same-run"
		if changed {
			name = "newer-run-rejected"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			m, _ := newAppManager(t, f)
			a, err := m.NewSession("fixture", "thread-a")
			if err != nil {
				t.Fatal(err)
			}
			stream := streamSession(t, a)
			stream.until(t, func(e client.Event) bool { return containsMessage(e, "thread-a/first-thread-a", "Initial") })
			awaitStatus(t, a, func(s client.Status) bool { return strings.Contains(s.State, "running") })
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := a.Navigate(ctx, true); err != nil {
				t.Fatal(err)
			}
			stream.until(t, func(e client.Event) bool { return containsMessage(e, "thread-a/old-thread-a", "Earlier page") })
			if changed {
				f.mu.Lock()
				f.threads["thread-a"]["runs"] = []any{object{"id": "later-run", "status": "running"}}
				f.mu.Unlock()
			}
			err = a.Stop(ctx)
			if changed {
				if err == nil {
					t.Fatal("Stop silently retargeted to a later run")
				}
				if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
					t.Fatal("stale historical Stop affected remote state")
				}
			} else {
				if err != nil {
					t.Fatalf("Stop lost the previously reviewed run while reading Earlier: %v", err)
				}
				f.mu.Lock()
				defer f.mu.Unlock()
				if len(f.commands) != 1 {
					t.Fatalf("Stop commands: %#v", f.commands)
				}
				for _, command := range f.commands {
					if command["runId"] != "run-thread-a" || command["holdQueue"] != true {
						t.Fatalf("historical Stop changed target: %#v", command)
					}
				}
			}
		})
	}
}

func TestClientSessionReadOnlyHostIsVisibleAndCannotWrite(t *testing.T) {
	f := newFixture(t)
	data, err := os.ReadFile(f.profile.HostConfig)
	if err != nil {
		t.Fatal(err)
	}
	var host object
	if err = json.Unmarshal(data, &host); err != nil {
		t.Fatal(err)
	}
	host["allowWrites"] = false
	data, _ = json.Marshal(host)
	if err = os.WriteFile(f.profile.HostConfig, data, 0600); err != nil {
		t.Fatal(err)
	}
	m, _ := newAppManager(t, f)
	a, err := m.NewSession("fixture", "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	stream := streamSession(t, a)
	stream.until(t, func(e client.Event) bool { return containsMessage(e, "thread-a/first-thread-a", "Initial") })
	awaitStatus(t, a, func(s client.Status) bool { return strings.Contains(s.State, "read-only") })
	if err = a.Send(context.Background(), "Must not write"); err == nil {
		t.Fatal("read-only host accepted a prompt")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err = a.Stop(ctx); err == nil {
		t.Fatal("read-only host accepted Stop")
	}
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("read-only UI caused a remote mutation")
	}
}
