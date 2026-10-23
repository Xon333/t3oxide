package client

import (
	"context"
	"testing"
)

func TestExactPromptSendUsesExistingAcknowledgmentAndRecovery(t *testing.T) {
	a, state, path := recoverySession(t, "out={'result':{'sequence':10}}")
	text := " \tfunc main() { � }\r\n\n "
	if err := a.StoreDraft(text); err != nil {
		t.Fatal(err)
	}
	if err := a.Send(context.Background(), text); err != nil {
		t.Fatal(err)
	}
	requests := requestsFromLog(t, path)
	if len(requests) != 1 || requests[0]["message"] != text {
		t.Fatalf("send changed bytes: %#v", requests)
	}
	entries := a.PromptHistory()
	if len(entries) != 1 || entries[0].Text != text || entries[0].Kind != "sent" {
		t.Fatalf("acknowledged bytes not recalled: %#v", entries)
	}
	if a.Draft() != "" || state.Pending("fixture-env", false) != nil {
		t.Fatal("acknowledged operation retained")
	}
	newer := "newer \t�\r\n"
	if err := a.StoreDraft(newer); err != nil {
		t.Fatal(err)
	}
	next, err := a.manager.NewSession("fixture", "thread")
	if err != nil {
		t.Fatal(err)
	}
	if next.Draft() != newer {
		t.Fatal("detach/reopen lost exact draft")
	}
}

func TestRejectedPromptDoesNotEnterHistory(t *testing.T) {
	a, _, _ := recoverySession(t, "out={'error':{'code':'rejected','message':'no'}}")
	text := "not acknowledged"
	if err := a.StoreDraft(text); err != nil {
		t.Fatal(err)
	}
	if err := a.Send(context.Background(), text); err == nil {
		t.Fatal("expected rejection")
	}
	if len(a.PromptHistory()) != 0 || a.Draft() != text {
		t.Fatal("rejection altered recovery history/draft")
	}
}
