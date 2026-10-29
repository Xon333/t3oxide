package integration_test

// This is a simulated T3 server, not a simulated companion adapter. Every
// integration test launches the repository's Node adapter and uses
// its real HTTP, WebSocket/Effect RPC and NDJSON code. The deliberately small
// RFC 6455 implementation keeps test-only dependencies out of the client.

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
)

type object = map[string]any

type fixture struct {
	t               *testing.T
	mu              sync.Mutex
	server          *httptest.Server
	profile         config.Environment
	environmentID   string
	sequence        int64
	threads         map[string]object
	sockets         map[*fixtureSocket]struct{}
	receipts        map[string]object
	commands        map[string]object
	dispatches      []string
	effects         int
	dropAck         map[string]bool
	dropMessages    bool
	dropSelections  bool
	catalog         object
	catalogReads    int
	violations      []string
	requests        map[string]int
	nextSendAck     *fixtureAckHold
	nextLaunchAck   *fixtureAckHold
	activityItems   map[string]object
	activityHistory map[string][]any
	nativeContract  bool
	projects        []any
	shellHold       <-chan struct{}
}

type fixtureAckHold struct {
	started chan string
	release chan struct{}
}

func clone[T any](value T) T {
	data, _ := json.Marshal(value)
	var result T
	_ = json.Unmarshal(data, &result)
	return result
}

func row(thread, id, text string) object {
	return object{"sourceThreadId": thread, "sourceItemId": id, "visibility": "local",
		"item": object{"id": id, "threadId": thread, "type": "assistant_message",
			"text": text, "messageId": "message-" + id, "streaming": false}}
}

func projection(thread, title string) object {
	return object{
		"thread": object{"id": thread, "title": title, "projectId": "project-a",
			"modelSelection": object{"instanceId": "fixture-provider", "model": "fixture-model"},
			"lineage":        object{"relationshipToParent": nil, "parentThreadId": nil, "rootThreadId": thread},
			"runtimeMode":    "approval-required", "interactionMode": "default",
			"worktreePath": "/fixture/existing-worktree", "branch": "fixture-branch", "updatedAt": "2026-01-01T00:00:00Z"},
		"runs":             []any{object{"id": "run-" + thread, "status": "running"}},
		"visibleTurnItems": []any{row(thread, "first-"+thread, "Initial **Markdown** for "+title)},
		"turnItems":        []any{}, "runtimeRequests": []any{},
	}
}

func newFixture(t *testing.T) *fixture {
	return newEnvironmentFixture(t, "env-a", "fixture", "Fixture")
}

func newEnvironmentFixture(t *testing.T, environmentID, profileID, label string) *fixture {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("integration tests need Node.js 22.4+ to launch the actual adapter")
	}
	f := &fixture{t: t, environmentID: environmentID, sequence: 10, threads: map[string]object{
		"thread-a": projection("thread-a", "Alpha"), "thread-b": projection("thread-b", "Beta")},
		sockets: map[*fixtureSocket]struct{}{}, receipts: map[string]object{}, commands: map[string]object{}, dropAck: map[string]bool{}, catalog: fixtureCatalog(), requests: map[string]int{}}
	f.enableNativeContract()
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	dir := t.TempDir()
	token := filepath.Join(dir, "token")
	if err := os.WriteFile(token, []byte("fixture-token-not-a-credential"), 0600); err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(dir, "host.json")
	encoded, _ := json.Marshal(object{"baseUrl": f.server.URL, "environmentId": environmentID, "tokenFile": token, "allowWrites": true})
	if err := os.WriteFile(host, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	f.profile = config.Environment{ID: profileID, Label: label, EnvironmentID: environmentID, Node: node,
		Adapter: filepath.Join(root, "adapter", "main.mjs"), HostConfig: host, Enabled: true}
	t.Cleanup(func() {
		f.closeSockets()
		f.server.Close()
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, violation := range f.violations {
			t.Error(violation)
		}
	})
	return f
}

func (f *fixture) violation(format string, args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.violations = append(f.violations, fmt.Sprintf(format, args...))
}

