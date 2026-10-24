package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestInspectionCLIUsesRealAdapterWithoutSubscriptionOrMutation(t *testing.T) {
	f := newFixture(t)
	item := object{"id": "tool-a", "threadId": "thread-a", "type": "command_execution", "input": "printf fixture", "status": "completed", "updatedAt": "2026-10-24T10:00:00Z", "output": "fixture result\nsecond line"}
	visible := clone(item)
	delete(visible, "output")
	visible["outputOmitted"] = true
	f.mu.Lock()
	f.threads["thread-a"]["visibleTurnItems"] = append(f.threads["thread-a"]["visibleTurnItems"].([]any), object{"sourceThreadId": "thread-a", "sourceItemId": "tool-a", "position": 2, "visibility": "local", "item": visible})
	if f.activityItems == nil {
		f.activityItems = map[string]object{}
	}
	f.activityItems["thread-a/tool-a"] = item
	f.mu.Unlock()
	binary := builtExecutable(t)
	config, state := profileFile(t, f)
	runRead := func(args ...string) map[string]any {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		args = append(args, "--env", "fixture", "--config", config, "--state", state, "--json")
		output, err := exec.CommandContext(ctx, binary, args...).Output()
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, output)
		}
		var envelope map[string]any
		if json.Unmarshal(output, &envelope) != nil || envelope["ok"] != true {
			t.Fatalf("bad inspection JSON: %s", output)
		}
		if strings.Contains(string(output), "fixture-token-not-a-credential") {
			t.Fatal("credential in inspection output")
		}
		return envelope["data"].(map[string]any)
	}
	capabilities := runRead("capabilities")
	if capabilities["adapter"].(map[string]any)["features"].(map[string]any)["activityInspection"] != true {
		t.Fatal("missing activity capability")
	}
	page := runRead("activity", "--thread", "thread-a")
	items := page["activity"].(map[string]any)["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["itemId"] != "tool-a" {
		t.Fatalf("wrong activity page: %#v", page)
	}
	f.mu.Lock()
	beforeTickets := f.requests["/api/auth/websocket-ticket"]
	f.mu.Unlock()
	if beforeTickets != 0 {
		t.Fatal("metadata activity read opened a WebSocket")
	}
	detail := runRead("inspect", "--thread", "thread-a", "--item", "tool-a", "--revision", "earlier-revision")
	if detail["output"] != "fixture result\nsecond line" || detail["changed"] != true || detail["requestedRevision"] != "earlier-revision" {
		t.Fatalf("lost tool output or revision truth: %#v", detail)
	}
	f.mu.Lock()
	reads := f.requests["rpc:orchestration.getTurnItem"]
	subscriptions := f.requests["rpc:orchestration.subscribeThread"]
	f.mu.Unlock()
	if reads != 1 || subscriptions != 0 {
		t.Fatalf("unexpected detail reads/subscriptions: %d/%d", reads, subscriptions)
	}
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("inspection caused mutation")
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatal("inspection CLI opened composer/recovery state")
	}
}
