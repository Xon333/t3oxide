package client

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
)

func recoveryManager(t *testing.T) (*Manager, *config.State, config.Environment) {
	t.Helper()
	state, err := config.OpenState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	env := config.Environment{ID: "fixture", Label: "Fixture", EnvironmentID: "fixture-env", Node: "/usr/bin/node", Adapter: "/tmp/fixture.mjs", HostConfig: "/tmp/host.json", Enabled: true}
	m := NewManager([]config.Environment{env}, state)
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
		_ = state.Close()
	})
	return m, state, env
}

func recoveryView(revision int, historical bool, requests []map[string]any) json.RawMessage {
	if requests == nil {
		requests = []map[string]any{}
	}
	state := "live"
	var run any = "reviewed-run"
	if historical {
		state = "historical"
		run = nil
	}
	b, _ := json.Marshal(map[string]any{"threadId": "thread", "title": "Test thread", "projectId": "project", "model": "model", "status": "running", "activeRunId": run, "requests": requests, "hasMore": true, "historyCursor": "opaque", "limited": false, "sequence": 3, "state": state, "live": !historical, "historical": historical, "revision": revision, "items": []any{}})
	return b
}

// A tiny process fixture isolates the app's admission/recovery behavior. The
// actual adapter, HTTP/WebSocket protocol and durable host receipts are tested
// separately by internal/integration; this fixture never claims that coverage.
func recoverySession(t *testing.T, responseBody string) (*Session, *config.State, string) {
	t.Helper()
	m, state, env := recoveryManager(t)
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 needed for isolated app fixture")
	}
	dir := t.TempDir()
	env.Node = python
	env.Adapter = filepath.Join(dir, "fixture.py")
	env.HostConfig = filepath.Join(dir, "requests.jsonl")
	body := "import sys,json\ncount=0\nfor line in sys.stdin:\n p=json.loads(line);count+=1\n with open(sys.argv[-1],'a') as f:f.write(json.dumps(p)+'\\n')\n " + responseBody + "\n out.update(v=1,environmentId='fixture-env',id=p['id']);sys.stdout.write(json.dumps(out)+'\\n');sys.stdout.flush()\n"
	if err = os.WriteFile(env.Adapter, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	m.Environments = []config.Environment{env}
	a, err := m.NewSession(env.ID, "thread")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	c, err := wire.StartWithOptions(ctx, env, wire.Options{RequestTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	a.ctx, a.cancel, a.conn, a.ready, a.writable, a.helloVerified = ctx, cancel, c, true, true, true
	if err = a.applyPacket(wire.Packet{Event: "view", Data: recoveryView(1, false, nil)}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close(); cancel() })
	return a, state, env.HostConfig
}
func requestsFromLog(t *testing.T, path string) []map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var result []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		var p map[string]any
		if err = json.Unmarshal([]byte(line), &p); err != nil {
			t.Fatal(err)
		}
		result = append(result, p)
	}
	return result
}

func TestReceiptNumbersRequireExactSafeNonnegativeIntegers(t *testing.T) {
	for _, bad := range []any{nil, "1", true, 1.5, math.NaN(), math.Inf(1), float64(wire.MaxInteger + 1), int64(wire.MaxInteger + 1), json.Number("bad"), json.Number("1.5"), json.Number("9007199254740992")} {
		if got := numberValue(bad); got >= 0 {
			t.Errorf("invalid acknowledgement number %#v became %d", bad, got)
		}
	}
	for _, good := range []any{0, 1, int64(2), float64(3), json.Number("4"), json.Number("9007199254740991")} {
		if got := numberValue(good); got < 0 {
			t.Errorf("valid integer rejected: %v", good)
		}
	}
}

