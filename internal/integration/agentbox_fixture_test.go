package integration_test

// Native-contract fixture only. Production Session, Bubble Tea and the actual
// Node adapter remain unchanged. Paths and identities are synthetic; there are
// no provider processes, host installations or scheduler substitutes here.

import (
	"strings"
	"sync"
	"time"
)

func (f *fixture) holdNativeShell() func() {
	hold := make(chan struct{})
	f.mu.Lock()
	f.shellHold = hold
	f.mu.Unlock()
	var once sync.Once
	release := func() { once.Do(func() { close(hold) }) }
	f.t.Cleanup(release)
	return release
}

func (f *fixture) enableNativeContract() {
	f.nativeContract = true
	if f.projects == nil {
		f.projects = []any{object{"id": "project-a", "title": "Fixture project", "workspaceRoot": "/fixture/project-root"}}
	}
}

func (f *fixture) nativeTimestamp() string {
	return time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC).Add(time.Duration(f.sequence) * time.Second).Format(time.RFC3339Nano)
}

func nativeFixtureShellThread(p object) object {
	t := clone(p["thread"].(object))
	runs, _ := p["runs"].([]any)
	t["queuedRunCount"] = 0
	t["status"] = "idle"
	queued := 0
	for _, v := range runs {
		run := v.(object)
		t["latestRunId"] = run["id"]
		switch run["status"] {
		case "preparing", "starting", "running", "waiting":
			t["activeRunId"], t["status"] = run["id"], run["status"]
		case "queued":
			queued++
			if t["activeRunId"] == nil {
				t["status"] = "queued"
			}
		default:
			if t["activeRunId"] == nil && queued == 0 {
				t["status"] = run["status"]
			}
		}
	}
	t["queuedRunCount"] = queued
	// Existing contract fixtures can explicitly model a shell lifecycle that
	// differs from the detailed projection (for example a waiting child).
	if status, exists := p["thread"].(object)["status"]; exists {
		t["status"] = status
	}
	for _, value := range p["runtimeRequests"].([]any) {
		request := value.(object)
		if request["status"] == "pending" {
			t["pendingRuntimeRequest"] = object{"id": request["id"], "kind": request["kind"]}
			break
		}
	}
	items, _ := p["visibleTurnItems"].([]any)
	for i := len(items) - 1; i >= 0; i-- {
		value, _ := items[i].(object)
		item, _ := value["item"].(object)
		kind, _ := item["type"].(string)
		if kind == "assistant_message" || kind == "user_message" {
			t["latestVisibleMessage"] = object{"role": strings.TrimSuffix(kind, "_message"), "text": item["text"], "updatedAt": t["updatedAt"]}
			break
		}
	}
	return t
}

func (f *fixture) nativeRPC(s *fixtureSocket, frame object) bool {
	tag, _ := frame["tag"].(string)
	if tag == "orchestration.searchThreads" {
		payload, _ := frame["payload"].(object)
		query, _ := payload["query"].(string)
		limit, _ := payload["limit"].(float64)
		f.mu.Lock()
		if len(query) < 2 || len(query) > 200 || limit < 1 || limit > 50 {
			f.violations = append(f.violations, "native search exceeded its query/result bounds")
		}
		matches := []any{}
		for id, p := range f.threads {
			items, _ := p["visibleTurnItems"].([]any)
			for _, value := range items {
				item := value.(object)["item"].(object)
				text, _ := item["text"].(string)
				if strings.Contains(strings.ToLower(text), strings.ToLower(query)) {
					matches = append(matches, object{"threadId": id, "projectId": p["thread"].(object)["projectId"], "source": "assistant", "snippet": "Synthetic matching message", "messageCreatedAt": nil})
					break
				}
			}
			if len(matches) >= int(limit) {
				break
			}
		}
		f.mu.Unlock()
		_ = s.send(object{"_tag": "Exit", "requestId": frame["id"], "exit": object{"_tag": "Success", "value": object{"matches": matches}}})
		return true
	}
	if tag != "vcs.listRefs" && tag != "review.getDiffPreview" {
		return false
	}
	payload, _ := frame["payload"].(object)
	cwd, _ := payload["cwd"].(string)
	f.mu.Lock()
	valid := false
	for _, p := range f.projects {
		if p.(object)["workspaceRoot"] == cwd {
			valid = true
		}
	}
	for _, p := range f.threads {
		if p["thread"].(object)["worktreePath"] == cwd {
			valid = true
		}
	}
	if !valid {
		f.violations = append(f.violations, "native review/refs read an unbound workspace: "+cwd)
	}
	f.mu.Unlock()
	var result object
	if tag == "vcs.listRefs" {
		result = object{"isRepo": true, "refs": []any{object{"name": "main", "current": true, "isDefault": true, "isRemote": false}}}
	} else {
		result = object{"cwd": cwd, "generatedAt": "2026-10-29T12:00:00Z", "sources": []any{object{"kind": "working-tree", "title": "Uncommitted changes", "diff": "diff --git a/export.go b/export.go\n--- a/export.go\n+++ b/export.go\n@@ -1 +1 @@\n-old destination\n+selected destination\n", "diffHash": "fixture-diff-hash", "files": []any{object{"path": "export.go", "additions": 1, "deletions": 1}}}}}
	}
	_ = s.send(object{"_tag": "Exit", "requestId": frame["id"], "exit": object{"_tag": "Success", "value": result}})
	return true
}

