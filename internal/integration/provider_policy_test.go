package integration_test

import (
	"github.com/Xon333/t3oxide/internal/client"
	"testing"
)

func TestProviderPolicyRejectsDisallowedNativeProjectDefaultBeforeLaunch(t *testing.T) {
	f := newFixture(t)
	f.profile.AllowedProviderInstanceIDs = []string{"different-native-instance"}
	manager, _ := newAppManager(t, f)
	a, err := manager.NewSession(f.profile.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	launch := featureCommand(a, "new", "")
	prompt := featurePrompt(t, a)
	if prompt.Question.Title != "Project" {
		t.Fatalf("missing project picker: %+v", prompt.Question)
	}
	prompt.Respond(client.Answer{ChoiceIDs: []string{"project-a"}})
	reply := featureReply(t, launch)
	if reply.err == nil {
		t.Fatal("disallowed native provider launched")
	}
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("policy rejection mutated native work")
	}
}
