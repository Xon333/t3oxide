package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"testing"
)

func TestCompactLaunchUsesOverlayAndEnterContinuesOnlyNewMessage(t *testing.T) {
	m := testModel(t)
	q := launchQuestion(client.LaunchInput{Title: "New conversation", Message: "message"})
	q.LaunchReview.Stage = "message"
	q.LaunchReview.Actions = []string{"continue", "cancel"}
	m.browserPage = true
	m.openPrompt(retiredPrompt(9910, q))
	if m.launchPage || m.browserPage {
		t.Fatal("New replaced workspace with a full page")
	}
	m.launchKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if m.dialog != nil {
		t.Fatal("Enter did not advance message stage")
	}
	q.LaunchReview.Stage = "ready"
	q.LaunchReview.Actions = []string{"create", "cancel", "model", "effort", "workspace"}
	m.openPrompt(retiredPrompt(9911, q))
	if m.dialog.launch.focus != 2 {
		t.Fatal("explicit launch was not preselected")
	}
	if got := m.launchActions(); len(got) != 5 || got[0] != "create" || got[2] != "model" || got[3] != "effort" {
		t.Fatalf("bloated final actions: %v", got)
	}
}
