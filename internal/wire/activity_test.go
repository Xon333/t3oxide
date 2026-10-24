package wire

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func activityTestItem(id string) map[string]any {
	return map[string]any{"key": "thread/" + id, "sourceThreadId": "thread", "itemId": id, "runId": "",
		"kind": "command_execution", "title": "Run tests", "summary": "go test ./...", "status": "running",
		"updatedAt": "revision", "outputOmitted": true, "truncated": false, "position": 0}
}
func activityTestPage(items ...map[string]any) map[string]any {
	if items == nil {
		items = []map[string]any{}
	}
	return map[string]any{"scope": "page", "items": items, "total": len(items), "limited": false, "hasMore": false, "historyCursor": nil}
}
func activityTestDetail() map[string]any {
	return map[string]any{"key": "thread/tool", "sourceThreadId": "thread", "itemId": "tool", "kind": "command_execution",
		"title": "Run tests", "status": "completed", "input": "go test ./...", "output": "ok", "outputPresent": true,
		"outputOmitted": false, "updatedAt": "new", "requestedRevision": "old", "changed": true, "truncated": false}
}

func TestActivityMetadataIsAdditiveBoundedAndCloned(t *testing.T) {
	v := modelView(1, modelRow("one", "message"))
	v["activity"] = activityTestPage(activityTestItem("tool"))
	var store Store
	applyOK(t, &store, "view", v)
	copy := store.Snapshot()
	copy.Metadata.Activity.Items[0].Summary = "changed"
	if store.Snapshot().Metadata.Activity.Items[0].Summary != "go test ./..." {
		t.Fatal("activity aliases stored snapshot")
	}
	legacy := modelView(2, modelRow("one", "message"))
	applyOK(t, &store, "view", legacy)
	if store.Snapshot().Metadata.Activity != nil {
		t.Fatal("omitted activity retained stale metadata")
	}
}

func TestActivityDecoderRejectsWrongIdentityDuplicateAndUnboundedContent(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"wrong identity": func(p map[string]any) { p["items"].([]map[string]any)[0]["sourceThreadId"] = "other" },
		"duplicate": func(p map[string]any) {
			p["items"] = []map[string]any{activityTestItem("tool"), activityTestItem("tool")}
			p["total"] = 2
		},
		"long summary": func(p map[string]any) { p["items"].([]map[string]any)[0]["summary"] = strings.Repeat("x", 769) },
		"wrong count":  func(p map[string]any) { p["total"] = 2 },
		"empty cursor": func(p map[string]any) { p["hasMore"] = true; p["historyCursor"] = "" },
		"unknown type": func(p map[string]any) { p["items"].([]map[string]any)[0]["kind"] = "secret_request" },
	} {
		t.Run(name, func(t *testing.T) {
			p := activityTestPage(activityTestItem("tool"))
			mutate(p)
			if _, err := DecodeActivityPage(modelRaw(p)); err == nil {
				t.Fatal("invalid metadata accepted")
			}
		})
	}
}

func TestActivityListAndDetailKeepReadRevisionAndSourceExplicit(t *testing.T) {
	list, err := DecodeActivityList(modelRaw(map[string]any{"threadId": "thread", "observedAt": "now", "activity": activityTestPage(activityTestItem("tool"))}))
	if err != nil || list.ThreadID != "thread" || len(list.Activity.Items) != 1 {
		t.Fatalf("%+v %v", list, err)
	}
	detail, err := DecodeActivityDetail(modelRaw(activityTestDetail()))
	if err != nil || !detail.Changed || detail.RequestedRevision != "old" || detail.UpdatedAt != "new" {
		t.Fatalf("%+v %v", detail, err)
	}
	wrong := activityTestDetail()
	wrong["changed"] = false
	if _, err := DecodeActivityDetail(modelRaw(wrong)); err == nil {
		t.Fatal("new output mislabeled as requested historical revision")
	}
	wrong = activityTestDetail()
	wrong["outputPresent"] = false
	if _, err := DecodeActivityDetail(modelRaw(wrong)); err == nil {
		t.Fatal("nonempty output declared absent")
	}
}

func TestActivityValidationAcceptsGoMapEncodingWithoutLosingHTMLOrUnicode(t *testing.T) {
	detail := activityTestDetail()
	text := strings.Repeat("<\u2028>", 4000)
	detail["input"] = text
	detail["output"] = text
	raw, _ := json.Marshal(detail)
	if len(raw) <= MaxActivityDetailBytes {
		t.Fatal("fixture did not exercise local JSON expansion")
	}
	parsed, err := DecodeActivityDetail(raw)
	if err != nil || parsed.Input != text || parsed.Output != text {
		t.Fatalf("valid output changed or rejected: %v", err)
	}
	items := make([]map[string]any, 20)
	for i := range items {
		items[i] = activityTestItem(strings.Repeat("x", i+1))
		items[i]["summary"] = strings.Repeat("<\u2028>", 140)
	}
	page := activityTestPage(items...)
	raw, _ = json.Marshal(page)
	if _, err := DecodeActivityPage(raw); err != nil {
		t.Fatalf("valid summaries rejected after map encoding: %v", err)
	}
}

func TestActivityWireOperationsUseNormalReadLaneAndKeepExactIdentity(t *testing.T) {
	c := fixtureConnection(t, `for line in sys.stdin:
 p=json.loads(line)
 emit({'id':p['id'],'result':{'op':p['op'],'threadId':p.get('threadId'),'itemId':p.get('itemId')}})
`, Options{RequestTimeout: time.Second})
	for _, op := range []string{"activity", "inspect-item"} {
		reply, err := c.Request(context.Background(), op, map[string]any{"threadId": "source/parent", "itemId": "tool:1"}, "")
		if err != nil || reply["op"] != op || reply["threadId"] != "source/parent" || reply["itemId"] != "tool:1" {
			t.Fatalf("%s: %+v %v", op, reply, err)
		}
		if _, err := json.Marshal(reply); err != nil {
			t.Fatal(err)
		}
	}
}
