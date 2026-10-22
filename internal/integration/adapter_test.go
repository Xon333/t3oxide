package integration_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Xon333/t3oxide/internal/wire"
)

func TestActualAdapterViewsHistoryAndThreadSwitch(t *testing.T) {
	f := newFixture(t)
	c := f.start(t)
	hello := request(t, c, "hello", nil, "")
	if hello["environmentId"] != "env-a" || hello["writesEnabled"] != true {
		t.Fatalf("unexpected hello: %#v", hello)
	}
	list := request(t, c, "list", object{"query": "Alpha"}, "")
	threads := list["threads"].([]any)
	if len(threads) != 1 || threads[0].(object)["id"] != "thread-a" {
		t.Fatalf("filter not honored by actual adapter: %#v", list)
	}
	var store wire.Store
	store.Select("thread-a")
	request(t, c, "watch", object{"threadId": "thread-a"}, "")
	initial := untilView(t, c, &store, func(s *wire.Snapshot) bool { return s.Metadata.Live })
	if len(initial.Items) != 1 || !strings.Contains(initial.Items[0].Text, "**Markdown**") {
		t.Fatalf("lost initial content: %#v", initial)
	}
	f.update("thread-a", "Streaming replacement, not append", 0)
	updated := untilView(t, c, &store, func(s *wire.Snapshot) bool {
		return len(s.Items) == 1 && s.Items[0].Text == "Streaming replacement, not append"
	})
	if updated.Revision <= initial.Revision {
		t.Fatal("revision did not advance")
	}
	// The adapter must reject replayed upstream sequence numbers before emitting
	// a new keyed companion revision. A subsequent valid update proves the loop
	// still runs and keeps this assertion independent of arbitrary sleeps.
	f.update("thread-a", "STALE CONTENT", 1)
	f.update("thread-a", "Fresh content after stale event", 0)
	untilView(t, c, &store, func(s *wire.Snapshot) bool {
		return len(s.Items) == 1 && s.Items[0].Text == "Fresh content after stale event"
	})
	if updated.Metadata.HistoryCursor == nil {
		t.Fatal("expected opaque history cursor")
	}
	request(t, c, "history", object{"cursor": *updated.Metadata.HistoryCursor}, "")
	historical := untilView(t, c, &store, func(s *wire.Snapshot) bool { return s.Metadata.Historical && s.Metadata.State == "historical" })
	if historical.Metadata.Live || historical.Metadata.ActiveRunID != nil || len(historical.Metadata.Requests) != 0 || len(historical.Items) != 1 || historical.Items[0].ItemID != "old-thread-a" {
		t.Fatalf("Earlier must replace the page and disable live controls: %#v", historical)
	}
	f.update("thread-a", "Changed while reading history", 0)
	request(t, c, "watch", object{"threadId": "thread-a"}, "")
	latest := untilView(t, c, &store, func(s *wire.Snapshot) bool {
		return s.Metadata.Live && !s.Metadata.Historical && len(s.Items) == 1 && s.Items[0].Text == "Changed while reading history"
	})
	if latest.Metadata.ActiveRunID == nil {
		t.Fatal("Latest did not restore current controls")
	}
	store.Select("thread-b")
	request(t, c, "watch", object{"threadId": "thread-b"}, "")
	untilView(t, c, &store, func(s *wire.Snapshot) bool { return s.Metadata.Live && s.Metadata.ThreadID == "thread-b" })
	f.update("thread-a", "Must never leak into Beta", 0)
	f.update("thread-b", "Beta current", 0)
	beta := untilView(t, c, &store, func(s *wire.Snapshot) bool { return len(s.Items) == 1 && s.Items[0].Text == "Beta current" })
	if beta.Items[0].SourceThreadID != "thread-b" {
		t.Fatal("old thread contaminated new view")
	}
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatalf("navigation caused mutation: %d/%d", effects, dispatches)
	}
}

