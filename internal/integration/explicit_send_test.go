package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/Xon333/t3oxide/internal/client"
)

func TestProgramEnterNewlineExplicitSendAndLiteralUseRealAdapter(t *testing.T) {
	f := newFixture(t)
	manager, state := newAppManager(t, f)
	session, err := manager.NewSession("fixture", "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	r, _ := startAppTUI(t, session)
	r.waitText("Initial Markdown for Alpha")
	r.send("first line\rsecond line")
	r.waitText("second line")
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("plain Enter dispatched before explicit Send")
	}
	r.send("\x1d") // Ctrl+] through the actual Bubble Tea input decoder.
	f.waitMessage(t, "first line\nsecond line")
	awaitStatus(t, session, func(s client.Status) bool {
		return s.Receipt != nil && s.Receipt.Phase == "responding" && !s.PendingAction
	})
	r.waitEmptyComposer()
	r.send("/help\x1c") // Ctrl+\ explicitly sends a recognized command literally.
	f.waitMessage(t, "/help")
	awaitStatus(t, session, func(s client.Status) bool {
		return s.Receipt != nil && s.Receipt.Phase == "responding" && !s.PendingAction
	})
	if state.Pending("env-a", false) != nil {
		t.Fatal("acknowledged sends retained a pending ordinary operation")
	}
	f.mu.Lock()
	commands := make([]object, 0, len(f.commands))
	for _, command := range f.commands {
		commands = append(commands, clone(command))
	}
	f.mu.Unlock()
	for _, command := range commands {
		if command["type"] == "message.dispatch" && command["deliveryIntent"] != "auto" {
			t.Fatalf("normal Send changed default auto delivery: %#v", command)
		}
	}
	r.send("\x11")
	<-r.done
}

func TestOneMessageSteerPreservesDeliveryDraftAndFormalRequests(t *testing.T) {
	f := newFixture(t)
	_, state, session, _ := startFeatureSession(t, f, "thread-a")
	if err := session.SetDelivery("queue"); err != nil {
		t.Fatal(err)
	}
	const message = "  adjust this exact running task\nsecond line  "
	if err := session.StoreDraft(message); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	beforeRequests := session.Status().Requests
	if err := session.SendWithDelivery(ctx, message, "steer"); err != nil {
		t.Fatal(err)
	}
	f.waitMessage(t, message)
	if session.Status().Delivery != "queue" || session.Status().Requests != beforeRequests {
		t.Fatal("one-message steer changed future delivery or dismissed a formal request")
	}
	if session.Draft() != "" || state.Pending("env-a", false) != nil {
		t.Fatal("steer did not share normal draft acknowledgement/journal cleanup")
	}
	f.mu.Lock()
	commands := make([]object, 0, len(f.commands))
	for _, command := range f.commands {
		commands = append(commands, clone(command))
	}
	f.mu.Unlock()
	for _, command := range commands {
		if command["type"] == "message.dispatch" && command["text"] == message && command["deliveryIntent"] != "steer" {
			t.Fatalf("explicit steer changed native delivery intent: %#v", command)
		}
	}
	effects, dispatches := f.counts()
	if err := session.SendWithDelivery(ctx, "must not send", "guess"); err == nil {
		t.Fatal("unrecognized one-message delivery was accepted")
	}
	if afterEffects, afterDispatches := f.counts(); afterEffects != effects || afterDispatches != dispatches {
		t.Fatal("invalid delivery reached the native adapter")
	}
}
