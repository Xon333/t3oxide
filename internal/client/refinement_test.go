package client

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
)

func receiptSnapshot(run, message, status, text string, sequence int64) *wire.Snapshot {
	return &wire.Snapshot{Metadata: wire.Metadata{ThreadID: "thread", Live: true, Sequence: &sequence, RuntimeDetails: &wire.RuntimeDetails{RunID: &run, RunMessageID: &message, Status: status}}, Items: []wire.Row{{Key: "thread/agent", RunID: &run, Kind: "assistant_message", Text: text}}}
}
func TestReceiptLateAcknowledgementCannotReviveRespondedOrCompletedSend(t *testing.T) {
	for _, phase := range []string{"running", "completed", "failed", "interrupted"} {
		t.Run(phase, func(t *testing.T) {
			a := &Session{}
			a.beginReceiptLocked("new", "thread", receiptSnapshot("old", "old:message", "running", "old", 1))
			s := receiptSnapshot("new-run", "new:message", phase, "new response", 4)
			a.observeReceiptLocked(s)
			want := phase
			if want == "running" {
				want = "responding"
			}
			if a.receipt.Phase != want {
				t.Fatalf("response evidence %s", a.receipt.Phase)
			}
			a.acknowledgeReceiptLocked("new", 3, s)
			if a.receipt.Phase != want || a.receipt.Sequence != 3 || a.receipt.RunID != "new-run" {
				t.Fatalf("late ACK revived evidence: %+v", a.receipt)
			}
		})
	}
}
func TestQueuedReceiptDoesNotOwnPreviousRunOutputOrCompletion(t *testing.T) {
	a := &Session{}
	a.beginReceiptLocked("queued", "thread", receiptSnapshot("old", "old:message", "running", "before", 1))
	for _, status := range []string{"running", "completed"} {
		a.observeReceiptLocked(receiptSnapshot("old", "old:message", status, "old response changed", 5))
		if a.receipt.Phase != "sending" || a.receipt.RunID != "" {
			t.Fatalf("old output claimed queued send: %+v", a.receipt)
		}
	}
	a.acknowledgeReceiptLocked("queued", 3, receiptSnapshot("old", "old:message", "completed", "old response", 6))
	if a.receipt.Phase != "acknowledged" {
		t.Fatal(a.receipt)
	}
	s := receiptSnapshot("queued-run", "queued:message", "running", "queued response", 9)
	oldRun := "old"
	s.Items[0].RunID = &oldRun
	a.observeReceiptLocked(s)
	if a.receipt.Phase != "acknowledged" {
		t.Fatal("new run inherited old run text")
	}
	newRun := "queued-run"
	s.Items[0].RunID = &newRun
	a.observeReceiptLocked(s)
	if a.receipt.Phase != "responding" {
		t.Fatal(a.receipt)
	}
	// Retry admission preserves already observed evidence.
	a.beginReceiptLocked("queued", "thread", s)
	if a.receipt.Phase != "responding" {
		t.Fatal("same-ID retry erased response")
	}
}
func TestReceiptRequiresNativeCausalIdentityAndLiveMatchingThread(t *testing.T) {
	a := &Session{}
	a.beginReceiptLocked("id", "thread", nil)
	s := receiptSnapshot("run", "id:message", "completed", "response", 3)
	s.Metadata.RuntimeDetails.RunMessageID = nil
	a.observeReceiptLocked(s)
	s.Metadata.RuntimeDetails.RunMessageID = &a.receipt.MessageID
	s.Metadata.Historical = true
	a.observeReceiptLocked(s)
	s.Metadata.Historical = false
	s.Metadata.ThreadID = "other"
	a.observeReceiptLocked(s)
	if a.receipt.Phase != "sending" {
		t.Fatal("unrelated/unreported page supplied receipt authority")
	}
}
func TestLaunchCatalogAllowsCreationOnlyModelsAndUsesAdvertisedDefaults(t *testing.T) {
	raw := clientCatalogFixture()
	raw["requiresNewThreadForModelChange"] = true
	c, err := DecodeCatalog(raw)
	if err != nil {
		t.Fatal(err)
	}
	next, err := c.launchModel("fast")
	if err != nil {
		t.Fatal(err)
	}
	if next.InstanceID != c.Current.InstanceID || next.Model != "fast" {
		t.Fatal(next)
	}
	if v, ok := next.Option("fastMode"); !ok || v != false {
		t.Fatal("advertised boolean default missing")
	}
	if _, ok := next.Option("reasoningEffort"); ok {
		t.Fatal("source effort leaked into launch")
	}
	if err = c.validate(next); err == nil {
		t.Fatal("creation bypass changed existing-thread validation")
	}
	if err = c.validateLaunch(next.WithOption("invented", "x")); err == nil {
		t.Fatal("launch permitted unoffered options")
	}
	next.InstanceID = "other"
	if c.validateLaunch(next) == nil {
		t.Fatal("launch switched account")
	}
}
func TestBrowserScopeRejectsUnadvertisedDuplicateAndEmptyIdentity(t *testing.T) {
	m, _, env := recoveryManager(t)
	other := env
	other.ID = "other"
	other.Label = env.Label
	m.Environments = append(m.Environments, other)
	for _, ids := range [][]string{nil, {env.ID, env.ID}, {"stale"}} {
		if m.selectScope(ids) == nil {
			t.Fatalf("invalid scope accepted %v", ids)
		}
	}
	if err := m.selectScope([]string{other.ID, env.ID}); err != nil {
		t.Fatal(err)
	}
	got := m.browserScope("")
	got[0] = "changed"
	if !reflect.DeepEqual(m.browserScope(""), []string{other.ID, env.ID}) {
		t.Fatal("scope alias mutated owner")
	}
	if !reflect.DeepEqual(m.ProfileIDs(), []string{env.ID, other.ID}) {
		t.Fatal("configured identities depend on scope")
	}
}
func TestBrowserCancellationAndStaleChoicesKeepCurrentConversation(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "stale"}[invalid], func(t *testing.T) {
			a, state, path := recoverySession(t, "out={'result':{'threads':[{'id':'thread','title':'Same','project':'Project','status':'running','model':'model'}],'more':False}}")
			_ = a.SetDraft("  exact source draft\n")
			a.asker = recoveryAsker(func(ctx context.Context, q Question) (Answer, error) {
				if q.Surface != SurfaceBrowser || q.BrowserPage.ProfileID != "fixture" {
					t.Fatal("missing browser identity")
				}
				if invalid {
					return Answer{ChoiceIDs: []string{browserChoiceID("other", "thread")}}, nil
				}
				return Answer{Cancelled: true}, nil
			})
			result, err := a.Browse(context.Background(), "")
			if invalid != (err != nil) || result.Next != nil {
				t.Fatalf("unexpected target %+v %v", result, err)
			}
			if a.Status().ThreadID != "thread" || a.Draft() != "  exact source draft\n" || state.Pending("fixture-env", false) != nil {
				t.Fatal("browser changed active conversation")
			}
			packets := requestsFromLog(t, path)
			if len(packets) != 1 || packets[0]["op"] != "list" {
				t.Fatalf("browser requested detail: %+v", packets)
			}
		})
	}
}
func TestLaunchCancelledReviewPersistsExactSeparateDraft(t *testing.T) {
	a, state, path := recoverySession(t, "out={'result':{'sequence':10}}")
	_ = a.SetDraft("source composer")
	input := LaunchInput{Title: "  Human title  ", Message: "  first\nmessage\n"}
	a.asker = recoveryAsker(func(_ context.Context, q Question) (Answer, error) {
		if q.Surface != SurfaceLaunch || q.LaunchReview.Input.Title != "New conversation" {
			t.Fatalf("missing review: %+v", q)
		}
		if q.LaunchReview.Unavailable == "" {
			t.Fatal("old adapter silently offered editing")
		}
		return Answer{Cancelled: true, Launch: &input}, nil
	})
	result, err := a.newConversation(context.Background(), "")
	if err != nil || result.Next != nil {
		t.Fatal(result, err)
	}
	if state.Draft("fixture-env", launchDraftThread("thread")) != input.Message || a.Draft() != "source composer" || len(requestsFromLog(t, path)) != 0 {
		t.Fatal("cancel changed source or sent launch")
	}
}
func TestStatusHasIndependentDimensionsAndDefensiveReceipt(t *testing.T) {
	a, state, _ := recoverySession(t, "out={'result':{'sequence':10}}")
	a.mu.Lock()
	a.beginReceiptLocked("operation", "thread", nil)
	a.mu.Unlock()
	record := map[string]any{"id": "saved", "op": "send", "environmentId": "fixture-env", "threadId": "thread", "message": "uncertain"}
	if err := state.SavePending("fixture-env", false, record); err != nil {
		t.Fatal(err)
	}
	s := a.Status()
	if s.RunState != "running" || s.ActiveRunID != "reviewed-run" || !s.PendingAction || s.PendingStop || len(s.ProfileIDs) != 1 || s.Location.ProjectID != "project" {
		t.Fatalf("lost dimensions: %+v", s)
	}
	s.Receipt.Phase = "changed"
	s.ProfileIDs[0] = "changed"
	if a.Status().Receipt.Phase != "sending" || a.Status().ProfileIDs[0] != "fixture" {
		t.Fatal("consumer mutated status")
	}
}
func TestPromptLaunchReplyIsCopied(t *testing.T) {
	b := newPromptBroker()
	out := make(chan Answer, 1)
	go func() { answer, _ := b.Ask(context.Background(), Question{Surface: SurfaceLaunch}); out <- answer }()
	p := <-b.requests
	input := &LaunchInput{Title: "title", Message: "exact"}
	if !p.Respond(Answer{Launch: input}) {
		t.Fatal("not accepted")
	}
	input.Message = "mutated"
	if got := <-out; got.Launch.Message != "exact" {
		t.Fatal("reply aliased")
	}
}
func TestDecodeThreadProjectIdentityIsOptionalAndValidated(t *testing.T) {
	raw := map[string]any{"more": false, "threads": []Thread{{ID: "thread", Title: "title", Project: "same", ProjectID: "project-id", Status: "idle", Model: "model"}}}
	rows, _, err := DecodeThreads(raw)
	if err != nil || rows[0].ProjectID != "project-id" {
		t.Fatal(rows, err)
	}
	b, _ := json.Marshal(raw)
	var copy map[string]any
	_ = json.Unmarshal(b, &copy)
	copy["threads"].([]any)[0].(map[string]any)["projectId"] = "bad\n"
	if _, _, err = DecodeThreads(copy); err == nil {
		t.Fatal("malformed project accepted")
	}
}
func TestChooseScopeCancellationCannotReplaceManagerMembership(t *testing.T) {
	m, _, e := recoveryManager(t)
	_ = m.selectScope([]string{e.ID})
	a, _ := m.NewSession(e.ID, "thread")
	a.asker = recoveryAsker(func(context.Context, Question) (Answer, error) {
		return Answer{Cancelled: true, ChoiceIDs: []string{"stale"}}, nil
	})
	ids, err := a.chooseScope(context.Background(), m.browserScope(""))
	if err != nil || ids != nil || !reflect.DeepEqual(m.browserScope(""), []string{e.ID}) {
		t.Fatal(ids, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a.asker = recoveryAsker(func(ctx context.Context, _ Question) (Answer, error) { return Answer{}, ctx.Err() })
	if _, err = a.chooseScope(ctx, ids); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestLaunchStoreDraftSurvivesPromptCancellationAndBindsSource(t *testing.T) {
	a, state, path := recoverySession(t, "out={'result':{'sequence':10}}")
	_ = a.SetDraft("source composer")
	var store func(LaunchInput) error
	input := LaunchInput{Title: "  exact title ", Message: " unsent\nlaunch draft "}
	a.asker = recoveryAsker(func(_ context.Context, q Question) (Answer, error) {
		store = q.LaunchReview.StoreDraft
		if store == nil {
			t.Fatal("launch has no synchronous draft owner")
		}
		if err := store(input); err != nil {
			t.Fatal(err)
		}
		return Answer{}, context.Canceled
	})
	if _, err := a.newConversation(context.Background(), ""); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if state.Draft("fixture-env", launchDraftThread("thread")) != input.Message || a.Draft() != "source composer" || len(requestsFromLog(t, path)) != 0 {
		t.Fatal("shutdown lost launch or touched source")
	}
	// The callback remains bound to the reviewed source after local prompt/session
	// retirement; it does not derive a destination from current UI attachment.
	a.mu.Lock()
	a.thread = "other"
	a.mu.Unlock()
	input.Message = "after editor\n"
	if err := store(input); err != nil {
		t.Fatal(err)
	}
	if state.Draft("fixture-env", launchDraftThread("thread")) != input.Message || state.Draft("fixture-env", launchDraftThread("other")) != "" {
		t.Fatal("draft callback retargeted source")
	}
	closedState, err := config.OpenState(filepath.Join(t.TempDir(), "closed-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := closedState.Close(); err != nil {
		t.Fatal(err)
	}
	a.manager.State = closedState
	defer func() { a.manager.State = state }()
	if err := store(LaunchInput{Message: "must not vanish"}); err == nil {
		t.Fatal("failed persistence was hidden")
	}
}
