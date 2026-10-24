package client

import (
	"context"
	"testing"

	"github.com/Xon333/t3oxide/internal/config"
)

func TestLaunchValidationRequiresAdvertisedExplicitOptions(t *testing.T) {
	catalog, err := DecodeCatalog(clientCatalogFixture())
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.validateLaunch(catalog.Current); err == nil {
		t.Fatal("explicit launch selection retained an unadvertised source option")
	}
	selection := catalog.Current.WithOption("opaque", nil).WithOption("reasoningEffort", "low")
	if err := catalog.validateLaunch(selection); err != nil {
		t.Fatalf("advertised explicit launch selection rejected: %v", err)
	}
	if _, ok := catalog.Current.Option("opaque"); !ok {
		t.Fatal("launch validation modified the retained source selection")
	}
}

func TestRestoredLaunchRefusesChangedWorkspaceUntilExplicitRebind(t *testing.T) {
	for _, rebind := range []bool{false, true} {
		t.Run(map[bool]string{false: "keep", true: "explicit rebind"}[rebind], func(t *testing.T) {
			a, state, path := recoverySession(t, "out={'result':{'sequence':10}}")
			old := config.LaunchDraft{Title: "saved title", Message: "saved first message", ProjectID: "old-project", Workspace: "old workspace"}
			if err := state.SetLaunchDraft("fixture-env", "thread", old); err != nil {
				t.Fatal(err)
			}
			calls := 0
			a.asker = recoveryAsker(func(_ context.Context, q Question) (Answer, error) {
				calls++
				if calls == 1 {
					if q.Title != "Saved launch workspace changed" {
						t.Fatalf("changed binding was not reviewed first: %#v", q)
					}
					if rebind {
						return Answer{ChoiceIDs: []string{"rebind"}}, nil
					}
					return Answer{ChoiceIDs: []string{"keep"}}, nil
				}
				if !rebind || q.Surface != SurfaceLaunch || q.LaunchReview.Location.ProjectID != "project" {
					t.Fatalf("unexpected follow-up after binding decision: %#v", q)
				}
				return Answer{Cancelled: true, Launch: &LaunchInput{Title: old.Title, Message: old.Message}}, nil
			})
			result, err := a.newConversation(context.Background(), "")
			if err != nil || result.Next != nil {
				t.Fatalf("workspace decision created or failed launch: %+v %v", result, err)
			}
			saved, ok := state.LaunchDraft("fixture-env", "thread")
			if !ok {
				t.Fatal("workspace decision lost the prepared launch")
			}
			if rebind {
				if saved.ProjectID != "project" || saved.Workspace != "Existing workspace" || saved.Title != old.Title || saved.Message != old.Message {
					t.Fatalf("explicit rebind did not preserve and retarget the complete form: %#v", saved)
				}
			} else if saved.ProjectID != old.ProjectID || saved.Workspace != old.Workspace {
				t.Fatalf("keeping the form silently rebased it: %#v", saved)
			}
			if len(requestsFromLog(t, path)) != 0 {
				t.Fatal("workspace review dispatched a launch")
			}
		})
	}
}