func TestActualAdapterStopPriorityAndExplicitSendRecovery(t *testing.T) {
	f := newFixture(t)
	c := f.start(t)
	request(t, c, "hello", nil, "")
	f.mu.Lock()
	f.dropAck["held-send"] = true
	f.mu.Unlock()
	sendFields := object{"threadId": "thread-a", "message": "User text", "mode": "queue"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sendResult := make(chan error, 1)
	go func() { _, err := c.Request(ctx, "send", sendFields, "held-send"); sendResult <- err }()
	f.waitCommand(t, "held-send")
	// This must reach the independent Stop lane while the ordinary send's
	// acknowledgement is withheld. A serialized client would time out here.
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	stopFields := object{"threadId": "thread-a", "runId": "run-thread-a"}
	if _, err := c.Request(stopCtx, "stop", stopFields, "priority-stop"); err != nil {
		t.Fatalf("Stop was blocked by send ACK: %v", err)
	}
	f.mu.Lock()
	if f.commands["held-send"]["dispatchMode"].(object)["type"] != "queue_after_active" {
		t.Error("queue intent changed")
	}
	if _, exists := f.commands["held-send"]["deliveryIntent"]; exists {
		t.Error("queue incorrectly mapped to steer/auto")
	}
	f.mu.Unlock()
	c.Close()
	if err := <-sendResult; err == nil || !wire.IsUncertain(err) {
		t.Fatalf("lost send ACK must remain uncertain: %v", err)
	}
	if effects, dispatches := f.counts(); effects != 2 || dispatches != 2 {
		t.Fatalf("unexpected automatic resend: %d/%d", effects, dispatches)
	}
	f.mu.Lock()
	f.dropAck["held-send"] = false
	f.mu.Unlock()
	c2 := f.start(t)
	// A new helper recovers the prepared journal record and T3's receipt with
	// the same command. No later run is derived from the latest projection.
	request(t, c2, "send", sendFields, "held-send")
	request(t, c2, "stop", stopFields, "priority-stop")
	if effects, dispatches := f.counts(); effects != 2 || dispatches != 3 {
		t.Fatalf("retry duplicated an effect or ignored journal receipt: %d/%d", effects, dispatches)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel2()
	_, err := c2.Request(ctx2, "send", object{"threadId": "thread-a", "message": "Changed text", "mode": "queue"}, "held-send")
	var fault *wire.Fault
	if !errors.As(err, &fault) || fault.Code != "invalid_request" {
		t.Fatalf("changed same-ID payload was not rejected: %v", err)
	}
}

func TestActualAdapterInterruptedStopAcknowledgementDoesNotStopLaterRun(t *testing.T) {
	f := newFixture(t)
	c := f.start(t)
	f.mu.Lock()
	f.dropAck["uncertain-stop"] = true
	f.mu.Unlock()
	fields := object{"threadId": "thread-a", "runId": "run-thread-a"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := c.Request(ctx, "stop", fields, "uncertain-stop"); result <- err }()
	f.waitCommand(t, "uncertain-stop")
	c.Close()
	if err := <-result; err == nil || !wire.IsUncertain(err) {
		t.Fatalf("interrupted Stop ACK lost uncertainty: %v", err)
	}
	f.mu.Lock()
	f.dropAck["uncertain-stop"] = false
	f.threads["thread-a"]["runs"] = []any{object{"id": "later-run", "status": "running"}}
	f.mu.Unlock()
	c2 := f.start(t)
	request(t, c2, "stop", fields, "uncertain-stop")
	c2.Close()
	c3 := f.start(t)
	request(t, c3, "stop", fields, "uncertain-stop")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.effects != 1 || len(f.dispatches) != 2 {
		t.Fatalf("expected one Stop effect and one durable receipt recovery: %d/%d", f.effects, len(f.dispatches))
	}
	if f.threads["thread-a"]["runs"].([]any)[0].(object)["status"] != "running" {
		t.Fatal("explicit retry stopped the later run")
	}
	if f.commands["uncertain-stop"]["runId"] != "run-thread-a" || f.commands["uncertain-stop"]["holdQueue"] != true {
		t.Fatal("Stop changed its reviewed run or queue hold")
	}
}

func (f *fixture) addRequests() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.threads["thread-a"]["turnItems"] = []any{
		object{"id": "approval-item", "threadId": "thread-a", "type": "approval_request", "requestId": "approval-a", "requestKind": "command",
			"prompt": "Approve the fixture?", "options": []any{object{"decision": "accept", "label": "Once"}, object{"decision": "decline", "label": "Decline"}}},
		object{"id": "question-item", "threadId": "thread-a", "type": "user_input_request", "requestId": "question-a",
			"prompt": "Choose fixture settings", "questions": []any{object{"id": "choice", "header": "Choice", "required": true}, object{"id": "notes", "header": "Notes", "required": false}}},
	}
	f.threads["thread-a"]["runtimeRequests"] = []any{
		object{"id": "approval-a", "status": "pending", "responseCapability": object{"type": "live", "providerSessionId": "fixture"}},
		object{"id": "question-a", "status": "pending", "responseCapability": object{"type": "live", "providerSessionId": "fixture"}},
	}
}

func TestActualAdapterExactApprovalsQuestionsLaunchAndSteer(t *testing.T) {
	f := newFixture(t)
	f.addRequests()
	c := f.start(t)
	var store wire.Store
	store.Select("thread-a")
	request(t, c, "watch", object{"threadId": "thread-a"}, "")
	view := untilView(t, c, &store, func(s *wire.Snapshot) bool { return s.Metadata.Live && len(s.Metadata.Requests) == 2 })
	requests := map[string]object{}
	for _, r := range view.Metadata.Requests {
		requests[r["id"].(string)] = r
	}
	approval := requests["approval-a"]
	question := requests["question-a"]
	if approval["supported"] != true || question["supported"] != true {
		t.Fatalf("actual adapter did not expose requests: %#v", requests)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	_, err := c.Request(ctx, "answer", object{"threadId": "thread-a", "requestId": "approval-a", "revision": "stale", "decision": "accept"}, "stale-answer")
	var fault *wire.Fault
	if !errors.As(err, &fault) || fault.Code != "stale" {
		t.Fatalf("stale request accepted: %v", err)
	}
	request(t, c, "answer", object{"threadId": "thread-a", "requestId": "approval-a", "revision": approval["revision"], "decision": "accept"}, "approval-answer")
	answers := object{"choice": "Continue", "notes": "human answer"}
	request(t, c, "answer", object{"threadId": "thread-a", "requestId": "question-a", "revision": question["revision"], "answers": answers}, "question-answer")
	request(t, c, "send", object{"threadId": "thread-a", "message": "Adjust the running task", "mode": "steer"}, "steer-message")
	launched := request(t, c, "launch", object{"threadId": "thread-a", "title": "Disposable new thread", "message": "A new task"}, "launch-id")
	if launched["threadId"] != "launch-id" {
		t.Fatalf("launch receipt lost thread identity: %#v", launched)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.effects != 4 {
		t.Fatalf("unexpected write count %d", f.effects)
	}
	if f.commands["approval-answer"]["requestId"] != "approval-a" || f.commands["approval-answer"]["decision"] != "accept" {
		t.Fatal("generic permission substituted for exact approval")
	}
	if f.commands["question-answer"]["requestId"] != "question-a" || !reflect.DeepEqual(f.commands["question-answer"]["answers"], answers) {
		t.Fatal("question answers lost exact request/answer mapping")
	}
	if f.commands["steer-message"]["deliveryIntent"] != "steer" {
		t.Fatal("steer changed into queued next turn")
	}
	launch := f.commands["launch-id"]
	if launch["projectId"] != "project-a" || launch["workspaceStrategy"].(object)["type"] != "existing_worktree" || launch["workspaceStrategy"].(object)["worktreePath"] != "/fixture/existing-worktree" {
		t.Fatalf("launch did not preserve existing workspace: %#v", launch)
	}
	if launch["modelSelection"].(object)["model"] != "fixture-model" || launch["runtimeMode"] != "approval-required" {
		t.Fatal("launch lost source model/mode")
	}
}

func TestActualAdapterReconnectAfterMalformedUpstreamAndHistoryDisconnect(t *testing.T) {
	f := newFixture(t)
	c := f.start(t)
	var store wire.Store
	store.Select("thread-a")
	request(t, c, "watch", object{"threadId": "thread-a"}, "")
	before := untilView(t, c, &store, func(s *wire.Snapshot) bool { return s.Metadata.Live })
	f.corruptSockets()
	untilView(t, c, &store, func(s *wire.Snapshot) bool { return s.Metadata.State == "unavailable" })
	after := untilView(t, c, &store, func(s *wire.Snapshot) bool { return s.Metadata.Live && s.Revision > before.Revision })
	request(t, c, "history", object{"cursor": *after.Metadata.HistoryCursor}, "")
	history := untilView(t, c, &store, func(s *wire.Snapshot) bool { return s.Metadata.Historical && s.Metadata.State == "historical" })
	f.closeSockets()
	untilView(t, c, &store, func(s *wire.Snapshot) bool { return s.Metadata.State == "unavailable" && s.Metadata.Historical })
	reconnected := untilView(t, c, &store, func(s *wire.Snapshot) bool {
		return s.Metadata.Historical && s.Metadata.State == "historical" && s.Revision > history.Revision
	})
	if reconnected.Metadata.Live || len(reconnected.Items) != 1 || reconnected.Items[0].ItemID != "old-thread-a" {
		t.Fatal("historical reconnect silently jumped to Latest")
	}
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("reconnect caused an automatic write")
	}
}

func TestActualAdapterAnswerRecoveryRetainsRequestIdentity(t *testing.T) {
	for _, kind := range []string{"approval", "question"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			f.addRequests()
			c := f.start(t)
			var store wire.Store
			store.Select("thread-a")
			request(t, c, "watch", object{"threadId": "thread-a"}, "")
			view := untilView(t, c, &store, func(s *wire.Snapshot) bool { return s.Metadata.Live && len(s.Metadata.Requests) == 2 })
			requestID := kind + "-a"
			var revision any
			for _, r := range view.Metadata.Requests {
				if r["id"] == requestID {
					revision = r["revision"]
				}
			}
			fields := object{"threadId": "thread-a", "requestId": requestID, "revision": revision}
			if kind == "approval" {
				fields["decision"] = "accept"
			} else {
				fields["answers"] = object{"choice": "Continue"}
			}
			id := "lost-ack-" + kind
			f.mu.Lock()
			f.dropAck[id] = true
			f.mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result := make(chan error, 1)
			go func() { _, err := c.Request(ctx, "answer", fields, id); result <- err }()
			f.waitCommand(t, id)
			c.Close()
			if err := <-result; err == nil || !wire.IsUncertain(err) {
				t.Fatalf("lost answer ACK must remain uncertain: %v", err)
			}
			f.mu.Lock()
			f.dropAck[id] = false
			f.threads["thread-a"]["runtimeRequests"] = []any{object{"id": "later-request", "status": "pending", "responseCapability": object{"type": "live"}}}
			f.mu.Unlock()
			c2 := f.start(t)
			request(t, c2, "answer", fields, id)
			c2.Close()
			c3 := f.start(t)
			request(t, c3, "answer", fields, id)
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.effects != 1 || len(f.dispatches) != 2 {
				t.Fatalf("answer recovery duplicated effects: %d/%d", f.effects, len(f.dispatches))
			}
			if f.commands[id]["requestId"] != requestID {
				t.Fatal("retry changed original request identity")
			}
			if f.threads["thread-a"]["runtimeRequests"].([]any)[0].(object)["status"] != "pending" {
				t.Fatal("retry answered a later request")
			}
		})
	}
}

func TestActualAdapterRejectsWrongEnvironmentIdentity(t *testing.T) {
	f := newFixture(t)
	wrong := f.profile
	wrong.EnvironmentID = "different-environment"
	c, err := wire.Start(context.Background(), wrong)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	_, err = c.Request(ctx, "hello", nil, "")
	if err == nil {
		t.Fatal("client accepted a helper belonging to another environment")
	}
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("identity mismatch permitted a command")
	}
	if err := c.WaitClosed(ctx); err != nil {
		t.Fatalf("identity failure leaked helper: %v", err)
	}
}
