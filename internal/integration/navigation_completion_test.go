package integration_test

import (
	"context"
	"github.com/Xon333/t3oxide/internal/client"
	"strings"
	"testing"
	"time"
)

func TestNavigationOpenClearsNativeCompletionOnlyOnce(t *testing.T) {
	f := newFixture(t)
	f.finishNativeThread("thread-a", "Synthetic finished result")
	manager, state := newAppManager(t, f)
	a, err := manager.NewSession(f.profile.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	row := nativeInboxThread(t, a, "thread-a")
	page := client.NavigatorPage{ProfileID: f.profile.ID, EnvironmentID: f.environmentID, Page: client.ThreadPage{Threads: []client.Thread{row.Thread}}}
	next, err := a.NavigatorOpenReviewed(ctx, page, "thread-a")
	if err != nil || next == nil {
		t.Fatalf("open completion: %v", err)
	}
	waitCommandType(t, f, "thread.settle")
	fresh := nativeInboxThread(t, a, "thread-a")
	if !fresh.Thread.Inbox.Settled || state.Pending(f.environmentID, false) != nil {
		t.Fatal("native completion not acknowledged durably")
	}
	if a.Status().ThreadID != "" {
		t.Fatal("review changed source attachment before UI adopts target")
	}
	page.Page.Threads = []client.Thread{fresh.Thread}
	if _, err = a.NavigatorOpenReviewed(ctx, page, "thread-a"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, command := range f.commands {
		if command["type"] == "thread.settle" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("reopening reviewed thread sent %d settlements", count)
	}
}

func TestProgramFinishedOpenClearsAndHoldsUntilNextSelection(t *testing.T) {
	f := newFixture(t)
	f.finishNativeThread("thread-a", "Synthetic finished Alpha")
	f.finishNativeThread("thread-b", "Synthetic finished Beta")
	manager, _ := newAppManager(t, f)
	a, err := manager.NewSession(f.profile.ID, "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	r, _ := startAppTUI(t, a)
	r.waitText("Synthetic finished Alpha")
	r.send("/mode agentbox\x1d")
	waitWorkspaceFrame(t, r, func(v string) bool {
		return strings.Contains(v, "AgentBox") && strings.Contains(v, "Alpha") && strings.Contains(v, "Beta") && !strings.Contains(v, "Loading…")
	}, "finished native rows")
	r.send("\r")
	waitCommandType(t, f, "thread.settle")
	waitWorkspaceFrame(t, r, func(v string) bool { return strings.Contains(v, "Alpha") && strings.Contains(v, "1 Changes") }, "held completed row")
	r.send("j")
	waitWorkspaceFrame(t, r, func(v string) bool { return strings.Contains(v, "Beta") && !strings.Contains(v, "Loading…") }, "next native selection")
	f.mu.Lock()
	count := 0
	for _, command := range f.commands {
		if command["type"] == "thread.settle" {
			count++
		}
	}
	f.mu.Unlock()
	if count != 1 {
		t.Fatalf("selection alone settled another thread: %d", count)
	}
	quitIntuitiveProgram(t, r)
}