func TestRapidSameThreadSwitchRestoresLatestUnflushedDraft(t *testing.T) {
	m, state, env := recoveryManager(t)
	a, err := m.NewSession(env.ID, "thread")
	if err != nil {
		t.Fatal(err)
	}
	if err = a.SetDraft("latest unsaved keystrokes"); err != nil {
		t.Fatal(err)
	}
	target, err := a.Target(env.ID, "thread")
	if err != nil {
		t.Fatal(err)
	}
	next := target
	if next.Draft() != "latest unsaved keystrokes" || state.Draft(env.EnvironmentID, "thread") != "latest unsaved keystrokes" {
		t.Fatal("rapid switch lost newest draft before persistence timer")
	}
}

func TestLazyTargetReadsDraftAtActivationAfterInterveningKeystrokes(t *testing.T) {
	m, _, env := recoveryManager(t)
	a, err := m.NewSession(env.ID, "thread")
	if err != nil {
		t.Fatal(err)
	}
	if err = a.SetDraft("initial draft"); err != nil {
		t.Fatal(err)
	}
	target, err := a.Target(env.ID, "thread")
	if err != nil {
		t.Fatal(err)
	}
	// The picker command prepared its target, but the outgoing UI can still
	// deliver final keystrokes before the UI saves the composer.
	if err = a.SetDraft("keystrokes before switch application"); err != nil {
		t.Fatal(err)
	}
	if got := target.Draft(); got != "keystrokes before switch application" {
		t.Fatalf("lazy target restored stale draft %q", got)
	}
}

func TestRetiredAttachmentFlushCannotReplaceCurrentDraft(t *testing.T) {
	m, state, env := recoveryManager(t)
	retired, err := m.NewSession(env.ID, "thread")
	if err != nil {
		t.Fatal(err)
	}
	if err = retired.SetDraft("last text before detaching"); err != nil {
		t.Fatal(err)
	}
	current, err := m.NewSession(env.ID, "thread")
	if err != nil {
		t.Fatal(err)
	}
	if err = current.SetDraft("new typing in the current attachment"); err != nil {
		t.Fatal(err)
	}
	if err = current.flushDraft(); err != nil {
		t.Fatal(err)
	}
	// The outgoing Events teardown can finish after the new attachment has
	// already saved its draft. Its final flush must not restore older text.
	if err = retired.flushDraft(); err != nil {
		t.Fatal(err)
	}
	if got := state.Draft(env.EnvironmentID, "thread"); got != "new typing in the current attachment" {
		t.Fatalf("retired attachment overwrote current durable draft with %q", got)
	}
}

func TestDiscardedLazyTargetsDoNotAccumulateRetiredAgents(t *testing.T) {
	m, _, env := recoveryManager(t)
	for i := 0; i < 50; i++ {
		if _, err := m.NewSession(env.ID, "thread"); err != nil {
			t.Fatal(err)
		}
	}
	m.mu.Lock()
	count := len(m.sessions)
	m.mu.Unlock()
	if count != 0 {
		t.Fatalf("%d unstarted agents retained by manager", count)
	}
}

