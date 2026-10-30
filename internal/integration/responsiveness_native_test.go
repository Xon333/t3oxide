package integration_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/wire"
)

func TestActualAdapterOneInitialProbeAndNoCrossSessionHandoff(t *testing.T) {
	f := newFixture(t)
	for cycle := 0; cycle < 3; cycle++ {
		before := f.requestCounts()
		c := f.start(t)
		request(t, c, "hello", nil, "")
		var store wire.Store
		store.Select("thread-a")
		request(t, c, "watch", object{"threadId": "thread-a"}, "")
		untilView(t, c, &store, func(s *wire.Snapshot) bool { return s.Metadata.Live })
		after := f.requestCounts()
		for _, route := range []string{"/.well-known/t3/environment", "/api/auth/websocket-ticket", "/ws", "/api/orchestration/threads/thread-a/bounded", "rpc:orchestration.subscribeThread"} {
			if after[route]-before[route] != 1 {
				t.Fatalf("fresh process %d route %s count %d, want one", cycle, route, after[route]-before[route])
			}
		}
		store.Select("thread-b")
		request(t, c, "watch", object{"threadId": "thread-b"}, "")
		untilView(t, c, &store, func(s *wire.Snapshot) bool { return s.Metadata.Live })
		switched := f.requestCounts()
		if switched["/.well-known/t3/environment"] != after["/.well-known/t3/environment"] || switched["/ws"] != after["/ws"] {
			t.Fatal("same-helper watch unexpectedly reprobed/reconnected")
		}
		t.Logf("process=%d descriptor=1 ticket=1 socket=1 bounded=2 subscriptions=2 effects=0", cycle)
		c.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		if err := c.WaitClosed(ctx); err != nil {
			t.Fatal(err)
		}
		cancel()
	}
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatalf("read path mutated: %d/%d", effects, dispatches)
	}
}

func TestActualAdapterFirstFrameBeforeSubscribeRetainsNativeReplay(t *testing.T) {
	f := newFixture(t)
	hold := make(chan struct{})
	released := false
	t.Cleanup(func() {
		if !released {
			close(hold)
		}
	})
	f.mu.Lock()
	f.subscriptionHold = hold
	f.mu.Unlock()
	c := f.start(t)
	request(t, c, "hello", nil, "")
	var store wire.Store
	store.Select("thread-a")
	request(t, c, "watch", object{"threadId": "thread-a"}, "")
	initial := untilView(t, c, &store, func(s *wire.Snapshot) bool { return len(s.Items) > 0 })
	if initial.Metadata.Live || initial.Metadata.State != "synchronizing" {
		t.Fatal("snapshot became authoritative before completion marker")
	}
	f.update("thread-a", "Authoritative event between bounded read and subscription", 0)
	close(hold)
	released = true
	updated := untilView(t, c, &store, func(s *wire.Snapshot) bool {
		return s.Metadata.Live && len(s.Items) == 1 && s.Items[0].Text == "Authoritative event between bounded read and subscription"
	})
	if updated.Metadata.Sequence == nil || initial.Metadata.Sequence == nil || *updated.Metadata.Sequence <= *initial.Metadata.Sequence || updated.Revision <= initial.Revision {
		t.Fatal("replay lost sequence/revision progress")
	}
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("replay read acquired mutation authority")
	}
}

func TestActualAdapterPreviewAndServerSearchDoNotAttachOrMutate(t *testing.T) {
	f := newFixture(t)
	f.addRequests()
	c := f.start(t)
	request(t, c, "hello", nil, "")
	before := f.requestCounts()
	preview := request(t, c, "thread-preview", object{"threadId": "thread-a"}, "")
	if preview["readOnly"] != true || preview["available"] != true {
		t.Fatalf("invalid preview flags: %#v", preview)
	}
	encoded, _ := json.Marshal(preview["view"])
	snapshot, err := wire.DecodeSnapshot(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Metadata.ThreadID != "thread-a" || snapshot.Metadata.Live || snapshot.Metadata.ActiveRunID != nil || len(snapshot.Metadata.Requests) != 0 || len(snapshot.Items) != 1 {
		t.Fatalf("preview acquired authority: %#v", snapshot)
	}
	after := f.requestCounts()
	if after["/ws"] != before["/ws"] || after["rpc:orchestration.subscribeThread"] != 0 || after["/api/orchestration/threads/thread-a/bounded"]-before["/api/orchestration/threads/thread-a/bounded"] != 1 {
		t.Fatalf("preview was not one-shot bounded HTTP: %#v", after)
	}
	local := request(t, c, "list", object{"query": "Markdown"}, "")
	if len(local["threads"].([]any)) != 0 {
		t.Fatal("fixture title unexpectedly matched message query")
	}
	search := request(t, c, "list", object{"query": "Markdown", "serverSearch": true, "limit": 50}, "")
	page, err := client.DecodeThreadPage(search)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Threads) != 2 || !page.SearchIncomplete || page.SearchScope == "" {
		t.Fatalf("search lost results/completeness: %#v", page)
	}
	counts := f.requestCounts()
	if counts["rpc:orchestration.searchThreads"] != 1 || counts["rpc:orchestration.subscribeThread"] != 0 || counts["/api/orchestration/threads/thread-b/bounded"] != 0 {
		t.Fatalf("search fetched transcripts/subscribed: %#v", counts)
	}
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatalf("read path mutated: %d/%d", effects, dispatches)
	}
}

