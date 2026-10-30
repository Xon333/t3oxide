package integration_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
)

func TestBatchReceiptExplicitUnchangedResumeKeepsReceiptAndNeverRefilesPrefix(t *testing.T) {
	f := newFixture(t)
	manager, state := newAppManager(t, f)
	a, err := manager.NewSession(f.profile.ID, "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	stream := streamSession(t, a)
	defer stream.stop(t)
	stream.until(t, func(e client.Event) bool { return e.Snapshot != nil && e.Snapshot.Metadata.Live })
	first := make(chan featureResult, 1)
	go func() {
		result, err := a.FileMany(context.Background())
		first <- featureResult{result, err}
	}()
	prompt := featurePrompt(t, a)
	const original = "  A first accepted\n界 B explicitly resumed\n"
	prompt.Respond(client.Answer{Text: original})
	prompt = nativeLaunchReview(t, a)
	input := prompt.Question.LaunchReview.Input
	prompt.Respond(client.Answer{Action: "create", Launch: &input})
	prompt = nativeLaunchReview(t, a)
	prompt.Respond(client.Answer{Cancelled: true})
	result := featureReply(t, first)
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.result.Next != nil {
		defer result.result.Next.Close()
	}
	receipts := a.BatchReceipts()
	if len(receipts) != 1 || receipts[0].Items[0].State != config.BatchAcknowledged || receipts[0].Items[1].State != config.BatchRemainingDraft {
		t.Fatal("cancel did not retain acknowledged prefix and remaining draft", receipts)
	}
	id, operation := receipts[0].ID, receipts[0].Items[0].OperationID
	remaining := state.Draft(receipts[0].EnvironmentID, receipts[0].Source)
	if remaining != "界 B explicitly resumed\n" {
		t.Fatal("acknowledged prefix changed the exact remainder", remaining)
	}
	resumed := make(chan featureResult, 1)
	go func() {
		result, err := a.FileMany(context.Background())
		resumed <- featureResult{result, err}
	}()
	prompt = featurePrompt(t, a)
	if prompt.Question.Initial != remaining {
		t.Fatal("explicit reopen did not show the exact remaining human input")
	}
	prompt.Respond(client.Answer{Text: prompt.Question.Initial})
	prompt = nativeLaunchReview(t, a)
	input = prompt.Question.LaunchReview.Input
	if input.Message != "界 B explicitly resumed" {
		t.Fatal("explicit resume returned to the acknowledged prefix")
	}
	prompt.Respond(client.Answer{Action: "create", Launch: &input})
	result = featureReply(t, resumed)
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.result.Next != nil {
		defer result.result.Next.Close()
	}
	receipts = a.BatchReceipts()
	if len(receipts) != 1 || receipts[0].ID != id || receipts[0].Original != original || receipts[0].Items[0].OperationID != operation || receipts[0].Items[1].State != config.BatchAcknowledged {
		t.Fatal("explicit resume lost original receipt/identity/input", receipts)
	}
	if effects, dispatches := f.counts(); effects != 2 || dispatches != 2 {
		t.Fatalf("explicit resume duplicated the acknowledged prefix: %d/%d", effects, dispatches)
	}
}

func TestBatchReceiptActualAdapterRestartPartialAcknowledgementAndEditedDraft(t *testing.T) {
	f := newFixture(t)
	path := filepath.Join(t.TempDir(), "state.json")
	state, err := config.OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	manager := client.NewManager([]config.Environment{f.profile}, state)
	a, err := manager.NewSession(f.profile.ID, "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	stream := streamSession(t, a)
	stream.until(t, func(e client.Event) bool { return e.Snapshot != nil && e.Snapshot.Metadata.Live })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan featureResult, 1)
	go func() { r, e := a.FileMany(ctx); out <- featureResult{r, e} }()
	p := featurePrompt(t, a)
	const original = "  A accepted\r\n\tB uncertain\n界 C remains draft\n"
	p.Respond(client.Answer{Text: original})
	p = nativeLaunchReview(t, a)
	input := p.Question.LaunchReview.Input
	p.Respond(client.Answer{Action: "create", Launch: &input})
	p = nativeLaunchReview(t, a)
	started, release := f.holdNextLaunchAck()
	input = p.Question.LaunchReview.Input
	p.Respond(client.Answer{Action: "create", Launch: &input})
	var operation string
	select {
	case operation = <-started:
	case <-time.After(6 * time.Second):
		t.Fatal("second item did not reach actual adapter")
	}
	cancel()
	result := featureReply(t, out)
	if result.err == nil {
		t.Fatal("uncertain second launch became success")
	}
	if !strings.Contains(result.result.Detail, "Acknowledged") || !strings.Contains(result.result.Detail, "Needs recovery") || !strings.Contains(result.result.Detail, "Remaining draft") {
		t.Fatalf("partial receipt not surfaced: %s", result.result.Detail)
	}
	pending := state.Pending("env-a", false)
	thread, _ := pending["threadId"].(string)
	if pending["id"] != operation || pending["message"] != "\tB uncertain" {
		t.Fatal("pending item lost original identity/text")
	}
	stream.stop(t)
	if err = manager.Close(); err != nil {
		t.Fatal(err)
	}
	if err = state.Close(); err != nil {
		t.Fatal(err)
	}
	release()
	state, err = config.OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	manager = client.NewManager([]config.Environment{f.profile}, state)
	defer manager.Close()
	a, err = manager.NewSession(f.profile.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	r := a.BatchReceipts()
	if len(r) != 1 || r[0].Original != original || r[0].Items[0].State != config.BatchAcknowledged || r[0].Items[1].State != config.BatchNeedsRecovery || r[0].Items[2].State != config.BatchRemainingDraft || r[0].Items[1].OperationID != operation || r[0].Items[1].ThreadID != thread {
		t.Fatalf("restart lost source-bound receipt %+v", r)
	}
	if effects, dispatches := f.counts(); effects != 2 || dispatches != 2 {
		t.Fatalf("restart dispatched remaining work/replayed: %d/%d", effects, dispatches)
	}
	const edited = "newer human list\t\r\n"
	if err = state.SetDraft(r[0].EnvironmentID, r[0].Source, edited); err != nil {
		t.Fatal(err)
	}
	recovered, err := a.RetryLaunch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Next != nil {
		defer recovered.Next.Close()
	}
	r = a.BatchReceipts()
	if r[0].Items[1].State != config.BatchAcknowledged || r[0].Items[1].OperationID != operation || r[0].Items[2].OperationID != "" || state.Draft(r[0].EnvironmentID, r[0].Source) != edited {
		t.Fatal("recovery changed human draft or dispatched C")
	}
	if effects, dispatches := f.counts(); effects != 2 || dispatches != 2 {
		t.Fatalf("same-ID reconciliation duplicated mutation: %d/%d", effects, dispatches)
	}
}

func TestCreationPinActualAdapterRestartResolvesCurrentDefaultsWithoutAttachment(t *testing.T) {
	source := newFixture(t)
	target := newEnvironmentFixture(t, "env-target", "target", "Target")
	path := filepath.Join(t.TempDir(), "state.json")
	state, err := config.OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	manager := client.NewManager([]config.Environment{source.profile, target.profile}, state)
	a, err := manager.NewSession(source.profile.ID, "thread-a")
	if err != nil {
		t.Fatal(err)
	}
	if err = a.PinCreationDestination(target.profile.ID, target.profile.EnvironmentID, "project-a"); err != nil {
		t.Fatal(err)
	}
	if err = manager.Close(); err != nil {
		t.Fatal(err)
	}
	state.Close()
	target.mu.Lock()
	target.catalog["settings"].(object)["defaultModelSelection"] = object{"instanceId": "fixture-provider", "model": "fixture-other", "options": []any{}}
	target.mu.Unlock()
	state, err = config.OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	manager = client.NewManager([]config.Environment{source.profile, target.profile}, state)
	defer manager.Close()
	a, err = manager.NewSession(source.profile.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	launch := featureCommand(a, "new", "")
	p := featurePrompt(t, a)
	if p.Question.LaunchReview == nil || p.Question.LaunchReview.Location.ProfileID != target.profile.ID || p.Question.LaunchReview.Location.ProjectID != "project-a" || p.Question.LaunchReview.Selection.Model != "fixture-other" {
		t.Fatalf("pin captured old defaults or source attachment: %+v", p.Question)
	}
	p.Respond(client.Answer{Cancelled: true})
	if result := featureReply(t, launch); result.err != nil {
		t.Fatal(result.err)
	}
	for _, fixture := range []*fixture{source, target} {
		counts := fixture.requestCounts()
		if counts["rpc:orchestration.subscribeThread"] != 0 || counts["/api/orchestration/threads/thread-a/bounded"] != 0 {
			t.Fatal("New review attached a transcript")
		}
	}
	if effects, dispatches := target.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("pin/review launched work")
	}
}

func TestNavigatorActualAdapterPreviewAndRevisionPinPreserveAttachment(t *testing.T) {
	f := newFixture(t)
	_, state, a, _ := startFeatureSession(t, f, "thread-a")
	const draft = "current A draft\t\r\n"
	if err := a.StoreDraft(draft); err != nil {
		t.Fatal(err)
	}
	page, err := a.NavigatorRead(context.Background(), f.profile.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	before := f.requestCounts()
	preview, err := a.NavigatorPreview(context.Background(), page, "thread-b")
	if err != nil || !preview.Available || preview.Snapshot == nil || preview.ThreadID != "thread-b" || preview.Snapshot.Metadata.Live || len(preview.Snapshot.Metadata.Requests) != 0 {
		t.Fatalf("unsafe/unavailable bounded preview %+v %v", preview, err)
	}
	if a.Status().ThreadID != "thread-a" || a.Draft() != draft || state.Draft("env-a", "thread-a") != draft {
		t.Fatal("preview switched source or lost draft")
	}
	if f.requestCounts()["/ws"] != before["/ws"] {
		t.Fatal("preview acquired a socket/subscription")
	}
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("preview mutated native state")
	}
	result, err := a.NavigatorPin(context.Background(), page, "thread-b")
	if err != nil || !strings.Contains(result.Text, "Pinned") {
		t.Fatalf("pin failed %s %v", result.Text, err)
	}
	if a.Status().ThreadID != "thread-a" || a.Draft() != draft {
		t.Fatal("pin changed attachment")
	}
	if _, err = a.NavigatorPin(context.Background(), page, "thread-b"); err == nil {
		t.Fatal("stale displayed pin revision auto-retried")
	}
	if effects, dispatches := f.counts(); effects != 1 || dispatches != 1 {
		t.Fatalf("pin duplicate dispatch %d/%d", effects, dispatches)
	}
}

func TestNativeDefaultsActualAdapterScopeIsReadOnlyAndPreservesOpaqueSelection(t *testing.T) {
	f := newFixture(t)
	_, _, a, _ := startFeatureSession(t, f, "thread-a")
	for _, scope := range []string{"project", "environment"} {
		v, err := a.NativeDefaults(context.Background(), f.profile.ID, "project-a", scope)
		if err != nil {
			t.Fatal(err)
		}
		if v.Scope != scope || v.WriteSupported || v.WriteReason == "" || v.SettingsRoute == "" || len(v.Fields) == 0 || v.DesktopLocal != "unreported" {
			t.Fatalf("unsafe/missing defaults scope %+v", v)
		}
	}
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("defaults inspection wrote settings")
	}
}

func TestNativeDefaultsActualEditorNamesEachScopeAndRetainsAttachment(t *testing.T) {
	f := newFixture(t)
	_, state, a, _ := startFeatureSession(t, f, "thread-a")
	const draft = "exact source draft\t\r\n  "
	if err := a.StoreDraft(draft); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan featureResult, 1)
	go func() {
		r, err := a.EditNativeDefaults(ctx, f.profile.ID, "project-a")
		result <- featureResult{r, err}
	}()
	for _, step := range []struct{ scope, action string }{{"Project on environment", "scope"}, {"Environment", "refresh"}, {"Environment", "close"}} {
		p := featurePrompt(t, a)
		if p.Question.Surface != client.SurfaceNativeDefaults || !strings.HasPrefix(p.Question.Title, step.scope+" defaults") || !strings.Contains(p.Question.Prompt, "Scope: "+step.scope) || !strings.Contains(p.Question.Prompt, "Environment: env-a") || !strings.Contains(p.Question.Prompt, "Editing unavailable:") || !strings.Contains(p.Question.Prompt, "Native Settings:") {
			t.Fatalf("native scope inspection omitted identity/limitation: %+v", p.Question)
		}
		if len(p.Question.Choices) != 3 || p.Question.Choices[0].ID != "scope" || p.Question.Choices[1].ID != "refresh" || p.Question.Choices[2].ID != "close" {
			t.Fatal("read-only defaults offered an unexpected operation")
		}
		p.Respond(client.Answer{ChoiceIDs: []string{step.action}})
	}
	if reply := featureReply(t, result); reply.err != nil {
		t.Fatal(reply.err)
	}
	if a.Status().EnvironmentID != "env-a" || a.Status().ThreadID != "thread-a" || a.Draft() != draft || state.Draft("env-a", "thread-a") != draft {
		t.Fatal("defaults scope/refresh changed its attached source or exact draft")
	}
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatalf("read-only scope/refresh dispatched a mutation: %d/%d", effects, dispatches)
	}
}
