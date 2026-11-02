package client

import (
	"context"
	"github.com/Xon333/t3oxide/internal/wire"
	"testing"
)

func TestOpenBusyThreadNeverSettlesOrAcquiresConnection(t *testing.T) {
	manager, _, env := recoveryManager(t)
	session, err := manager.NewSession(env.ID, "attached")
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"running", "waiting", "waiting_for_input", "queued", "pending", "preparing", "starting"} {
		row := Thread{ID: "offered", Status: status, Inbox: &wire.InboxMetadata{NativeRevision: "revision"}}
		page := NavigatorPage{ProfileID: env.ID, EnvironmentID: env.EnvironmentID, Page: ThreadPage{Threads: []Thread{row}}}
		next, err := session.NavigatorOpenReviewed(context.Background(), page, row.ID)
		if err != nil || next == nil || session.conn != nil || manager.State.Pending(env.EnvironmentID, false) != nil {
			t.Fatalf("%s attempted settlement: %v", status, err)
		}
	}
	for _, v := range []*wire.InboxMetadata{{NativeRevision: "r", PendingRequestCount: 1}, {NativeRevision: "r", PendingChildRequest: true}, {NativeRevision: "r", QueuedCount: 1}, {NativeRevision: "r", Working: new(true)}, {NativeRevision: "r", Settled: true}} {
		if CompletionReviewable(Thread{Status: "completed", Inbox: v}) {
			t.Fatal("busy/reviewed completion admitted")
		}
	}
	if !CompletionReviewable(Thread{Status: "completed", Inbox: &wire.InboxMetadata{NativeRevision: "r"}}) {
		t.Fatal("finished native work cannot clear on open")
	}
}
