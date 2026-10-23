package integration_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/wire"
)

func historyRequestsPage(t *testing.T, a *client.Session, stream *appStream) client.Event {
	t.Helper()
	if err := a.Navigate(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	e := stream.until(t, func(e client.Event) bool {
		return e.Snapshot != nil && e.Snapshot.Metadata.State == "historical" && containsMessage(e, "thread-a/old-thread-a", "Earlier page")
	})
	if e.Status.Requests != 2 || !e.Status.Historical || e.Follow || !e.ResetScroll || len(e.Snapshot.Metadata.Requests) != 0 {
		t.Fatalf("history lost separate live offers or changed replacement semantics: %+v", e)
	}
	return e
}

func TestHistoryRequestsActualAdapterRetainsOffersAndAnswersAcrossReconnect(t *testing.T) {
	f := newFixture(t)
	f.addRequests()
	_, state, a, stream := startFeatureSession(t, f, "thread-a")
	awaitStatus(t, a, func(s client.Status) bool { return s.Requests == 2 })
	const draft = " \tunsent exact bytes\r\n� "
	if err := a.StoreDraft(draft); err != nil {
		t.Fatal(err)
	}
	historyRequestsPage(t, a, stream)
	before := f.requestCounts()
	featureOK(t, a, "reconnect", "")
	stream.until(t, func(e client.Event) bool { return e.Status.ConnectionState == "connecting" })
	e := stream.until(t, func(e client.Event) bool { return e.Status.ConnectionState == "connected" && e.Status.Historical })
	if e.Status.Requests != 2 || e.ResetScroll || e.Follow {
		t.Fatal("reconnect removed offers or moved historical reading anchor")
	}
	after := f.requestCounts()
	if before["rpc:orchestration.subscribeThread"] != after["rpc:orchestration.subscribeThread"] || before["/api/orchestration/threads/thread-a/bounded"] != after["/api/orchestration/threads/thread-a/bounded"] {
		t.Fatal("historical reconnect automatically followed/refreshed live detail")
	}
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("history/reconnect mutated requests")
	}
	for _, selection := range []string{"0", "1"} {
		review := featureCommand(a, "requests", "")
		pick := featurePrompt(t, a)
		if pick.Question.Title != "Pending T3 requests" || len(pick.Question.Choices) != 2 || !pick.Respond(client.Answer{ChoiceIDs: []string{selection}}) {
			t.Fatal("retained requests not offered for review")
		}
		p := featurePrompt(t, a)
		if selection == "0" {
			if p.Question.Title != "T3 approval" || p.Question.Choices[0].ID != "accept" {
				t.Fatal("approval offer changed")
			}
			p.Respond(client.Answer{ChoiceIDs: []string{"accept"}})
		} else {
			p.Respond(client.Answer{Text: " \tcanonical answer\r\n� "})
			featurePrompt(t, a).Respond(client.Answer{Text: ""})
		}
		if result := featureReply(t, review); result.err != nil {
			t.Fatal(result.err)
		}
	}
	if a.Draft() != draft || state.Pending("env-a", false) != nil || !a.Status().Historical {
		t.Fatal("request answer changed draft, recovery or history")
	}
	after = f.requestCounts()
	if after["rpc:orchestration.subscribeThread"] != before["rpc:orchestration.subscribeThread"] || after["/api/orchestration/threads/thread-a/history"] != before["/api/orchestration/threads/thread-a/history"] || after["/api/orchestration/threads/thread-a/bounded"] != before["/api/orchestration/threads/thread-a/bounded"]+2 {
		t.Fatal("answer did more than its fresh bounded validation")
	}
	f.mu.Lock()
	commands := clone(f.commands)
	f.mu.Unlock()
	if len(commands) != 2 {
		t.Fatalf("wrong answer count: %#v", commands)
	}
	for _, command := range commands {
		if command["type"] != "runtime-request.respond" || command["threadId"] != "thread-a" {
			t.Fatalf("wrong mutation: %#v", command)
		}
		switch command["requestId"] {
		case "approval-a":
			if command["decision"] != "accept" {
				t.Fatal("approval decision changed")
			}
		case "question-a":
			if !reflect.DeepEqual(command["answers"], object{"choice": " \tcanonical answer\r\n� "}) {
				t.Fatalf("canonical answer changed: %#v", command)
			}
		default:
			t.Fatal("answer retargeted")
		}
	}
	// An answer acknowledgement does not replace a paused page's control
	// snapshot. Explicit Latest obtains authoritative removal of both offers.
	if err := a.Navigate(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	stream.until(t, func(e client.Event) bool {
		return e.Snapshot != nil && e.Snapshot.Metadata.Live && e.Status.Requests == 0
	})
	if a.ReviewRequests(context.Background()) == nil {
		t.Fatal("removed requests remained reviewable")
	}
	if effects, dispatches := f.counts(); effects != 2 || dispatches != 2 {
		t.Fatalf("answers duplicated effects: %d/%d", effects, dispatches)
	}
}

func TestHistoryRequestsActualAdapterRejectsStaleIDAndRevisionWithoutDispatch(t *testing.T) {
	for _, change := range []string{"removed", "revised", "different-id"} {
		t.Run(change, func(t *testing.T) {
			f := newFixture(t)
			f.addRequests()
			_, state, a, stream := startFeatureSession(t, f, "thread-a")
			historyRequestsPage(t, a, stream)
			review := featureCommand(a, "requests", "")
			featurePrompt(t, a).Respond(client.Answer{ChoiceIDs: []string{"0"}})
			approval := featurePrompt(t, a)
			f.mu.Lock()
			p := f.threads["thread-a"]
			switch change {
			case "removed":
				p["runtimeRequests"].([]any)[0].(object)["status"] = "resolved"
			case "revised":
				p["turnItems"].([]any)[0].(object)["prompt"] = "Changed approval requires another review"
			case "different-id":
				p["runtimeRequests"].([]any)[0].(object)["id"] = "replacement"
				p["turnItems"].([]any)[0].(object)["requestId"] = "replacement"
			}
			f.mu.Unlock()
			approval.Respond(client.Answer{ChoiceIDs: []string{"accept"}})
			result := featureReply(t, review)
			var fault *wire.Fault
			if !errors.As(result.err, &fault) || fault.Code != "stale" {
				t.Fatalf("fresh adapter validation did not reject stale offer: %v", result.err)
			}
			if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 || state.Pending("env-a", false) != nil {
				t.Fatal("stale historical offer mutated or retained definitely rejected operation")
			}
			if !a.Status().Historical || a.Status().Requests != 2 {
				t.Fatal("rejection automatically followed live or invented an authoritative replacement")
			}
			if err := a.Navigate(context.Background(), false); err != nil {
				t.Fatal(err)
			}
			want := 2
			if change == "removed" {
				want = 1
			}
			e := stream.until(t, func(e client.Event) bool {
				return e.Snapshot != nil && e.Snapshot.Metadata.Live && e.Status.Requests == want
			})
			if change == "revised" && e.Snapshot.Metadata.Requests[0]["prompt"] != "Changed approval requires another review" || change == "different-id" && e.Snapshot.Metadata.Requests[0]["id"] != "replacement" {
				t.Fatal("Latest did not replace retained offers")
			}
		})
	}
}