func TestMalformedAckBlocksNewWritesAndExplicitRetryKeepsID(t *testing.T) {
	a, state, path := recoverySession(t, "out={'result':{'sequence':1.5 if count==1 else 7}}")
	if err := a.SetDraft("hello"); err != nil {
		t.Fatal(err)
	}
	if err := a.Send(context.Background(), "hello"); err == nil {
		t.Fatal("fractional ACK reported success")
	}
	pending := state.Pending("fixture-env", false)
	if pending == nil || state.Draft("fixture-env", "thread") != "hello" {
		t.Fatal("malformed ACK discarded recovery or draft")
	}
	if err := a.Send(context.Background(), "duplicate attempt"); err == nil {
		t.Fatal("new operation allowed before explicit recovery")
	}
	if len(requestsFromLog(t, path)) != 1 {
		t.Fatal("duplicate write escaped pending guard")
	}
	if _, err := a.Retry(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	writes := requestsFromLog(t, path)
	if len(writes) != 2 || writes[0]["id"] != writes[1]["id"] || writes[1]["message"] != "hello" {
		t.Fatalf("retry changed original operation: %#v", writes)
	}
	if state.Pending("fixture-env", false) != nil || state.Draft("fixture-env", "thread") != "" {
		t.Fatal("authoritative retry ACK did not clear recovery and submitted draft")
	}
}

func TestNegativeRetryCannotDisproveEarlierAcceptance(t *testing.T) {
	a, state, path := recoverySession(t, "out={'error':{'code':'authorization','message':'renew credential','uncertain':count==1}}")
	if err := a.SetDraft("hello"); err != nil {
		t.Fatal(err)
	}
	if err := a.Send(context.Background(), "hello"); err == nil {
		t.Fatal("expected uncertain first attempt")
	}
	first := state.Pending("fixture-env", false)
	if _, err := a.Retry(context.Background(), false); err == nil {
		t.Fatal("expected rejected retry")
	}
	if got := state.Pending("fixture-env", false); got == nil || got["id"] != first["id"] {
		t.Fatal("negative retry discarded original uncertainty")
	}
	if len(requestsFromLog(t, path)) != 2 {
		t.Fatal("wrong explicit attempt count")
	}
}

func TestStopRetainsReviewedTargetAcrossHistoryAndIgnoresDraftFailure(t *testing.T) {
	a, state, path := recoverySession(t, "out={'result':{'sequence':10}}")
	if err := state.SavePending("fixture-env", false, map[string]any{"id": "normal-uncertain", "op": "send", "environmentId": "fixture-env", "threadId": "thread", "message": "saved"}); err != nil {
		t.Fatal(err)
	}
	if err := a.applyPacket(wire.Packet{Event: "view", Data: recoveryView(2, true, nil)}); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.draftError = errors.New("draft limit reached")
	a.mu.Unlock()
	if err := a.Stop(context.Background()); err != nil {
		t.Fatal("independent Stop unavailable:", err)
	}
	writes := requestsFromLog(t, path)
	if len(writes) != 1 || writes[0]["op"] != "stop" || writes[0]["runId"] != "reviewed-run" {
		t.Fatalf("Stop retargeted or sent wrong operation: %#v", writes)
	}
	if state.Pending("fixture-env", false) == nil {
		t.Fatal("Stop consumed normal recovery lane")
	}
	a.mu.Lock()
	a.draftError = nil
	a.mu.Unlock()
}

func TestLegacyStopInNormalRecoveryLaneBlocksAnotherStop(t *testing.T) {
	a, state, path := recoverySession(t, "out={'result':{'sequence':10}}")
	legacy := map[string]any{"id": "legacy-stop", "op": "stop", "environmentId": "fixture-env", "threadId": "thread", "runId": "original-reviewed-run"}
	if err := state.SavePending("fixture-env", false, legacy); err != nil {
		t.Fatal(err)
	}
	if err := a.Stop(context.Background()); err == nil {
		t.Fatal("new Stop bypassed unresolved legacy Stop")
	}
	if len(requestsFromLog(t, path)) != 0 || state.Pending("fixture-env", false)["id"] != "legacy-stop" {
		t.Fatal("legacy Stop protection changed host or recovery record")
	}
}

type recoveryAsker func(context.Context, Question) (Answer, error)

func (f recoveryAsker) Ask(ctx context.Context, r Question) (Answer, error) {
	return f(ctx, r)
}
func approval(revision string) map[string]any {
	return map[string]any{"id": "request-id", "revision": revision, "kind": "approval_request", "prompt": "Review this command", "supported": true, "options": []map[string]any{{"decision": "acceptForSession", "label": "Approve for session", "warning": "Applies to this session"}, {"decision": "decline", "label": "Decline"}}}
}

func TestApprovalUsesExactOfferedDecisionRequestIDAndRevision(t *testing.T) {
	a, _, path := recoverySession(t, "out={'result':{'sequence':10}}")
	if err := a.applyPacket(wire.Packet{Event: "view", Data: recoveryView(2, false, []map[string]any{approval("request-revision")})}); err != nil {
		t.Fatal(err)
	}
	a.asker = recoveryAsker(func(_ context.Context, r Question) (Answer, error) {
		if r.Kind != ChooseOne || r.Choices[0].ID != "acceptForSession" || r.Choices[0].Description != "Applies to this session" {
			t.Errorf("approval semantics lost: %#v", r)
		}
		return Answer{Cancelled: false, ChoiceIDs: []string{"acceptForSession"}}, nil
	})
	if err := a.ReviewRequests(context.Background()); err != nil {
		t.Fatal(err)
	}
	writes := requestsFromLog(t, path)
	if len(writes) != 1 || writes[0]["op"] != "answer" || writes[0]["requestId"] != "request-id" || writes[0]["revision"] != "request-revision" || writes[0]["decision"] != "acceptForSession" {
		t.Fatalf("wrong authoritative answer: %#v", writes)
	}
}

func TestDismissedOrSupersededApprovalDoesNotSendGenericDecision(t *testing.T) {
	for _, superseded := range []bool{false, true} {
		t.Run(map[bool]string{false: "dismissed", true: "superseded"}[superseded], func(t *testing.T) {
			a, state, path := recoverySession(t, "out={'result':{'sequence':10}}")
			if err := a.applyPacket(wire.Packet{Event: "view", Data: recoveryView(2, false, []map[string]any{approval("old")})}); err != nil {
				t.Fatal(err)
			}
			a.asker = recoveryAsker(func(_ context.Context, _ Question) (Answer, error) {
				if superseded {
					if err := a.applyPacket(wire.Packet{Event: "view", Data: recoveryView(3, false, []map[string]any{approval("new")})}); err != nil {
						t.Fatal(err)
					}
					return Answer{Cancelled: false, ChoiceIDs: []string{"acceptForSession"}}, nil
				}
				return Answer{Cancelled: true}, nil
			})
			err := a.ReviewRequests(context.Background())
			if !superseded && err != nil {
				t.Fatal(err)
			}
			if len(requestsFromLog(t, path)) != 0 || state.Pending("fixture-env", false) != nil {
				t.Fatal("dismissal/stale modal sent a response")
			}
		})
	}
}

func TestQuestionAnswersKeepT3ValuesAndCustomMultiSelection(t *testing.T) {
	m, _, env := recoveryManager(t)
	a, err := m.NewSession(env.ID, "thread")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	a.asker = recoveryAsker(func(_ context.Context, r Question) (Answer, error) {
		n++
		if n == 1 {
			if r.Kind != ChooseMany {
				t.Error("multi choice lost")
			}
			return Answer{Cancelled: false, ChoiceIDs: []string{"1", "custom"}}, nil
		}
		return Answer{Cancelled: false, Text: "custom value"}, nil
	})
	question := map[string]any{"id": "question-id", "multiSelect": true, "allowCustomAnswer": true, "options": []map[string]any{{"label": "First", "value": "a"}, {"label": "Second", "value": "canonical-b"}}}
	value, answered, err := a.askQuestion(context.Background(), "Prompt", question)
	if err != nil || !answered || !reflect.DeepEqual(value, []string{"canonical-b", "custom value"}) {
		t.Fatalf("question answer mapping failed: %#v %v %v", value, answered, err)
	}
}

func TestLateAcknowledgementReachesMatchingCurrentAttachment(t *testing.T) {
	for _, currentText := range []string{"submitted text", "newer typing"} {
		t.Run(currentText, func(t *testing.T) {
			original, state, _ := recoverySession(t, "out={'result':{'sequence':10}}")
			if err := original.SetDraft("submitted text"); err != nil {
				t.Fatal(err)
			}
			if err := original.flushDraft(); err != nil {
				t.Fatal(err)
			}
			// The original submit command still awaits its ACK while a newly
			// attached instance of the same thread now owns the composer.
			current, err := original.manager.NewSession("fixture", "thread")
			if err != nil {
				t.Fatal(err)
			}
			if err = current.SetDraft(currentText); err != nil {
				t.Fatal(err)
			}
			if err = current.flushDraft(); err != nil {
				t.Fatal(err)
			}
			unrelated, err := original.manager.NewSession("fixture", "another-thread")
			if err != nil {
				t.Fatal(err)
			}
			if err = unrelated.SetDraft("submitted text"); err != nil {
				t.Fatal(err)
			}
			original.manager.mu.Lock()
			original.manager.sessions[current] = struct{}{}
			original.manager.sessions[unrelated] = struct{}{}
			original.manager.mu.Unlock()
			if _, err = original.perform(context.Background(), "send", map[string]any{"threadId": "thread", "message": "submitted text", "mode": "auto"}, "", false); err != nil {
				t.Fatal(err)
			}
			want := currentText
			if currentText == "submitted text" {
				want = ""
			}
			if current.Draft() != want || state.Draft("fixture-env", "thread") != want {
				t.Fatalf("late ACK did not preserve conditional draft semantics: current=%q durable=%q want=%q", current.Draft(), state.Draft("fixture-env", "thread"), want)
			}
			if event := current.renderEvent(); event.DraftAccepted == nil || *event.DraftAccepted != "submitted text" {
				t.Fatalf("current attachment did not receive exact accepted text: %#v", event.DraftAccepted)
			}
			if current.renderEvent().DraftAccepted != nil {
				t.Fatal("accepted-draft event was replayed")
			}
			if unrelated.Draft() != "submitted text" || unrelated.renderEvent().DraftAccepted != nil {
				t.Fatal("late ACK crossed thread boundary")
			}
		})
	}
}

func TestQueuedAttachmentFlushCannotRestoreAcknowledgedText(t *testing.T) {
	m, state, env := recoveryManager(t)
	current, err := m.NewSession(env.ID, "thread")
	if err != nil {
		t.Fatal(err)
	}
	if err = current.SetDraft("submitted text"); err != nil {
		t.Fatal(err)
	}
	if err = state.SetDraft(env.EnvironmentID, "thread", "submitted text"); err != nil {
		t.Fatal(err)
	}
	if err = state.SavePending(env.EnvironmentID, false, map[string]any{"id": "original-send", "op": "send", "environmentId": env.EnvironmentID, "threadId": "thread", "message": "submitted text"}); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.sessions[current] = struct{}{}
	m.mu.Unlock()
	finished := make(chan error, 1)
	// Hold the ACK transaction's shared persistence boundary while a timer
	// on the newly attached composer requests its own draft flush.
	func() {
		m.draftPersistence.Lock()
		defer m.draftPersistence.Unlock()
		started := make(chan struct{})
		go func() {
			close(started)
			finished <- current.flushDraft()
		}()
		<-started
		select {
		case err := <-finished:
			t.Fatalf("attachment flush bypassed in-progress ACK transaction: %v", err)
		case <-time.After(50 * time.Millisecond):
		}
		if err = state.Acknowledge(env.EnvironmentID, false, "original-send", "thread", "submitted text"); err != nil {
			t.Fatal(err)
		}
		m.acceptDraft(env.EnvironmentID, "thread", "submitted text")
	}()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued draft flush did not finish after ACK transaction")
	}
	if got := state.Draft(env.EnvironmentID, "thread"); got != "" {
		t.Fatalf("queued flush resurrected accepted text %q without its pending ID", got)
	}
	if state.Pending(env.EnvironmentID, false) != nil {
		t.Fatal("acknowledgement failed to release exact pending operation")
	}
}
