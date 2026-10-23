package client

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Xon333/t3oxide/internal/wire"
)

func historyRequestView(t *testing.T, a *Session, revision int, historical bool, requests ...map[string]any) {
	t.Helper()
	if err := a.applyPacket(wire.Packet{Event: "view", Data: recoveryView(revision, historical, requests)}); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryRequestsRetainVerifiedOffersAcrossPagesAndReconnect(t *testing.T) {
	a, state, path := recoverySession(t, "out={'result':{'protocol':1,'environmentId':'fixture-env','orchestrationProtocol':2,'writesEnabled':True}}")
	historyRequestView(t, a, 2, false, approval("verified"))
	const draft = " \texact draft\r\n� "
	if err := a.StoreDraft(draft); err != nil {
		t.Fatal(err)
	}
	for revision := 3; revision <= 4; revision++ {
		historyRequestView(t, a, revision, true)
		if got := a.Status(); got.Requests != 1 || !got.Historical || !strings.Contains(got.State, "awaiting input (1)") {
			t.Fatalf("history removed verified request: %+v", got)
		}
		if len(a.snapshot().Metadata.Requests) != 0 {
			t.Fatal("retained offer rewrote historical projection")
		}
	}
	a.mu.Lock()
	a.cachedHistory = true
	a.store.Reset()
	a.store.Select("thread")
	a.helloVerified, a.ready, a.writable = false, false, false
	a.mu.Unlock()
	if got := a.Status(); got.Requests != 1 || !got.Historical || got.Writable {
		t.Fatalf("reconnect lost cached offer/history: %+v", got)
	}
	if err := a.prepare(context.Background(), a.conn); err != nil {
		t.Fatal(err)
	}
	a.asker = recoveryAsker(func(_ context.Context, q Question) (Answer, error) {
		if q.Title != "T3 approval" || q.Choices[0].ID != "acceptForSession" {
			t.Fatalf("retained request not reviewable: %+v", q)
		}
		return Answer{Cancelled: true}, nil
	})
	if err := a.ReviewRequests(context.Background()); err != nil {
		t.Fatal(err)
	}
	packets := requestsFromLog(t, path)
	if len(packets) != 1 || packets[0]["op"] != "hello" || a.Draft() != draft || state.Pending("fixture-env", false) != nil {
		t.Fatalf("history/review changed draft, followed live or mutated: %#v", packets)
	}
}

func TestHistoryRequestsNeedVerifiedLiveAuthority(t *testing.T) {
	m, _, env := recoveryManager(t)
	a, _ := m.NewSession(env.ID, "thread")
	historyRequestView(t, a, 1, true, approval("historical-only"))
	if a.Status().Requests != 0 || a.ReviewRequests(context.Background()) == nil {
		t.Fatal("historical offer invented live authority")
	}
	historyRequestView(t, a, 2, false, approval("before-hello"))
	if a.Status().Requests != 0 {
		t.Fatal("unverified connection authorized an offer")
	}
}

func TestHistoryRequestsOnlyLiveRemovalOrRevisionRetiresReview(t *testing.T) {
	for _, revised := range []bool{false, true} {
		t.Run(map[bool]string{false: "removed", true: "revised"}[revised], func(t *testing.T) {
			a, state, path := recoverySession(t, "out={'result':{'sequence':10}}")
			historyRequestView(t, a, 2, false, approval("old"))
			historyRequestView(t, a, 3, true)
			a.asker = recoveryAsker(func(ctx context.Context, _ Question) (Answer, error) {
				historyRequestView(t, a, 4, true)
				if ctx.Err() != nil {
					t.Fatal("another history page retired live request review")
				}
				var current []map[string]any
				if revised {
					current = append(current, approval("new"))
				}
				historyRequestView(t, a, 5, false, current...)
				if !errors.Is(ctx.Err(), context.Canceled) {
					t.Fatal("authoritative replacement did not retire review")
				}
				return Answer{ChoiceIDs: []string{"acceptForSession"}}, nil
			})
			if err := a.ReviewRequests(context.Background()); err == nil {
				t.Fatal("superseded request was answered")
			}
			want := 0
			if revised {
				want = 1
			}
			if a.Status().Requests != want || len(requestsFromLog(t, path)) != 0 || state.Pending("fixture-env", false) != nil {
				t.Fatal("stale review mutated or live offer replacement was ignored")
			}
		})
	}
}

func TestHistoryRequestsAnswerUsesExactOfferWithoutMovingPage(t *testing.T) {
	a, state, path := recoverySession(t, "out={'result':{'sequence':10}}")
	historyRequestView(t, a, 2, false, approval("reviewed-revision"))
	historyRequestView(t, a, 3, true)
	a.asker = recoveryAsker(func(_ context.Context, _ Question) (Answer, error) {
		return Answer{ChoiceIDs: []string{"acceptForSession"}}, nil
	})
	if err := a.ReviewRequests(context.Background()); err != nil {
		t.Fatal(err)
	}
	packets := requestsFromLog(t, path)
	if len(packets) != 1 || packets[0]["op"] != "answer" || packets[0]["requestId"] != "request-id" || packets[0]["revision"] != "reviewed-revision" || packets[0]["decision"] != "acceptForSession" {
		t.Fatalf("historical review changed exact offer: %#v", packets)
	}
	if e := a.renderEvent(); !e.Status.Historical || e.Follow || e.ResetScroll || state.Pending("fixture-env", false) != nil {
		t.Fatal("answer changed history/scroll or recovery")
	}
	if err := a.Send(context.Background(), "remain local"); err == nil {
		t.Fatal("answer exception enabled historical send")
	}
}

func TestHistoryRequestsNonliveUpdatesDoNotResolveOffers(t *testing.T) {
	a, _, _ := recoverySession(t, "out={'result':{'sequence':10}}")
	historyRequestView(t, a, 2, false, approval("verified"))
	for i, state := range []string{"synchronizing", "unavailable"} {
		var page map[string]any
		_ = json.Unmarshal(recoveryView(i+3, false, nil), &page)
		page["state"], page["live"] = state, false
		data, _ := json.Marshal(page)
		if err := a.applyPacket(wire.Packet{Event: "view", Data: data}); err != nil {
			t.Fatal(err)
		}
		if a.Status().Requests != 1 {
			t.Fatalf("%s resolved an offer without current authority", state)
		}
	}
	historyRequestView(t, a, 5, false)
	if a.Status().Requests != 0 {
		t.Fatal("current live removal did not clear retained offer")
	}
}

func TestHistoryRequestsRetainedSnapshotCannotRewriteOffer(t *testing.T) {
	a, _, path := recoverySession(t, "out={'result':{'sequence':10}}")
	historyRequestView(t, a, 2, false, approval("verified"))
	a.store.Reset()
	a.store.Select("thread")
	// Retained snapshots are available to the UI while the helper reconnects.
	page := a.renderEvent().Snapshot
	page.Metadata.Requests[0]["id"] = "unoffered"
	page.Metadata.Requests[0]["options"].([]any)[0].(map[string]any)["decision"] = "acceptAlways"
	historyRequestView(t, a, 1, true)
	a.asker = recoveryAsker(func(_ context.Context, q Question) (Answer, error) {
		if q.Choices[0].ID != "acceptForSession" {
			t.Fatal("snapshot reader changed canonical offered decision")
		}
		return Answer{ChoiceIDs: []string{"acceptForSession"}}, nil
	})
	if err := a.ReviewRequests(context.Background()); err != nil {
		t.Fatal(err)
	}
	if packets := requestsFromLog(t, path); len(packets) != 1 || packets[0]["requestId"] != "request-id" {
		t.Fatalf("snapshot reader changed request authority: %#v", packets)
	}
}

func TestHistoryRequestsUncertainAnswerRetriesOriginalIDAfterOfferRemoval(t *testing.T) {
	a, state, path := recoverySession(t, "out={'error':{'code':'disconnected','message':'lost acknowledgement','uncertain':True}} if count==1 else {'result':{'sequence':10}}")
	historyRequestView(t, a, 2, false, approval("original-revision"))
	historyRequestView(t, a, 3, true)
	a.asker = recoveryAsker(func(_ context.Context, _ Question) (Answer, error) {
		return Answer{ChoiceIDs: []string{"acceptForSession"}}, nil
	})
	if err := a.ReviewRequests(context.Background()); err == nil || !wire.IsUncertain(err) {
		t.Fatalf("lost answer acknowledgement was not uncertain: %v", err)
	}
	pending := state.Pending("fixture-env", false)
	if pending == nil || pending["requestId"] != "request-id" || pending["revision"] != "original-revision" {
		t.Fatal("uncertain answer lost its original target")
	}
	// Seeing removal never proves the uncertain operation's receipt. Return
	// to history and explicitly retry the durable original operation only.
	historyRequestView(t, a, 4, false)
	historyRequestView(t, a, 5, true)
	if a.Status().Requests != 0 || len(requestsFromLog(t, path)) != 1 || state.Pending("fixture-env", false) == nil {
		t.Fatal("projection removal resolved or replayed uncertain answer")
	}
	if _, err := a.Retry(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	packets := requestsFromLog(t, path)
	if len(packets) != 2 || packets[0]["id"] != packets[1]["id"] || packets[1]["requestId"] != "request-id" || packets[1]["revision"] != "original-revision" || packets[1]["decision"] != "acceptForSession" || state.Pending("fixture-env", false) != nil || !a.Status().Historical {
		t.Fatalf("retry changed original identity/content, recovery or history: %#v", packets)
	}
}

func TestHistoryRequestsRetainedOfferDoesNotBypassWriteSafety(t *testing.T) {
	for _, blocked := range []string{"read-only", "unverified", "draft-failure", "navigating", "resnapshot"} {
		t.Run(blocked, func(t *testing.T) {
			a, state, path := recoverySession(t, "out={'result':{'sequence':10}}")
			historyRequestView(t, a, 2, false, approval("verified"))
			historyRequestView(t, a, 3, true)
			a.mu.Lock()
			switch blocked {
			case "read-only":
				a.writable = false
			case "unverified":
				a.helloVerified = false
			case "draft-failure":
				a.draftError = errors.New("draft persistence failed")
			case "navigating":
				a.navigating = true
			case "resnapshot":
				_, _ = a.store.Apply("delta", []byte(`{}`))
			}
			a.mu.Unlock()
			a.asker = recoveryAsker(func(_ context.Context, _ Question) (Answer, error) {
				return Answer{ChoiceIDs: []string{"acceptForSession"}}, nil
			})
			if err := a.ReviewRequests(context.Background()); err == nil {
				t.Fatal("retained request bypassed " + blocked)
			}
			if state.Pending("fixture-env", false) != nil || len(requestsFromLog(t, path)) != 0 {
				t.Fatal("blocked answer allocated recovery state or reached host")
			}
			a.mu.Lock()
			a.draftError = nil
			a.mu.Unlock()
		})
	}
}
