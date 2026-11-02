package integration_test

import (
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
