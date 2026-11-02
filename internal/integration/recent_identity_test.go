package integration_test

import (
	"context"
	"fmt"
	"github.com/Xon333/t3oxide/internal/client"
	"testing"
	"time"
)

func TestRecentExactMetadataIgnoresTitleQueryAndBrowsePage(t *testing.T) {
	f := newFixture(t)
	f.mu.Lock()
	for i := 0; i < 205; i++ {
		id := fmt.Sprintf("row-%03d", i)
		f.threads[id] = projection(id, "Unrelated title")
	}
	f.mu.Unlock()
	manager, _ := newAppManager(t, f)
	session, err := manager.NewSession(f.profile.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	offered := client.NavigatorPage{ProfileID: f.profile.ID, Page: client.ThreadPage{NextCursor: new(24)}}
	if err = session.NavigatorChange(offered, "next", ""); err != nil {
		t.Fatal(err)
	}
	if err = session.NavigatorChange(offered, "failed", ""); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	page, err := session.NavigatorRecentRead(ctx, f.profile.ID, "thread-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Page.Threads) != 1 || page.Page.Threads[0].ID != "thread-b" || page.Page.Threads[0].Title != "Beta" || page.EnvironmentID != f.environmentID {
		t.Fatalf("wrong exact source: %+v", page)
	}
	if page.Page.Cursor < 200 {
		t.Fatal("fixture failed to exercise continuation")
	}
	stillScoped, err := session.NavigatorRead(ctx, f.profile.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if stillScoped.Page.Lifecycle != "failed" {
		t.Fatalf("recent lookup changed browse lifecycle: %+v", stillScoped.Page)
	}
	if session.Status().ThreadID != "" || f.requestCounts()["rpc:orchestration.subscribeThread"] != 0 {
		t.Fatal("identity read attached detail")
	}
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("identity read mutated native work")
	}
}
