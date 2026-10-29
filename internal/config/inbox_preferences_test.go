package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInboxPriorityCASRestartAndSecondClient(t *testing.T) {
	path := filepath.Join(t.TempDir(), "go-drafts.json")
	state, err := OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = state.SetDraft("env", "thread", "exact\n原文"); err != nil {
		t.Fatal(err)
	}
	initial, err := state.InboxPreferences()
	if err != nil {
		t.Fatal(err)
	}
	next := initial.Clone()
	next.Tasks[InboxIdentity("env", "project", "thread")] = 3
	saved, err := state.SaveInboxPreferences(initial.Revision, next)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Revision != 1 {
		t.Fatal("priority write did not advance revision")
	}
	stale := initial.Clone()
	stale.Tasks[InboxIdentity("env", "project", "thread")] = 0
	if _, err = state.SaveInboxPreferences(initial.Revision, stale); err == nil {
		t.Fatal("stale undo replaced a newer rank")
	}
	if second, err := OpenState(path); err == nil {
		second.Close()
		t.Fatal("second client stole the state/recovery owner")
	}
	if err = state.Close(); err != nil {
		t.Fatal(err)
	}
	state, err = OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	restored, err := state.InboxPreferences()
	if err != nil {
		t.Fatal(err)
	}
	if restored.Tasks[InboxIdentity("env", "project", "thread")] != 3 || state.Draft("env", "thread") != "exact\n原文" {
		t.Fatal("restart lost priority or source-bound draft")
	}
	info, err := os.Stat(path + ".inbox-v1.json")
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("rank file is not private")
	}
}

func TestInboxMetadataCannotBecomeTaskAuthorityAndMalformedStatePreserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "go-drafts.json")
	state, err := OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	malformed := `{"version":1,"revision":1,"taskPriorities":{},"projectRanks":{},"launchAt":"2030-01-01"}`
	if err = os.WriteFile(path+".inbox-v1.json", []byte(malformed), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = state.InboxPreferences(); err == nil {
		t.Fatal("execution-like unknown field was accepted as presentation state")
	}
	if _, err = state.SaveInboxPreferences(0, emptyInboxPreferences()); err == nil {
		t.Fatal("malformed preferences were overwritten")
	}
	retained, _ := os.ReadFile(path + ".inbox-v1.json")
	if string(retained) != malformed {
		t.Fatal("malformed file was not preserved")
	}
}

func TestInboxSourceIdentityIsUnambiguous(t *testing.T) {
	a := InboxIdentity("host/a", "project", "task")
	b := InboxIdentity("host", "a/project", "task")
	if a == b {
		t.Fatal("separator collision retargeted a rank")
	}
	for _, bad := range []string{`["env","project",""]`, `["env","project","bad\nthread"]`, `["env", "project", "thread"]`} {
		prefs := emptyInboxPreferences()
		prefs.Tasks[bad] = 2
		if validateInboxPreferences(prefs) == nil {
			t.Fatalf("accepted invalid identity %s", bad)
		}
	}
}