func TestActualAdapterNativeDefaultsReadShowsScopesAndPreciseWriteGate(t *testing.T) {
	f := newFixture(t)
	c := f.start(t)
	hello := request(t, c, "hello", nil, "")
	features := hello["features"].(object)
	if features["nativeDefaults"] != true || features["nativeDefaultsWrite"] != false {
		t.Fatalf("wrong settings capability: %#v", features)
	}
	for _, scope := range []string{"project", "environment"} {
		report := request(t, c, "native-defaults", object{"projectId": "project-a", "scope": scope}, "")
		if report["scope"] != scope || report["projectId"] != "project-a" || report["writeSupported"] != false || !strings.Contains(report["writeReason"].(string), "revision/CAS") || len(report["fields"].([]any)) != 3 {
			t.Fatalf("incomplete scoped defaults: %#v", report)
		}
		if report["desktopLocal"] != "unreported" {
			t.Fatal("invented desktop-local defaults")
		}
	}
	counts := f.requestCounts()
	if counts["rpc:server.updateSettings"] != 0 || counts["rpc:orchestration.subscribeThread"] != 0 {
		t.Fatalf("defaults read wrote/subscribed: %#v", counts)
	}
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatalf("settings read mutated: %d/%d", effects, dispatches)
	}
}

func TestActualAdapterCreateAndSendPreservesWhitespaceFirstMessageExactly(t *testing.T) {
	for _, tc := range []struct{ name, message string }{{"empty", ""}, {"spaces", " \t "}, {"newlines", "\r\n\t\n"}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			_, state, a, stream := startFeatureSession(t, f, "thread-a")
			defer stream.stop(t)
			const draft = " original source draft\r\n"
			if err := a.SetDraft(draft); err != nil {
				t.Fatal(err)
			}
			out := featureCommand(a, "new", "")
			p := nativeLaunchReview(t, a)
			p.Respond(client.Answer{Action: "create", Launch: &client.LaunchInput{Title: "Exact filing " + tc.name, Message: tc.message}})
			result := featureReply(t, out)
			if result.err != nil || result.result.Next == nil {
				t.Fatalf("Create failed: %+v %v", result.result, result.err)
			}
			defer result.result.Next.Close()
			f.mu.Lock()
			var payload object
			for _, command := range f.commands {
				if command["threadId"] == result.result.Next.Status().ThreadID {
					payload = clone(command)
				}
			}
			f.mu.Unlock()
			if payload == nil {
				t.Fatal("missing exact native launch")
			}
			if tc.message == "" {
				if payload["initialMessage"] != nil {
					t.Fatal("empty Create submitted an initial message")
				}
			} else {
				initial, ok := payload["initialMessage"].(object)
				if !ok || initial["text"] != tc.message {
					t.Fatalf("first message bytes changed: %#v", initial)
				}
			}
			if effects, dispatches := f.counts(); effects != 1 || dispatches != 1 {
				t.Fatalf("launch must dispatch exactly once: %d/%d", effects, dispatches)
			}
			if counts := f.requestCounts(); counts["rpc:orchestration.dispatchCommand"] != 0 || counts["rpc:orchestration.launchThread"] != 1 {
				t.Fatalf("separate send/duplicate native launch: %#v", counts)
			}
			if state.Draft("env-a", "thread-a") != draft {
				t.Fatal("source draft changed")
			}
		})
	}
}
