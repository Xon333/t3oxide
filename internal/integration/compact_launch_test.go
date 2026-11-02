package integration_test

import (
	"context"
	"github.com/Xon333/t3oxide/internal/client"
	"testing"
	"time"
)

func TestCompactNewStagesPreserveNativeDefaultsAndClearAcknowledgedMessage(t *testing.T) {
	f := newFixture(t)
	_, state, a, _ := startFeatureSession(t, f, "thread-a")
	launch := featureCommand(a, "new", "")
	next := func() *client.Prompt {
		t.Helper()
		select {
		case p := <-a.Prompts():
			return p
		case <-time.After(5 * time.Second):
			t.Fatal("missing launch stage")
			return nil
		}
	}
	p := next()
	if p.Question.LaunchReview == nil || p.Question.LaunchReview.Stage != "message" {
		t.Fatalf("first stage: %+v", p.Question)
	}
	input := client.LaunchInput{Title: "New conversation", Message: " exact message\n"}
	p.Respond(client.Answer{Action: "continue", Launch: &input})
	p = next()
	if p.Question.Title != "Project" {
		t.Fatalf("project stage: %+v", p.Question)
	}
	p.Respond(client.Answer{ChoiceIDs: []string{"project-a"}})

	p = next()
	if p.Question.LaunchReview == nil || p.Question.LaunchReview.Stage != "ready" || p.Question.LaunchReview.Input != input {
		t.Fatalf("launch binding: %+v", p.Question)
	}
	if _, present := p.Question.LaunchReview.Selection.Option("serviceTier"); present {
		t.Fatal("untouched native default acquired an explicit service tier")
	}
	p.Respond(client.Answer{Action: "create", Launch: &input})
	result := featureReply(t, launch)
	if result.err != nil || result.result.Next == nil {
		t.Fatalf("launch failed: %v", result.err)
	}
	defer result.result.Next.Close()
	if _, ok := state.LaunchDraft("t3oxide-client", "new-message"); ok {
		t.Fatal("acknowledged message remains in next New")
	}
}

func TestCompactLaunchLostAckRecoveryClearsSubmittedMessage(t *testing.T) {
	f := newFixture(t)
	_, state, a, _ := startFeatureSession(t, f, "thread-a")
	started, release := f.holdNextLaunchAck()
	defer release()
	launch := featureCommand(a, "new", "")
	next := func() *client.Prompt {
		t.Helper()
		select {
		case p := <-a.Prompts():
			return p
		case <-time.After(5 * time.Second):
			t.Fatal("missing compact launch prompt")
			return nil
		}
	}
	p := next()
	input := client.LaunchInput{Title: "Recovered", Message: " exact recovery draft\n"}
	p.Respond(client.Answer{Action: "continue", Launch: &input})
	p = next()
	p.Respond(client.Answer{ChoiceIDs: []string{"project-a"}})
	p = next()
	p.Respond(client.Answer{Action: "create", Launch: &input})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("launch not received")
	}
	pending := state.Pending("env-a", false)
	if pending["compactMessageDraft"] != true {
		t.Fatal("compact draft binding was not journaled")
	}
	f.closeSockets()
	release()
	if reply := featureReply(t, launch); reply.err == nil {
		t.Fatal("lost acknowledgment inferred success")
	}
	if _, ok := state.LaunchDraft("t3oxide-client", "new-message"); !ok {
		t.Fatal("uncertain launch lost its message")
	}
	result, err := a.RetryLaunch(context.Background())
	if err != nil || result.Next == nil {
		t.Fatalf("recovery failed: %v", err)
	}
	defer result.Next.Close()
	if _, ok := state.LaunchDraft("t3oxide-client", "new-message"); ok {
		t.Fatal("recovered message returned in New")
	}
	if effects, dispatches := f.counts(); effects != 1 || dispatches != 1 {
		t.Fatalf("recovery duplicated launch: %d/%d", effects, dispatches)
	}
}
