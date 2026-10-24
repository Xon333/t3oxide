package client

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Xon333/t3oxide/internal/wire"
)

func activityClientItem(id, revision string) wire.ActivityItem {
	return wire.ActivityItem{Key: "thread/" + id, SourceThreadID: "thread", ItemID: id, Kind: "command_execution",
		Title: "Run tests", Summary: "go test ./...", Status: "running", UpdatedAt: revision, OutputOmitted: true}
}
func activityClientPage(t *testing.T, a *Session, revision int, historical bool, items ...wire.ActivityItem) {
	t.Helper()
	var view map[string]any
	if err := json.Unmarshal(recoveryView(revision, historical, nil), &view); err != nil {
		t.Fatal(err)
	}
	if items == nil {
		items = []wire.ActivityItem{}
	}
	scope := "page"
	if historical {
		scope = "history"
	}
	view["activity"] = wire.ActivityPage{Scope: scope, Items: items, Total: len(items)}
	raw, _ := json.Marshal(view)
	if err := a.applyPacket(wire.Packet{Event: "view", Data: raw}); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.features = map[string]bool{"activity": true, "activityInspection": true}
	a.mu.Unlock()
}

const activityReadReply = `out={'result':{'key':p['threadId']+'/'+p['itemId'],'sourceThreadId':p['threadId'],'itemId':p['itemId'],'kind':'command_execution','title':'Run tests','status':'completed','input':'go test ./...','output':'ok '+p.get('revision',''),'outputPresent':True,'outputOmitted':False,'updatedAt':p.get('revision',''),'requestedRevision':p.get('revision',''),'changed':False,'truncated':False}}`

func TestActivityInspectionPreservesDraftAndCachesExactSourceRevision(t *testing.T) {
	a, state, path := recoverySession(t, activityReadReply)
	activityClientPage(t, a, 2, false, activityClientItem("tool", "v1"))
	if err := a.SetDraft("exact \tdraft\r\n"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		r, err := a.InspectActivity(context.Background(), "thread/tool")
		if err != nil || !strings.Contains(r.Detail, "ok v1") {
			t.Fatalf("%+v %v", r, err)
		}
	}
	if len(requestsFromLog(t, path)) != 1 || a.Draft() != "exact \tdraft\r\n" || state.Pending("fixture-env", false) != nil {
		t.Fatal("inspection changed draft, journal or repeated a cached read")
	}
	activityClientPage(t, a, 3, false, activityClientItem("tool", "v2"))
	if _, err := a.InspectActivity(context.Background(), "thread/tool"); err != nil {
		t.Fatal(err)
	}
	if len(requestsFromLog(t, path)) != 2 {
		t.Fatal("updated item reused older output")
	}
}

func TestActivityDetailCacheHasHardEntryAndByteBounds(t *testing.T) {
	largeReply := strings.ReplaceAll(activityReadReply, "'input':'go test ./...'", "'input':'x'*24000")
	largeReply = strings.ReplaceAll(largeReply, "'output':'ok '+p.get('revision','')", "'output':'y'*24000")
	a, _, _ := recoverySession(t, largeReply)
	for i := 0; i < 12; i++ {
		id := strings.Repeat("x", i+1)
		activityClientPage(t, a, i+2, false, activityClientItem(id, "v1"))
		if _, err := a.InspectActivity(context.Background(), "thread/"+id); err != nil {
			t.Fatal(err)
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.activityCache) > activityCacheEntries || a.activityCacheSize > activityCacheBytes {
		t.Fatal("unbounded detail cache")
	}
	if len(a.activityCache) >= 12 {
		t.Fatal("old details were never evicted")
	}
}

func TestActivityInspectionRejectsUnadvertisedAndUnseenTargets(t *testing.T) {
	a, _, path := recoverySession(t, activityReadReply)
	activityClientPage(t, a, 2, false, activityClientItem("tool", "v1"))
	if _, err := a.InspectActivity(context.Background(), "other/tool"); err == nil {
		t.Fatal("invented target accepted")
	}
	a.mu.Lock()
	a.features = map[string]bool{}
	a.mu.Unlock()
	if _, err := a.InspectActivity(context.Background(), "thread/tool"); err == nil {
		t.Fatal("unadvertised helper extension used")
	}
	if len(requestsFromLog(t, path)) != 0 {
		t.Fatal("unadvertised/unseen target reached helper")
	}
}

func TestEmptyHistoricalActivityOffersLatestExplicitlyWithoutReadingIt(t *testing.T) {
	a, _, path := recoverySession(t, activityReadReply)
	activityClientPage(t, a, 2, true)
	a.asker = recoveryAsker(func(_ context.Context, q Question) (Answer, error) {
		if len(q.Choices) != 1 || q.Choices[0].ID != "\x00latest" {
			t.Fatalf("historical empty page lost explicit latest choice: %+v", q)
		}
		return Answer{Cancelled: true}, nil
	})
	if _, err := a.Command(context.Background(), "activity", ""); err != nil {
		t.Fatal(err)
	}
	if len(requestsFromLog(t, path)) != 0 || !a.snapshot().Metadata.Historical {
		t.Fatal("activity silently crossed to latest")
	}
}

func TestActivityMarkdownFenceCannotBeClosedByToolContent(t *testing.T) {
	result := activityResult(Location{Environment: "Fixture", Title: "Thread", ThreadID: "thread"}, wire.ActivityDetail{
		SourceThreadID: "thread", Title: "Tool", Status: "completed", OutputPresent: true, Output: "```\n# not a heading\n```"})
	if !strings.Contains(result.Detail, "````text\n```\n# not a heading\n```\n````") {
		t.Fatal(result.Detail)
	}
}