func jsonResponse(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func (f *fixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests[r.URL.Path]++
	f.mu.Unlock()
	if r.URL.Path == "/.well-known/t3/environment" {
		jsonResponse(w, object{"environmentId": f.environmentID, "orchestrationProtocolVersion": 2,
			"serverVersion": "0.0.0-go-fixture", "capabilities": object{"serverResolvedCommandContext": true}})
		return
	}
	if r.URL.Path == "/ws" {
		f.websocket(w, r)
		return
	}
	if r.Header.Get("Authorization") != "Bearer fixture-token-not-a-credential" || r.Header.Get("x-t3-orchestration-protocol") != "2" {
		f.violation("missing adapter authentication/protocol headers for %s", r.URL.Path)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.URL.Path == "/api/auth/websocket-ticket" {
		jsonResponse(w, object{"ticket": "fixture-ticket"})
		return
	}
	if r.URL.Path == "/api/orchestration/shell" {
		f.mu.Lock()
		hold := f.shellHold
		f.mu.Unlock()
		if hold != nil {
			select {
			case <-hold:
			case <-r.Context().Done():
				return
			}
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path == "/api/orchestration/shell" {
		threads := []any{}
		for _, p := range f.threads {
			item := clone(p["thread"].(object))
			if f.nativeContract {
				item = nativeFixtureShellThread(p)
			}
			if _, ok := item["status"]; !ok {
				item["status"] = "running"
			}
			threads = append(threads, item)
		}
		jsonResponse(w, object{"threads": threads, "projects": clone(f.projects)})
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) == 5 && parts[0] == "api" && parts[1] == "orchestration" && parts[2] == "threads" {
		thread := parts[3]
		p := f.threads[thread]
		if p == nil {
			http.NotFound(w, r)
			return
		}
		switch parts[4] {
		case "bounded":
			jsonResponse(w, object{"snapshotSequence": f.sequence, "projection": clone(p),
				"historyCursor": "opaque-" + thread, "hasMoreHistory": true})
			return
		case "history":
			if r.URL.Query().Get("cursor") != "opaque-"+thread || r.URL.Query().Get("view") != "conversation" && r.URL.Query().Get("view") != "activity" {
				f.violations = append(f.violations, "history cursor must remain opaque and unchanged")
				http.Error(w, "Bad cursor", 400)
				return
			}
			if r.URL.Query().Get("view") == "activity" {
				items := f.activityHistory[thread]
				if items == nil {
					items = []any{}
				}
				jsonResponse(w, object{"items": clone(items), "snapshotSequence": f.sequence, "hasMoreHistory": false, "nextCursor": nil})
				return
			}
			jsonResponse(w, object{"items": []any{row(thread, "old-"+thread, "Earlier page for "+thread)},
				"snapshotSequence": f.sequence, "hasMoreHistory": false, "nextCursor": nil})
			return
		}
	}
	http.NotFound(w, r)
}

type fixtureSocket struct {
	conn         net.Conn
	reader       *bufio.Reader
	mu           sync.Mutex
	subscription string // guarded by fixture.mu
	thread       string // guarded by fixture.mu
}

func (s *fixtureSocket) sendRaw(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	header := []byte{0x81}
	switch {
	case len(data) < 126:
		header = append(header, byte(len(data)))
	case len(data) < 65536:
		header = append(header, 126, byte(len(data)>>8), byte(len(data)))
	default:
		header = append(header, 127)
		header = binary.BigEndian.AppendUint64(header, uint64(len(data)))
	}
	if _, err := s.conn.Write(append(header, data...)); err != nil {
		return err
	}
	return nil
}

func (s *fixtureSocket) send(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return s.sendRaw(data)
}

func (s *fixtureSocket) read() ([]byte, error) {
	var h [2]byte
	if _, err := io.ReadFull(s.reader, h[:]); err != nil {
		return nil, err
	}
	if h[0]&0xf == 8 {
		return nil, io.EOF
	}
	if h[0] != 0x81 || h[1]&0x80 == 0 {
		return nil, fmt.Errorf("unexpected fixture websocket frame %x", h)
	}
	n := uint64(h[1] & 0x7f)
	if n == 126 {
		var b [2]byte
		if _, err := io.ReadFull(s.reader, b[:]); err != nil {
			return nil, err
		}
		n = uint64(binary.BigEndian.Uint16(b[:]))
	} else if n == 127 {
		var b [8]byte
		if _, err := io.ReadFull(s.reader, b[:]); err != nil {
			return nil, err
		}
		n = binary.BigEndian.Uint64(b[:])
	}
	if n > 1024*1024 {
		return nil, fmt.Errorf("fixture frame too large: %d", n)
	}
	var mask [4]byte
	if _, err := io.ReadFull(s.reader, mask[:]); err != nil {
		return nil, err
	}
	data := make([]byte, int(n))
	if _, err := io.ReadFull(s.reader, data); err != nil {
		return nil, err
	}
	for i := range data {
		data[i] ^= mask[i%4]
	}
	return data, nil
}

func (f *fixture) websocket(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("wsTicket") != "fixture-ticket" || r.URL.Query().Get("orchestrationProtocol") != "2" {
		f.violation("adapter websocket ticket or protocol missing")
		http.Error(w, "Bad ticket", 400)
		return
	}
	conn, rw, err := w.(http.Hijacker).Hijack()
	if err != nil {
		f.violation("hijack: %v", err)
		return
	}
	hash := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	_, _ = fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", base64.StdEncoding.EncodeToString(hash[:]))
	if err := rw.Flush(); err != nil {
		_ = conn.Close()
		return
	}
	s := &fixtureSocket{conn: conn, reader: rw.Reader}
	f.mu.Lock()
	f.sockets[s] = struct{}{}
	f.mu.Unlock()
	defer func() { _ = conn.Close(); f.mu.Lock(); delete(f.sockets, s); f.mu.Unlock() }()
	for {
		data, err := s.read()
		if err != nil {
			return
		}
		var frame object
		if err := json.Unmarshal(data, &frame); err != nil {
			f.violation("adapter sent invalid RPC JSON: %v", err)
			return
		}
		switch frame["_tag"] {
		case "Ping":
			_ = s.send(object{"_tag": "Pong"})
		case "Ack":
		case "Interrupt":
			f.mu.Lock()
			if frame["requestId"] == s.subscription {
				s.subscription = ""
			}
			f.mu.Unlock()
		case "Request":
			f.rpc(s, frame)
		default:
			f.violation("unexpected RPC tag: %v", frame["_tag"])
		}
	}
}

func (f *fixture) rpc(s *fixtureSocket, frame object) {
	payload, _ := frame["payload"].(object)
	rid, _ := frame["id"].(string)
	tag, _ := frame["tag"].(string)
	f.mu.Lock()
	f.requests["rpc:"+tag]++
	f.mu.Unlock()
	if f.nativeContract && f.nativeRPC(s, frame) {
		return
	}
	if frame["tag"] == "server.getConfig" {
		f.mu.Lock()
		f.catalogReads++
		config := clone(f.catalog)
		f.mu.Unlock()
		_ = s.send(object{"_tag": "Exit", "requestId": rid, "exit": object{"_tag": "Success", "value": config}})
		return
	}
	if frame["tag"] == "orchestration.getTurnItem" {
		thread, _ := payload["threadId"].(string)
		itemID, _ := payload["itemId"].(string)
		f.mu.Lock()
		var item object
		if stored := f.activityItems[thread+"/"+itemID]; stored != nil {
			item = clone(stored)
		} else if p := f.threads[thread]; p != nil {
			for _, value := range p["visibleTurnItems"].([]any) {
				candidate, _ := value.(object)["item"].(object)
				if candidate["id"] == itemID {
					item = clone(candidate)
					break
				}
			}
		}
		f.mu.Unlock()
		_ = s.send(object{"_tag": "Exit", "requestId": rid, "exit": object{"_tag": "Success", "value": object{"item": item}}})
		return
	}
	if frame["tag"] == "orchestration.subscribeThread" {
		if payload["requestCompletionMarker"] != true || payload["acceptBoundedSnapshot"] != true {
			f.violation("unbounded subscription requested")
		}
		f.mu.Lock()
		s.subscription = rid
		s.thread, _ = payload["threadId"].(string)
		f.mu.Unlock()
		_ = s.send([]any{object{"_tag": "Chunk", "requestId": rid, "values": []any{object{"kind": "synchronized"}}}})
		return
	}
	if frame["tag"] != "orchestration.dispatchCommand" && frame["tag"] != "orchestration.launchThread" {
		f.violation("unexpected RPC call %v", frame["tag"])
		return
	}
	id, _ := payload["commandId"].(string)
	f.mu.Lock()
	f.dispatches = append(f.dispatches, id)
	if old := f.commands[id]; old != nil {
		a, _ := json.Marshal(old)
		b, _ := json.Marshal(payload)
		if string(a) != string(b) {
			f.violations = append(f.violations, "same operation ID changed upstream payload")
		}
	}
	f.commands[id] = clone(payload)
	receipt := f.receipts[id]
	if receipt == nil {
		f.effects++
		f.sequence++
		if frame["tag"] == "orchestration.launchThread" {
			thread := f.nativeLaunch(payload)
			receipt = object{"threadId": thread}
		} else {
			thread, _ := payload["threadId"].(string)
			p := f.threads[thread]
			switch payload["type"] {
			case "thread.model-selection.set":
				p["thread"].(object)["modelSelection"] = clone(payload["modelSelection"])
			case "message.dispatch":
				if selection := payload["modelSelection"]; selection != nil {
					// Source pin Orchestrator.ts: a dispatch selection can become
					// the thread preference. Do not simulate a temporary override.
					p["thread"].(object)["modelSelection"] = clone(selection)
				}
				if payload["createdBy"] != "user" || payload["creationSource"] != "web" {
					f.violations = append(f.violations, "prompt lost human provenance")
				}
				reply := row(thread, id+"-reply", "Response after "+payload["text"].(string))
				reply["item"].(object)["runId"] = "run-" + thread
				p["visibleTurnItems"] = append(p["visibleTurnItems"].([]any), reply)
				p["runs"] = []any{object{"id": "run-" + thread, "status": "running", "userMessageId": payload["messageId"]}}
			case "run.interrupt":
				if payload["holdQueue"] != true {
					f.violations = append(f.violations, "Stop did not hold server queue")
				}
				for _, item := range p["runs"].([]any) {
					run := item.(object)
					if run["id"] == payload["runId"] {
						run["status"] = "interrupted"
					}
				}
			case "runtime-request.respond":
				for _, item := range p["runtimeRequests"].([]any) {
					request := item.(object)
					if request["id"] == payload["requestId"] {
						request["status"] = "resolved"
					}
				}
			default:
				if !f.nativeThreadAction(p, payload) {
					f.violations = append(f.violations, fmt.Sprintf("unexpected command: %v", payload["type"]))
				}
			}
			if f.nativeContract {
				p["thread"].(object)["updatedAt"] = f.nativeTimestamp()
			}
			receipt = object{"sequence": f.sequence}
		}
		f.receipts[id] = receipt
	}
	drop := f.dropAck[id] || (f.dropMessages && payload["type"] == "message.dispatch") || (f.dropSelections && payload["type"] == "thread.model-selection.set")
	var hold *fixtureAckHold
	if tag == "orchestration.launchThread" {
		hold, f.nextLaunchAck = f.nextLaunchAck, nil
	} else if payload["type"] == "message.dispatch" {
		hold, f.nextSendAck = f.nextSendAck, nil
	}
	f.mu.Unlock()
	if hold != nil {
		hold.started <- id
		// Delay only the receipt, so the socket can still process Stop and
		// subscription traffic while authoritative events overtake this ACK.
		go func() {
			<-hold.release
			if !drop {
				_ = s.send(object{"_tag": "Exit", "requestId": rid, "exit": object{"_tag": "Success", "value": receipt}})
			}
		}()
		return
	}
	if !drop {
		_ = s.send(object{"_tag": "Exit", "requestId": rid, "exit": object{"_tag": "Success", "value": receipt}})
	}
}

func (f *fixture) broadcast(thread, kind string, payload object, sequence int64) {
	f.mu.Lock()
	if sequence == 0 {
		f.sequence++
		sequence = f.sequence
	}
	frames := map[*fixtureSocket]object{}
	for s := range f.sockets {
		if s.subscription != "" {
			frames[s] = object{"_tag": "Chunk", "requestId": s.subscription, "values": []any{object{"kind": "event", "sequence": sequence,
				"event": object{"threadId": thread, "type": kind, "payload": payload}}}}
		}
	}
	f.mu.Unlock()
	for s, frame := range frames {
		_ = s.send(frame)
	}
}

func (f *fixture) update(thread, text string, sequence int64) {
	f.mu.Lock()
	item := f.threads[thread]["visibleTurnItems"].([]any)[0].(object)["item"].(object)
	if sequence == 0 {
		item["text"] = text
		item["streaming"] = true
	}
	payload := clone(item)
	payload["text"] = text
	f.mu.Unlock()
	f.broadcast(thread, "turn-item.updated", payload, sequence)
}

func (f *fixture) closeSockets() {
	f.mu.Lock()
	sockets := make([]*fixtureSocket, 0, len(f.sockets))
	for s := range f.sockets {
		sockets = append(sockets, s)
	}
	f.mu.Unlock()
	for _, s := range sockets {
		_ = s.conn.Close()
	}
}

func (f *fixture) corruptSockets() {
	f.mu.Lock()
	sockets := make([]*fixtureSocket, 0, len(f.sockets))
	for s := range f.sockets {
		sockets = append(sockets, s)
	}
	f.mu.Unlock()
	for _, s := range sockets {
		_ = s.sendRaw([]byte("{not-valid-json"))
	}
}

func (f *fixture) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.effects, len(f.dispatches)
}

func (f *fixture) requestCounts() map[string]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	counts := make(map[string]int, len(f.requests))
	for path, count := range f.requests {
		counts[path] = count
	}
	return counts
}

func (f *fixture) holdNextSendAck() (<-chan string, func()) {
	return f.holdNextAck(false)
}

func (f *fixture) holdNextLaunchAck() (<-chan string, func()) {
	return f.holdNextAck(true)
}

func (f *fixture) holdNextAck(launch bool) (<-chan string, func()) {
	hold := &fixtureAckHold{started: make(chan string, 1), release: make(chan struct{})}
	f.mu.Lock()
	next := &f.nextSendAck
	if launch {
		next = &f.nextLaunchAck
	}
	if *next != nil {
		f.mu.Unlock()
		f.t.Fatal("an acknowledgement for this operation kind is already held")
	}
	*next = hold
	f.mu.Unlock()
	var once sync.Once
	release := func() { once.Do(func() { close(hold.release) }) }
	f.t.Cleanup(release)
	return hold.started, release
}

func (f *fixture) waitCommand(t *testing.T, id string) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		found := f.receipts[id] != nil
		f.mu.Unlock()
		if found {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("actual adapter did not dispatch %s", id)
}

func (f *fixture) waitMessage(t *testing.T, text string) string {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		for id, command := range f.commands {
			if command["text"] == text {
				f.mu.Unlock()
				return id
			}
		}
		f.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("actual app did not dispatch message %q", text)
	return ""
}

func (f *fixture) start(t *testing.T) *wire.Connection {
	t.Helper()
	c, err := wire.Start(context.Background(), f.profile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		if err := c.WaitClosed(ctx); err != nil {
			t.Errorf("helper cleanup: %v", err)
		}
	})
	return c
}

func request(t *testing.T, c *wire.Connection, op string, fields object, id string) object {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	result, err := c.Request(ctx, op, fields, id)
	if err != nil {
		t.Fatalf("%s: %v", op, err)
	}
	return result
}

func untilView(t *testing.T, c *wire.Connection, store *wire.Store, predicate func(*wire.Snapshot) bool) *wire.Snapshot {
	t.Helper()
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	if current := store.Snapshot(); current != nil && predicate(current) {
		return current
	}
	for {
		select {
		case packet, ok := <-c.Events():
			if !ok {
				t.Fatalf("connection ended before expected view: %v", c.Err())
			}
			if packet.Event == "view" || packet.Event == "delta" {
				if _, err := store.Apply(packet.Event, packet.Data); err != nil {
					t.Fatalf("actual-adapter %s rejected: %v\n%s", packet.Event, err, packet.Data)
				}
				if current := store.Snapshot(); current != nil && predicate(current) {
					return current
				}
			}
		case <-deadline.C:
			t.Fatalf("timed out waiting for actual-adapter view; last=%+v", store.Snapshot())
		}
	}
}

func fixtureCatalog() object {
	options := []any{
		object{"id": "reasoningEffort", "label": "Reasoning", "type": "select", "currentValue": "medium", "options": []any{
			object{"id": "low", "label": "Low"}, object{"id": "medium", "label": "Medium", "isDefault": true}, object{"id": "high", "label": "High"}}},
		object{"id": "serviceTier", "label": "Service tier", "type": "select", "currentValue": "default", "options": []any{
			object{"id": "default", "label": "Standard", "isDefault": true}, object{"id": "fast", "label": "Fast"}}},
	}
	return object{"settings": object{"defaultModelSelection": object{"instanceId": "fixture-provider", "model": "fixture-model"}, "defaultRuntimeMode": "approval-required", "defaultThreadEnvMode": "local"}, "providers": []any{object{
		"instanceId": "fixture-provider", "name": "Fixture provider", "status": "ready", "auth": object{"status": "authenticated"},
		"models": []any{
			object{"slug": "fixture-model", "name": "Fixture Model", "isDefault": true, "capabilities": object{"optionDescriptors": options}},
			object{"slug": "fixture-other", "name": "Other Model", "capabilities": object{"optionDescriptors": options}},
			object{"slug": "fixture-basic", "name": "Basic Model", "capabilities": nil},
			object{"slug": "fixture-boolean", "name": "Boolean Speed", "capabilities": object{"optionDescriptors": []any{object{"id": "fastMode", "label": "Fast mode", "type": "boolean", "currentValue": false}}}},
		},
	}}}
}
