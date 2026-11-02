package client

import (
	"context"
	"github.com/Xon333/t3oxide/internal/config"
	"testing"
)

func TestBrowseFilterExactSourceAndProject(t *testing.T) {
	page := NavigatorPage{ProfileID: "one", EnvironmentID: "env", Page: ThreadPage{Threads: []Thread{{ID: "same", ProjectID: "p"}, {ID: "other", ProjectID: "q"}, {ID: "unknown", Project: "p"}}}}
	got, err := FilterBrowsePage(page, BrowseScope{"one", "env", "p"})
	if err != nil || len(got.Page.Threads) != 1 || got.Page.Threads[0].ID != "same" {
		t.Fatalf("exact project filter: %+v %v", got, err)
	}
	if len(page.Page.Threads) != 3 {
		t.Fatal("filter mutated original page")
	}
	if _, err = FilterBrowsePage(page, BrowseScope{"two", "env", "p"}); err == nil {
		t.Fatal("accepted wrong profile")
	}
	if _, err = FilterBrowsePage(page, BrowseScope{"one", "other", "p"}); err == nil {
		t.Fatal("accepted wrong environment")
	}
}
func TestBatchReceiptActionRejectsStaleRecoveryAndPreservesDraft(t *testing.T) {
	a, state, _ := recoverySession(t, "out={'result':{}}")
	r := config.BatchReceipt{ID: "receipt", EnvironmentID: "fixture-env", Source: "\x00file-many/source", Original: "task\n", CreatedAt: "2026-11-02T00:00:00Z", Items: []config.BatchReceiptItem{{Start: 0, End: 5, State: config.BatchRemainingDraft}}}
	if err := state.SetDraft(r.EnvironmentID, r.Source, r.Original); err != nil {
		t.Fatal(err)
	}
	if err := state.BeginBatchReceipt(r); err != nil {
		t.Fatal(err)
	}
	if _, err := a.BatchReceiptAction(context.Background(), r.ID, 9); err == nil {
		t.Fatal("accepted missing item")
	}
	if _, err := a.BatchReceiptAction(context.Background(), "other", 0); err == nil {
		t.Fatal("accepted missing receipt")
	}
	if err := state.SetDraft(r.EnvironmentID, r.Source, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := a.BatchReceiptAction(context.Background(), r.ID, 0); err == nil {
		t.Fatal("accepted absent remainder")
	}
	if len(a.BatchReceipts()) != 1 {
		t.Fatal("lost receipt")
	}
}

func TestBatchRemainderEditorRejectsConcurrentHumanEdits(t *testing.T) {
	a, state, _ := recoverySession(t, "out={'result':{}}")
	r := config.BatchReceipt{ID: "receipt", EnvironmentID: "fixture-env", Source: "\x00file-many/source", Original: "task\n", CreatedAt: "2026-11-02T00:00:00Z", Items: []config.BatchReceiptItem{{Start: 0, End: 5, State: config.BatchRemainingDraft}}}
	if err := state.SetDraft(r.EnvironmentID, r.Source, r.Original); err != nil {
		t.Fatal(err)
	}
	if err := state.BeginBatchReceipt(r); err != nil {
		t.Fatal(err)
	}
	a.asker = recoveryAsker(func(_ context.Context, q Question) (Answer, error) {
		if q.Initial != r.Original || q.StoreText == nil {
			t.Fatal("lost exact remainder")
		}
		if err := state.SetDraft(r.EnvironmentID, r.Source, "newer human text"); err != nil {
			t.Fatal(err)
		}
		if err := q.StoreText("stale editor"); err == nil {
			t.Fatal("overwrote newer draft")
		}
		return Answer{Text: "stale final answer"}, nil
	})
	if _, err := a.BatchReceiptAction(context.Background(), r.ID, 0); err == nil {
		t.Fatal("accepted stale final answer")
	}
	if got := state.Draft(r.EnvironmentID, r.Source); got != "newer human text" {
		t.Fatalf("draft overwritten: %q", got)
	}
	if state.Pending(r.EnvironmentID, false) != nil {
		t.Fatal("editing dispatched an operation")
	}
}
