package client

import (
	"context"
	"testing"

	"github.com/Xon333/t3oxide/internal/config"
)

func TestNativeCreationTargetIsExplicitDefaultOrChoiceNeverAttachment(t *testing.T) {
	m, _, first := recoveryManager(t)
	second := first
	second.ID, second.EnvironmentID, second.Label = "second", "second-environment", "Second"
	m.Environments = []config.Environment{first, second}
	a, err := m.NewSession(first.ID, "source")
	if err != nil {
		t.Fatal(err)
	}
	questions := 0
	a.asker = recoveryAsker(func(_ context.Context, q Question) (Answer, error) {
		questions++
		if q.Title != "New in environment" || len(q.Choices) != 2 {
			t.Fatalf("ambiguous default was not an explicit environment choice: %+v", q)
		}
		return Answer{ChoiceIDs: []string{second.ID}}, nil
	})
	chosen, err := a.creationEnvironment(context.Background())
	if err != nil || chosen.ID != second.ID || questions != 1 {
		t.Fatalf("attached profile became creation default: %s %v", chosen.ID, err)
	}
	m.Environments[1].DefaultForNew = true
	chosen, err = a.creationEnvironment(context.Background())
	if err != nil || chosen.ID != second.ID || questions != 1 {
		t.Fatal("configured default was not used")
	}
	m.CreationProfile = first.ID
	chosen, err = a.creationEnvironment(context.Background())
	if err != nil || chosen.ID != first.ID || questions != 1 {
		t.Fatal("explicit CLI creation profile did not win")
	}
	m.CreationProfile = "not-configured"
	if _, err = a.creationEnvironment(context.Background()); err == nil {
		t.Fatal("unconfigured explicit target silently fell back")
	}
}

func TestNativeProjectDecoderKeepsEmptyProjectIdentityAndRejectsUnboundedMetadata(t *testing.T) {
	valid := map[string]any{"projects": []any{map[string]any{"id": "project-with-no-thread", "title": "Empty", "workspaceRoot": "/fixture/empty"}}, "nextCursor": nil}
	projects, next, err := decodeProjects(valid)
	if err != nil || len(projects) != 1 || projects[0].ID != "project-with-no-thread" || next != nil {
		t.Fatalf("first-thread project lost: %+v %v", projects, err)
	}
	for _, bad := range []map[string]any{{"projects": nil}, {"projects": valid["projects"], "nextCursor": 1.5}, {"projects": []any{map[string]any{"id": "p", "workspaceRoot": "relative"}}}, {"projects": []any{map[string]any{"id": "p", "workspaceRoot": "/one"}, map[string]any{"id": "p", "workspaceRoot": "/two"}}}} {
		if _, _, err := decodeProjects(bad); err == nil {
			t.Fatalf("invalid project metadata accepted: %#v", bad)
		}
	}
}