// Called under fixture.mu. The two UUIDs have distinct native roles; an
// operation receipt must return the requested thread ID, never the command ID.
func (f *fixture) nativeLaunch(payload object) string {
	thread, _ := payload["threadId"].(string)
	if thread == "" {
		thread, _ = payload["commandId"].(string)
	}
	p := projection(thread, payload["title"].(string))
	t := p["thread"].(object)
	for _, field := range []string{"projectId", "modelSelection", "runtimeMode", "interactionMode"} {
		if value := payload[field]; value != nil {
			t[field] = clone(value)
		}
	}
	strategy, _ := payload["workspaceStrategy"].(object)
	switch strategy["type"] {
	case "worktree":
		t["worktreePath"] = "/fixture/worktrees/" + thread
		t["branch"] = "task/" + thread
	case "root":
		t["worktreePath"] = nil
		t["branch"] = "main"
	case "existing_worktree":
		t["worktreePath"] = strategy["worktreePath"]
	}
	t["updatedAt"] = f.nativeTimestamp()
	initial, hasMessage := payload["initialMessage"].(object)
	if hasMessage {
		p["runs"] = []any{object{"id": "run-" + thread, "status": "running", "userMessageId": initial["messageId"]}}
		p["visibleTurnItems"] = append([]any{object{"sourceThreadId": thread, "sourceItemId": initial["messageId"], "visibility": "local", "item": object{"id": initial["messageId"], "threadId": thread, "type": "user_message", "messageId": initial["messageId"], "text": initial["text"]}}}, p["visibleTurnItems"].([]any)...)
	} else {
		p["runs"], p["visibleTurnItems"] = []any{}, []any{}
	}
	f.threads[thread] = p
	return thread
}

func (f *fixture) nativeThreadAction(p, command object) bool {
	if p == nil {
		return false
	}
	t := p["thread"].(object)
	switch command["type"] {
	case "thread.settle":
		t["settledOverride"] = "settled"
	case "thread.unsettle":
		t["settledOverride"] = "unsettled"
	case "thread.snooze":
		t["snoozedUntil"] = command["snoozedUntil"]
	case "thread.unsnooze":
		t["snoozedUntil"] = nil
	case "thread.pin":
		t["pinnedAt"] = f.nativeTimestamp()
	case "thread.unpin":
		t["pinnedAt"] = nil
	default:
		return false
	}
	return true
}

func (f *fixture) finishNativeThread(thread, outcome string) {
	f.mu.Lock()
	p := f.threads[thread]
	for _, value := range p["runs"].([]any) {
		value.(object)["status"] = "completed"
	}
	f.sequence++
	p["thread"].(object)["updatedAt"] = f.nativeTimestamp()
	if outcome != "" {
		p["visibleTurnItems"] = append(p["visibleTurnItems"].([]any), row(thread, "outcome-"+thread, outcome))
	}
	sequence := f.sequence
	f.mu.Unlock()
	f.broadcast(thread, "run.updated", object{"id": "run-" + thread, "threadId": thread, "status": "completed"}, sequence)
}
