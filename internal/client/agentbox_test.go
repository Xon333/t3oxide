package client

import (
	"context"
	"testing"
	"time"

	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
)

func inboxFixtureRow(env, project, thread, status string) InboxThread {
	return InboxThread{Target: InboxTarget{ProfileID: env, EnvironmentID: env, ProjectID: project, ThreadID: thread, Revision: "revision"}, Thread: Thread{ID: thread, ProjectID: project, Status: status, UpdatedAt: "2026-10-29T12:00:00Z", Inbox: &wire.InboxMetadata{QueuedCount: -1}}}
}

func TestInboxLifecycleDoesNotInferCompletionFromProseOrOverrideRequests(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	row := inboxFixtureRow("env", "p", "t", "running")
	row.Thread.Inbox.LatestMessage = &wire.InboxMessage{Role: "assistant", Text: "Everything is done and merged."}
	if InboxBucket(row, now) != "working" {
		t.Fatal("prose claimed authoritative completion")
	}
	row.Thread.Inbox.Settled = true
	if InboxBucket(row, now) != "working" {
		t.Fatal("review marker hid active work")
	}
	row.Thread.Inbox.PendingRequestCount = 1
	row.Thread.Inbox.SnoozedUntil = now.Add(time.Hour).Format(time.RFC3339)
	if InboxBucket(row, now) != "needs" {
		t.Fatal("Later hid an exact native request")
	}
	row.Thread.Inbox.PendingRequestCount = 0
	if InboxBucket(row, now) != "working" {
		t.Fatal("native active work was hidden by an older snooze")
	}
	row.Thread.Status = "completed"
	if InboxBucket(row, now) != "later" {
		t.Fatal("native Later ignored")
	}
	row.Thread.Inbox.SnoozedUntil = now.Add(-time.Hour).Format(time.RFC3339)
	if InboxBucket(row, now) != "done" {
		t.Fatal("native settled state ignored")
	}
	row.Thread.Status = "failed"
	if InboxBucket(row, now) != "done" {
		t.Fatal("explicit native reviewed disposition was ignored")
	}
	row.Thread.Inbox.QueuedCount = 1
	if InboxBucket(row, now) != "queued" {
		t.Fatal("native queued work was hidden by an older review marker")
	}
	row.Thread.Inbox.QueuedCount = 0
	row.Thread.Inbox.Settled = false
	if InboxBucket(row, now) != "needs" {
		t.Fatal("completed work skipped human review")
	}
	row.Thread.Inbox = nil
	if InboxBucket(row, now) != "unknown" {
		t.Fatal("missing native projection became success")
	}
}

func TestInboxPresentationPriorityRetainsExactIdentityAndStableTies(t *testing.T) {
	rows := []InboxThread{inboxFixtureRow("b", "project", "same", "completed"), inboxFixtureRow("a", "project", "same", "completed"), inboxFixtureRow("a", "other", "new", "running")}
	prefs := config.InboxPreferences{Version: 1, Tasks: map[string]int{}, Projects: map[string]int{}}
	prefs.Projects[config.InboxIdentity("a", "project", "")] = 1
	prefs.Tasks[rows[0].Target.Key()] = 3
	InboxRank(rows, prefs)
	if rows[0].Target.EnvironmentID != "b" || rows[1].Target.ProjectID != "project" {
		t.Fatal("project/priority ranking changed exact sources")
	}
	if InboxPriority(prefs, rows[2].Target) != 1 {
		t.Fatal("untagged presentation priority is not Medium")
	}
	prefs.Tasks = map[string]int{}
	prefs.Projects = map[string]int{}
	InboxRank(rows, prefs)
	first := rows[0].Target.Key()
	InboxRank(rows, prefs)
	if rows[0].Target.Key() != first {
		t.Fatal("equal ranks reorder on refresh")
	}
}

func TestInboxOpenAndActionRejectWrongSourceBeforeIO(t *testing.T) {
	m, state, env := recoveryManager(t)
	s, err := m.NewSession(env.ID, "attached")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetDraft("exact prior draft"); err != nil {
		t.Fatal(err)
	}
	row := inboxFixtureRow(env.EnvironmentID, "project", "offered", "completed")
	row.Target.ProfileID = env.ID
	page := InboxPage{Rows: []InboxThread{row}}
	if _, err = s.InboxOpen(page, "unoffered"); err == nil {
		t.Fatal("unoffered row opened")
	}
	next, err := s.InboxOpen(page, row.Target.Key())
	if err != nil {
		t.Fatal(err)
	}
	if next.thread != "offered" || next.conn != nil || s.thread != "attached" {
		t.Fatal("metadata selection subscribed or retargeted current session")
	}
	if _, err = s.AgentBoxAction(context.Background(), row.Target, "settle", ""); err == nil {
		t.Fatal("unattached task accepted mutation")
	}
	if state.Draft(env.EnvironmentID, "attached") != "exact prior draft" {
		t.Fatal("inbox open lost exact draft")
	}
}

func TestInboxUnexpectedActionReceiptRetainsJournalBeforeAcknowledgement(t *testing.T) {
	for _, body := range []string{
		"out={'ok':True,'result':{'sequence':3,'threadId':'wrong','action':'pin'}}",
		"out={'ok':True,'result':{'sequence':3,'threadId':'thread','action':'unpin'}}",
		"out={'ok':True,'result':{'sequence':3}}",
	} {
		s, state, _ := recoverySession(t, body)
		s.features = map[string]bool{"nativeWorkflows": true}
		target := InboxTarget{ProfileID: "fixture", EnvironmentID: "fixture-env", ProjectID: "project", ThreadID: "thread", Revision: "reviewed-revision"}
		if _, err := s.AgentBoxAction(context.Background(), target, "pin", ""); err == nil {
			t.Fatal("unmatched native action acknowledgement became success")
		}
		pending := state.Pending("fixture-env", false)
		if pending == nil || pending["action"] != "pin" || pending["threadId"] != "thread" || pending["expectedRevision"] != target.Revision {
			t.Fatal("unexpected reply cleared or retargeted recovery journal")
		}
		if err := s.Stop(context.Background()); err != nil {
			t.Fatalf("bad normal receipt blocked independent Stop: %v", err)
		}
	}
}
