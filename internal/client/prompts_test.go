package client

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Xon333/t3oxide/internal/wire"
)

func TestPromptReplyIsCorrelatedSingleUseAndCopied(t *testing.T) {
	b := newPromptBroker()
	result := make(chan Answer, 1)
	go func() {
		a, err := b.Ask(context.Background(), Question{Kind: ChooseMany, Title: "Exact request", Choices: []Choice{{ID: "a"}, {ID: "b"}}})
		if err != nil {
			result <- Answer{Cancelled: true}
			return
		}
		result <- a
	}()
	p := <-b.requests
	ids := []string{"a"}
	if !p.Respond(Answer{ChoiceIDs: ids}) || p.Respond(Answer{ChoiceIDs: []string{"b"}}) {
		t.Fatal("prompt accepted zero or multiple replies")
	}
	ids[0] = "changed after delivery"
	answer := <-result
	if answer.Cancelled || len(answer.ChoiceIDs) != 1 || answer.ChoiceIDs[0] != "a" {
		t.Fatalf("reply ownership lost: %#v", answer)
	}
	select {
	case <-p.Done:
	case <-time.After(time.Second):
		t.Fatal("answered prompt did not retire")
	}
	if p.Respond(Answer{}) {
		t.Fatal("retired prompt accepted another answer")
	}
}

func TestOldRequestCleanupDoesNotRetireReopenedSameIdentity(t *testing.T) {
	s, _, _ := recoverySession(t, "out={'result':{'sequence':10}}")
	if err := s.applyPacket(wire.Packet{Event: "view", Data: recoveryView(2, false, []map[string]any{approval("same-revision")})}); err != nil {
		t.Fatal(err)
	}
	firstStarted, secondStarted := make(chan struct{}), make(chan struct{})
	releaseOld := make(chan struct{})
	var release sync.Once
	defer release.Do(func() { close(releaseOld) })
	var calls atomic.Int32
	s.asker = recoveryAsker(func(ctx context.Context, _ Question) (Answer, error) {
		if calls.Add(1) == 1 {
			close(firstStarted)
			<-releaseOld
		} else {
			close(secondStarted)
			<-ctx.Done()
		}
		return Answer{Cancelled: true}, ctx.Err()
	})
	oldDone, newDone := make(chan error, 1), make(chan error, 1)
	go func() { oldDone <- s.ReviewRequests(context.Background()) }()
	<-firstStarted
	// Latest cancels the old review but the same authoritative request can still
	// be present when a new review opens before old cleanup has completed.
	if err := s.Navigate(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	go func() { newDone <- s.ReviewRequests(context.Background()) }()
	<-secondStarted
	release.Do(func() { close(releaseOld) })
	<-oldDone
	s.mu.Lock()
	active := s.requestCancel != nil
	s.mu.Unlock()
	if !active {
		t.Fatal("old request cleanup retired the new review")
	}
	if err := s.applyPacket(wire.Packet{Event: "view", Data: recoveryView(3, false, nil)}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-newDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("wrong retirement result: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("new review could not be cancelled after request removal")
	}
}

func TestDetachedOperationsDoNotAllocateOrSendNewWrites(t *testing.T) {
	s, state, path := recoverySession(t, "out={'result':{'sequence':10}}")
	s.cancel()
	if err := s.Send(context.Background(), "late queued UI command"); !errors.Is(err, context.Canceled) {
		t.Fatalf("detached send: %v", err)
	}
	if err := s.Stop(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("detached Stop: %v", err)
	}
	if state.Pending("fixture-env", false) != nil || state.Pending("fixture-env", true) != nil || len(requestsFromLog(t, path)) != 0 {
		t.Fatal("detached operation allocated or sent a new write")
	}
}

func TestPromptCancellationRejectsLateAnswerAndConcurrentInteraction(t *testing.T) {
	b := newPromptBroker()
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { _, err := b.Ask(ctx, Question{Kind: EnterText, Title: "Original"}); finished <- err }()
	p := <-b.requests
	if _, err := b.Ask(context.Background(), Question{Kind: EnterText, Title: "Unexpected concurrent dialog"}); err == nil {
		t.Fatal("concurrent interaction was silently queued")
	}
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("wrong cancellation result: %v", err)
	}
	if p.Respond(Answer{Text: "late"}) {
		t.Fatal("canceled prompt accepted late answer")
	}
	nextResult := make(chan Answer, 1)
	go func() {
		answer, _ := b.Ask(context.Background(), Question{Kind: ChooseOne, Title: "New request"})
		nextResult <- answer
	}()
	next := <-b.requests
	if next.ID == p.ID {
		t.Fatal("prompt identity reused")
	}
	if !next.Respond(Answer{Cancelled: true}) || !(<-nextResult).Cancelled {
		t.Fatal("dismissal changed semantics")
	}
}

func TestSessionHasOnlyOneListenerAndClosingRetiresPrompts(t *testing.T) {
	m, _, env := recoveryManager(t)
	env2 := env
	env2.ID, env2.EnvironmentID = "other-profile", "other-environment"
	m.Environments = append(m.Environments, env2)
	s, err := m.NewSession("", "")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range s.Events(context.Background()) {
		}
	}()
	p := <-s.Prompts()
	seenError := false
	for _, err := range s.Events(context.Background()) {
		if err != nil {
			seenError = true
		}
	}
	if !seenError {
		t.Fatal("duplicate Events acquired another listener")
	}
	s.Close()
	select {
	case <-p.Done:
	case <-time.After(time.Second):
		t.Fatal("detach left old prompt live")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("detach left listener alive")
	}
	if p.Respond(Answer{ChoiceIDs: []string{env.ID}}) {
		t.Fatal("detached environment dialog accepted reply")
	}
}
