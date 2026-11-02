package client

import (
	"context"
	"strings"
	"testing"
)

func TestReceiptRecoveryReviewsOnlyExactOperation(t *testing.T) {
	a, state, _ := recoverySession(t, "out={'result':{}}")
	for _, item := range []struct {
		id   string
		stop bool
	}{{"normal-original", false}, {"stop-original", true}} {
		op := "send"
		if item.stop {
			op = "stop"
		}
		if err := state.SavePending("fixture-env", item.stop, map[string]any{"id": item.id, "op": op, "environmentId": "fixture-env", "threadId": "thread", "message": "exact human bytes\r\n"}); err != nil {
			t.Fatal(err)
		}
	}
	asked := 0
	a.asker = recoveryAsker(func(_ context.Context, q Question) (Answer, error) {
		asked++
		if !strings.Contains(q.Prompt, "normal-original") || strings.Contains(q.Prompt, "stop-original") {
			t.Fatal("recovery used a different saved operation")
		}
		return Answer{ChoiceIDs: []string{"keep"}}, nil
	})
	if _, err := a.ReviewSavedOperation(context.Background(), "other-env", "normal-original"); err == nil {
		t.Fatal("wrong environment accepted")
	}
	if _, err := a.ReviewSavedOperation(context.Background(), "fixture-env", "missing"); err == nil {
		t.Fatal("wrong operation accepted")
	}
	if asked != 0 {
		t.Fatal("invalid identity opened recovery")
	}
	if _, err := a.ReviewSavedOperation(context.Background(), "fixture-env", "normal-original"); err != nil {
		t.Fatal(err)
	}
	if asked != 1 || state.Pending("fixture-env", false) == nil || state.Pending("fixture-env", true) == nil {
		t.Fatal("review discarded or retried saved work")
	}
}
