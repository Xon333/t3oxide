package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeCreationProfileDefaultIsOptionalAndUniqueAmongEnabledProfiles(t *testing.T) {
	first, second := testEnvironment(), testEnvironment()
	second.ID, second.EnvironmentID = "second", "second-env"
	first.DefaultForNew, second.DefaultForNew = true, true
	path := filepath.Join(t.TempDir(), "environments.json")
	write := func() {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"version": 1, "environments": []Environment{first, second}})
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	if _, err := Load(path); err == nil {
		t.Fatal("two enabled creation defaults accepted")
	}
	second.Enabled = false
	write()
	profiles, err := Load(path)
	if err != nil || len(profiles) != 1 || !profiles[0].DefaultForNew {
		t.Fatalf("enabled creation default failed: %+v %v", profiles, err)
	}
}

func TestNativeLaunchReceiptClearsOnlyIdenticalProjectWorkspaceForm(t *testing.T) {
	s, err := OpenState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	selection := modelSelectionFixture()
	draft := LaunchDraft{Title: " exact title ", Message: "\nexact body", ProjectID: "project", Workspace: "/fixture/root", Selection: &selection, NativeDefaultsRevision: strings.Repeat("a", 64), NativeWorkspaceRoot: "/fixture/root", WorkspaceStrategy: &WorkspaceStrategy{Type: "root"}, RuntimeMode: "approval-required", InteractionMode: "default"}
	fields := map[string]any{"id": "command", "op": "launch-project", "environmentId": "env", "threadId": "new-thread", "newThreadId": "new-thread", "projectId": "project", "title": draft.Title, "message": draft.Message, "formSource": "project-launch/project", "modelSelection": selection, "workspaceStrategy": *draft.WorkspaceStrategy, "defaultsRevision": draft.NativeDefaultsRevision}
	if err = s.SetLaunchDraft("env", "project-launch/project", draft); err != nil {
		t.Fatal(err)
	}
	if err = s.SavePending("env", false, fields); err != nil {
		t.Fatal(err)
	}
	newer := draft
	newer.WorkspaceStrategy = &WorkspaceStrategy{Type: "worktree", BaseRef: "main"}
	if err = s.SetLaunchDraft("env", "project-launch/project", newer); err != nil {
		t.Fatal(err)
	}
	if err = s.Acknowledge("env", false, "command", "new-thread", ""); err != nil {
		t.Fatal(err)
	}
	if got, ok := s.LaunchDraft("env", "project-launch/project"); !ok || !launchDraftEqual(got, newer) {
		t.Fatal("receipt cleared newer workspace choice")
	}
	if err = s.SetLaunchDraft("env", "project-launch/project", draft); err != nil {
		t.Fatal(err)
	}
	if err = s.SavePending("env", false, fields); err != nil {
		t.Fatal(err)
	}
	if err = s.Acknowledge("env", false, "command", "new-thread", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.LaunchDraft("env", "project-launch/project"); ok || s.Draft("env", "\x00new/project-launch/project") != "" {
		t.Fatal("exact receipt did not atomically clear its form")
	}
}
