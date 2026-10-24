package client

import (
	"context"
	"testing"
)

func TestNavigatorChangesRemainLocalAndUseOfferedPage(t *testing.T) {
	m, _, env := recoveryManager(t)
	s, err := m.NewSession(env.ID, "attached")
	if err != nil {
		t.Fatal(err)
	}
	page := NavigatorPage{ProfileID: env.ID, Tree: true, Page: ThreadPage{Threads: []Thread{{ID: "root", Title: "Root", ChildCount: 2}}, Limit: 24, NextCursor: new(24)}}
	n := m.navigation(env.EnvironmentID)
	if n.Lifecycle != "active" || len(n.Expanded) != 0 || n.Cursor != 0 {
		t.Fatal("quiet initial scope changed")
	}
	for _, tc := range []struct{ action, target string }{{"expand", "missing"}, {"open", "root"}, {"type:unknown", ""}, {"previous", ""}} {
		if s.NavigatorChange(page, tc.action, tc.target) == nil {
			t.Fatalf("accepted unoffered action %+v", tc)
		}
	}
	if err = s.NavigatorChange(page, "expand", "root"); err != nil {
		t.Fatal(err)
	}
	if !m.navigation(env.EnvironmentID).Expanded["root"] || s.thread != "attached" || s.conn != nil {
		t.Fatal("explicit expansion attached a transcript")
	}
	if err = s.NavigatorChange(page, "next", ""); err != nil {
		t.Fatal(err)
	}
	if m.navigation(env.EnvironmentID).Cursor != 24 {
		t.Fatal("offered continuation missing")
	}
	if err = s.NavigatorChange(page, "search", "finished work"); err != nil {
		t.Fatal(err)
	}
	n = m.navigation(env.EnvironmentID)
	if n.Cursor != 0 || n.Lifecycle != "all" {
		t.Fatal("host search retained hidden lifecycle bound")
	}
	if err = s.NavigatorChange(page, "type:forks", ""); err != nil {
		t.Fatal(err)
	}
	if m.navigation(env.EnvironmentID).Filter != "forks" {
		t.Fatal("forks were conflated with subagents")
	}
}

func TestNavigatorOpenAndSourceRequireExactOfferedEnvironmentAndThread(t *testing.T) {
	m, state, env := recoveryManager(t)
	s, err := m.NewSession(env.ID, "attached")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetDraft("exact outgoing draft\n界"); err != nil {
		t.Fatal(err)
	}
	page := NavigatorPage{ProfileID: env.ID, Page: ThreadPage{Threads: []Thread{{ID: "offered"}}}}
	if _, err = s.NavigatorOpen(page, "unoffered"); err == nil {
		t.Fatal("opened unoffered row")
	}
	other := page
	other.ProfileID = "unconfigured"
	if _, err = s.NavigatorOpen(other, "offered"); err == nil {
		t.Fatal("opened unconfigured environment")
	}
	if _, err = s.NavigatorSource(context.Background(), page, "unoffered", "settings"); err == nil {
		t.Fatal("settings accepted unoffered source")
	}
	if _, err = s.NavigatorSource(context.Background(), page, "offered", "arbitrary-write"); err == nil {
		t.Fatal("source accepted unoffered action")
	}
	next, err := s.NavigatorOpen(page, "offered")
	if err != nil {
		t.Fatal(err)
	}
	if next.thread != "offered" || next.conn != nil || next.started || s.thread != "attached" {
		t.Fatal("highlight/open changed attachment before explicit listener")
	}
	if state.Draft(env.EnvironmentID, "attached") != "exact outgoing draft\n界" {
		t.Fatal("switch lost outgoing draft")
	}
}

func TestNavigatorUnsupportedLineageAndUnknownProfileFailBeforeIO(t *testing.T) {
	m, _, env := recoveryManager(t)
	s, err := m.NewSession(env.ID, "attached")
	if err != nil {
		t.Fatal(err)
	}
	page := NavigatorPage{ProfileID: env.ID, Page: ThreadPage{Threads: []Thread{{ID: "root", ChildCount: 2}}}}
	for _, action := range []string{"expand", "type:subagents"} {
		if s.NavigatorChange(page, action, "root") == nil {
			t.Fatal("unsupported lineage became available")
		}
	}
	if _, err = s.NavigatorRead(context.Background(), "unknown", ""); err == nil {
		t.Fatal("unknown profile reached helper")
	}
	profiles := s.NavigatorProfiles()
	profiles[0].ID = "changed"
	if s.NavigatorProfiles()[0].ID != env.ID {
		t.Fatal("profile presentation mutated configuration")
	}
}
