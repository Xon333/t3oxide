package config

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestBrowserPreferencesPersistAndCloneExactSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	preferences := BrowserPreferences{ProfileIDs: []string{"profile-a", "profile-b"}, LastProfileID: "profile-b"}
	if err := s.SetBrowserPreferences(preferences); err != nil {
		t.Fatal(err)
	}
	preferences.ProfileIDs[0] = "mutated"
	got := s.BrowserPreferences()
	if !reflect.DeepEqual(got.ProfileIDs, []string{"profile-a", "profile-b"}) || got.LastProfileID != "profile-b" {
		t.Fatalf("stored browser preferences changed through caller: %#v", got)
	}
	got.ProfileIDs[1] = "mutated"
	if s.BrowserPreferences().ProfileIDs[1] != "profile-b" {
		t.Fatal("browser preference accessor aliases saved state")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got = s.BrowserPreferences(); !reflect.DeepEqual(got.ProfileIDs, []string{"profile-a", "profile-b"}) || got.LastProfileID != "profile-b" {
		t.Fatalf("browser preferences did not persist: %#v", got)
	}
	if err := s.SetBrowserPreferences(BrowserPreferences{ProfileIDs: []string{"profile-a"}, LastProfileID: "missing"}); err == nil {
		t.Fatal("last browser profile outside selected list accepted")
	}
}

func TestLaunchDraftFullFormRestoresAndOnlyExactAcknowledgementClearsIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	selection := modelSelectionFixture()
	draft := LaunchDraft{Title: " exact title ", Message: "first\nmessage", ProjectID: "project-id", Workspace: "Existing workspace", Selection: &selection}
	if err = s.SetLaunchDraft("env", "source/thread", draft); err != nil {
		t.Fatal(err)
	}
	if s.Draft("env", "\x00new/source/thread") != draft.Message {
		t.Fatal("compatibility launch message draft was not committed")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	restored, ok := s.LaunchDraft("env", "source/thread")
	if !ok || !launchDraftEqual(restored, draft) {
		t.Fatalf("exact launch form did not restore: %#v", restored)
	}
	restored.Selection.Options[0].Value = "changed"
	unchanged, ok := s.LaunchDraft("env", "source/thread")
	if !ok || unchanged.Selection.Options[0].Value != "high" {
		t.Fatal("launch selection accessor aliases state")
	}
	pending := map[string]any{
		"id": "launch-id", "op": "launch", "environmentId": "env", "threadId": "source/thread",
		"title": draft.Title, "message": draft.Message, "modelSelection": selection,
	}
	if err = s.SavePending("env", false, pending); err != nil {
		t.Fatal(err)
	}
	changedSelection := selection.WithOption("reasoningEffort", "low")
	newer := LaunchDraft{Title: "new title", Message: "new first message", ProjectID: "another-project", Workspace: "another workspace", Selection: &changedSelection}
	if err = s.SetLaunchDraft("env", "source/thread", newer); err != nil {
		t.Fatal(err)
	}
	if err = s.Acknowledge("env", false, "launch-id", "source/thread", draft.Message); err != nil {
		t.Fatal(err)
	}
	if restored, ok = s.LaunchDraft("env", "source/thread"); !ok || !launchDraftEqual(restored, newer) {
		t.Fatal("acknowledgement cleared a newer launch form")
	}
	if err = s.SetLaunchDraft("env", "source/thread", draft); err != nil {
		t.Fatal(err)
	}
	if err = s.SavePending("env", false, pending); err != nil {
		t.Fatal(err)
	}
	if err = s.Acknowledge("env", false, "launch-id", "source/thread", draft.Message); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.LaunchDraft("env", "source/thread"); ok || s.Draft("env", "\x00new/source/thread") != "" {
		t.Fatal("exact acknowledgement did not clear only the submitted launch form")
	}
}
