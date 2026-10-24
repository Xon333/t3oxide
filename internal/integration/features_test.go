package integration_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
)

type featureResult struct {
	result client.Result
	err    error
}

func featureCommand(a *client.Session, name, args string) <-chan featureResult {
	out := make(chan featureResult, 1)
	go func() { r, e := a.Command(context.Background(), name, args); out <- featureResult{r, e} }()
	return out
}
func featureReply(t *testing.T, c <-chan featureResult) featureResult {
	t.Helper()
	select {
	case r := <-c:
		return r
	case <-time.After(8 * time.Second):
		t.Fatal("feature command did not complete")
		return featureResult{}
	}
}
func featurePrompt(t *testing.T, a *client.Session) *client.Prompt {
	t.Helper()
	select {
	case p := <-a.Prompts():
		return p
	case <-time.After(5 * time.Second):
		t.Fatal("feature prompt missing")
		return nil
	}
}
func startFeatureSession(t *testing.T, f *fixture, thread string) (*client.Manager, *config.State, *client.Session, *appStream) {
	t.Helper()
	m, s := newAppManager(t, f)
	a, e := m.NewSession(f.profile.ID, thread)
	if e != nil {
		t.Fatal(e)
	}
	stream := streamSession(t, a)
	stream.until(t, func(e client.Event) bool { return e.Snapshot != nil && e.Snapshot.Metadata.Live })
	return m, s, a, stream
}
func featureOK(t *testing.T, a *client.Session, name, args string) client.Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, e := a.Command(ctx, name, args)
	if e != nil {
		t.Fatalf("/%s %s: %v", name, args, e)
	}
	return r
}
func fixtureOption(f *fixture, thread, id string) any {
	f.mu.Lock()
	defer f.mu.Unlock()
	sel := f.threads[thread]["thread"].(object)["modelSelection"].(object)
	opts, _ := sel["options"].([]any)
	for _, raw := range opts {
		option := raw.(object)
		if option["id"] == id {
			return option["value"]
		}
	}
	return nil
}
func waitCommandType(t *testing.T, f *fixture, typ string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		id := ""
		for key, c := range f.commands {
			if c["type"] == typ {
				id = key
				break
			}
		}
		f.mu.Unlock()
		if id != "" {
			return id
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("command not dispatched: " + typ)
	return ""
}

func TestFeaturesActualAdapterModelEffortTierAndDefaultThreadScope(t *testing.T) {
	f := newFixture(t)
	_, state, a, stream := startFeatureSession(t, f, "thread-a")
	c, err := a.Catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Models) != 4 || c.InstanceID != "fixture-provider" {
		t.Fatalf("wrong catalog: %+v", c)
	}
	choose := featureCommand(a, "model", "")
	p := featurePrompt(t, a)
	if !strings.Contains(p.Question.Title, "Current thread") || p.Question.Initial != "fixture-model" || len(p.Question.Choices) != 4 {
		t.Fatal("model picker not grounded in thread/catalog")
	}
	if !p.Respond(client.Answer{ChoiceIDs: []string{"fixture-other"}}) {
		t.Fatal("picker reply refused")
	}
	if r := featureReply(t, choose); r.err != nil {
		t.Fatal(r.err)
	}
	stream.until(t, func(e client.Event) bool {
		return e.Snapshot != nil && e.Snapshot.Metadata.ModelSelection != nil && e.Snapshot.Metadata.ModelSelection.Model == "fixture-other" && e.Snapshot.Metadata.Live
	})
	featureOK(t, a, "effort", "high")
	featureOK(t, a, "speed", "fast")
	if fixtureOption(f, "thread-a", "reasoningEffort") != "high" || fixtureOption(f, "thread-a", "serviceTier") != "fast" {
		t.Fatal("effort/tier not dispatched through actual adapter")
	}
	if state.Pending("env-a", false) != nil {
		t.Fatal("accepted selection retained recovery record")
	}
	stream.until(t, func(e client.Event) bool {
		return strings.Contains(e.Status.ModelName, "high effort") && strings.Contains(e.Status.ModelName, "fast tier")
	})
	featureOK(t, a, "effort", "default")
	if fixtureOption(f, "thread-a", "reasoningEffort") != nil {
		t.Fatal("default should remove explicit selection")
	}
	if _, e := a.Command(context.Background(), "effort", "fabricated"); e == nil {
		t.Fatal("unadvertised effort accepted")
	}
	f.mu.Lock()
	effects := f.effects
	f.mu.Unlock()
	if effects != 4 {
		t.Fatalf("unexpected mutation count %d", effects)
	}
}
func TestFeaturesBooleanSpeedAndUnsupportedEffort(t *testing.T) {
	f := newFixture(t)
	_, _, a, _ := startFeatureSession(t, f, "thread-a")
	featureOK(t, a, "model", "fixture-boolean")
	featureOK(t, a, "speed", "on")
	if fixtureOption(f, "thread-a", "fastMode") != true {
		t.Fatal("boolean speed not applied")
	}
	featureOK(t, a, "speed", "off")
	if fixtureOption(f, "thread-a", "fastMode") != false {
		t.Fatal("false speed dropped")
	}
	if _, e := a.Command(context.Background(), "effort", "high"); e == nil {
		t.Fatal("unsupported control fabricated")
	}
}

func TestFeaturesProviderModelRestrictionPreservesEffortAndDraftOverride(t *testing.T) {
	f := newFixture(t)
	f.mu.Lock()
	f.catalog["providers"].([]any)[0].(object)["requiresNewThreadForModelChange"] = true
	f.mu.Unlock()
	_, state, a, _ := startFeatureSession(t, f, "thread-a")
	for _, args := range []string{"", "fixture-other", "next fixture-other"} {
		if _, err := a.Command(context.Background(), "model", args); err == nil || !strings.Contains(err.Error(), "new conversation") {
			t.Fatalf("restricted model choice %q offered: %v", args, err)
		}
	}
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 || state.Override("env-a", "thread-a") != nil {
		t.Fatal("restricted model choice changed T3 or staged a draft override")
	}
	featureOK(t, a, "effort", "high")
	featureOK(t, a, "speed", "fast")
	if fixtureOption(f, "thread-a", "reasoningEffort") != "high" || fixtureOption(f, "thread-a", "serviceTier") != "fast" {
		t.Fatal("provider restriction disabled compatible same-model options")
	}
}
func TestFeaturesStalePickerRefusedWithoutChangingNewerThreadSelection(t *testing.T) {
	f := newFixture(t)
	_, state, a, _ := startFeatureSession(t, f, "thread-a")
	choose := featureCommand(a, "model", "")
	p := featurePrompt(t, a)
	f.mu.Lock()
	f.threads["thread-a"]["thread"].(object)["modelSelection"] = object{"instanceId": "fixture-provider", "model": "fixture-basic"}
	f.mu.Unlock()
	p.Respond(client.Answer{ChoiceIDs: []string{"fixture-other"}})
	r := featureReply(t, choose)
	if r.err == nil || !strings.Contains(r.err.Error(), "changed") {
		t.Fatalf("stale picker accepted: %v", r.err)
	}
	if e, d := f.counts(); e != 0 || d != 0 {
		t.Fatal("stale choice reached upstream mutation")
	}
	if state.Pending("env-a", false) != nil {
		t.Fatal("definitely rejected first choice retained pending")
	}
}
func TestFeaturesFavoritesAreEnvironmentScopedAndRevalidate(t *testing.T) {
	f := newFixture(t)
	_, state, a, _ := startFeatureSession(t, f, "thread-a")
	featureOK(t, a, "effort", "high")
	featureOK(t, a, "speed", "fast")
	featureOK(t, a, "presets", "save Focus")
	presets := state.Presets("env-a")
	if len(presets) != 1 || presets[0].Name != "Focus" || len(state.Presets("env-b")) != 0 {
		t.Fatal("favorite persistence/scope")
	}
	model := featureCommand(a, "model", "")
	p := featurePrompt(t, a)
	star := false
	for _, c := range p.Question.Choices {
		if c.ID == "fixture-model" && strings.HasPrefix(c.Label, "★") {
			star = true
		}
	}
	if !star {
		t.Fatal("favorite not marked in model picker")
	}
	p.Respond(client.Answer{Cancelled: true})
	if r := featureReply(t, model); r.err != nil {
		t.Fatal(r.err)
	}
	featureOK(t, a, "model", "fixture-other")
	featureOK(t, a, "presets", "Focus")
	if fixtureOption(f, "thread-a", "reasoningEffort") != "high" || fixtureOption(f, "thread-a", "serviceTier") != "fast" {
		t.Fatal("preset did not restore complete selection")
	}
	f.mu.Lock()
	f.catalog["providers"].([]any)[0].(object)["models"] = []any{}
	before := f.effects
	f.mu.Unlock()
	if _, e := a.Command(context.Background(), "presets", "Focus"); e == nil {
		t.Fatal("unavailable preset accepted")
	}
	if effects, _ := f.counts(); effects != before {
		t.Fatal("invalid favorite mutated T3")
	}
}
func TestFeaturesOneDispatchOverrideIsStagedIsolatedAndAdoptedOnlyByT3(t *testing.T) {
	f := newFixture(t)
	m, state, a, stream := startFeatureSession(t, f, "thread-a")
	featureOK(t, a, "model", "next fixture-other")
	featureOK(t, a, "effort", "next high")
	featureOK(t, a, "speed", "next fast")
	staged := state.Override("env-a", "thread-a")
	if staged == nil || staged.Selection.Model != "fixture-other" {
		t.Fatal("override not staged")
	}
	if e, d := f.counts(); e != 0 || d != 0 {
		t.Fatal("staging changed server")
	}
	if state.Override("env-a", "thread-b") != nil || state.Override("env-b", "thread-a") != nil {
		t.Fatal("override leaked scope")
	}
	b, err := m.NewSession("fixture", "thread-b")
	if err != nil {
		t.Fatal(err)
	}
	if b.Status().NextModel != "" {
		t.Fatal("override visible on other thread")
	}
	b.Close()
	if err = a.SetDraft("one override message"); err != nil {
		t.Fatal(err)
	}
	if err = a.Send(context.Background(), "one override message"); err != nil {
		t.Fatal(err)
	}
	if state.Override("env-a", "thread-a") != nil {
		t.Fatal("exact accepted override not cleared")
	}
	stream.until(t, func(e client.Event) bool {
		return e.Snapshot != nil && e.Snapshot.Metadata.ModelSelection != nil && e.Snapshot.Metadata.ModelSelection.Model == "fixture-other"
	})
	if fixtureOption(f, "thread-a", "reasoningEffort") != "high" {
		t.Fatal("server adoption of override hidden by client restore")
	}
	if effects, _ := f.counts(); effects != 1 {
		t.Fatal("client performed a hidden restoring mutation")
	}
}
func TestFeaturesUncertainModelWriteKeepsExactReceiptAndStopPriority(t *testing.T) {
	f := newFixture(t)
	_, state, a, stream := startFeatureSession(t, f, "thread-a")
	f.mu.Lock()
	f.dropSelections = true
	f.mu.Unlock()
	change := featureCommand(a, "effort", "high")
	id := waitCommandType(t, f, "thread.model-selection.set")
	if state.Pending("env-a", false)["id"] != id {
		t.Fatal("write not persisted before dispatch")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	err := a.Stop(ctx)
	cancel()
	if err != nil {
		t.Fatalf("Stop blocked by model change: %v", err)
	}
	f.closeSockets()
	if r := featureReply(t, change); r.err == nil {
		t.Fatal("lost model acknowledgment claimed success")
	}
	stream.until(t, func(e client.Event) bool {
		return e.Snapshot != nil && e.Snapshot.Metadata.Live && strings.Contains(e.Status.ModelName, "high effort")
	})
	if effects, dispatches := f.counts(); effects != 2 || dispatches != 2 {
		t.Fatalf("automatic retry occurred: %d/%d", effects, dispatches)
	}
	f.mu.Lock()
	reads := f.catalogReads
	f.catalog["providers"] = []any{}
	f.dropSelections = false
	f.mu.Unlock()
	featureOK(t, a, "retry", "")
	if state.Pending("env-a", false) != nil {
		t.Fatal("retry did not clear exact acknowledged operation")
	}
	f.mu.Lock()
	afterReads := f.catalogReads
	original := clone(f.commands[id])
	f.mu.Unlock()
	if reads != afterReads {
		t.Fatal("retry revalidated against changed catalog")
	}
	if original["modelSelection"].(object)["options"].([]any)[0].(object)["value"] != "high" {
		t.Fatal("retry changed selection")
	}
	if effects, dispatches := f.counts(); effects != 2 || dispatches != 3 {
		t.Fatalf("duplicate effects %d/%d", effects, dispatches)
	}
}
func TestFeaturesUncertainOverrideSendKeepsNewerDraftOverrideAfterExplicitRetry(t *testing.T) {
	f := newFixture(t)
	_, state, a, stream := startFeatureSession(t, f, "thread-a")
	featureOK(t, a, "effort", "next high")
	old := state.Override("env-a", "thread-a")
	a.SetDraft("override uncertain")
	f.mu.Lock()
	f.dropMessages = true
	f.mu.Unlock()
	send := make(chan error, 1)
	go func() { send <- a.Send(context.Background(), "override uncertain") }()
	id := f.waitMessage(t, "override uncertain")
	f.closeSockets()
	select {
	case e := <-send:
		if e == nil {
			t.Fatal("lost ack reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("send did not settle")
	}
	if state.Override("env-a", "thread-a").ID != old.ID {
		t.Fatal("uncertain override removed")
	}
	stream.until(t, func(e client.Event) bool {
		return e.Snapshot != nil && e.Snapshot.Metadata.Live && containsMessage(e, "thread-a/"+id+"-reply", "Response")
	})
	featureOK(t, a, "effort", "next low")
	newer := state.Override("env-a", "thread-a")
	if newer.ID == old.ID {
		t.Fatal("override identity was reused")
	}
	a.SetDraft("new draft")
	f.mu.Lock()
	f.dropMessages = false
	f.mu.Unlock()
	featureOK(t, a, "retry", "")
	if state.Override("env-a", "thread-a").ID != newer.ID || a.Draft() != "new draft" {
		t.Fatal("acknowledged old override erased newer draft/selection")
	}
	if effects, dispatches := f.counts(); effects != 1 || dispatches != 2 {
		t.Fatalf("override replay effects %d/%d", effects, dispatches)
	}
}
func TestFeaturesRuntimeDetailsRefreshFromProviderEventsAndRemainDistinct(t *testing.T) {
	f := newFixture(t)
	f.mu.Lock()
	p := f.threads["thread-a"]
	p["runs"].([]any)[0].(object)["providerThreadId"] = "provider-thread"
	p["runs"].([]any)[0].(object)["modelSelection"] = object{"instanceId": "fixture-provider", "model": "requested-model"}
	p["providerThreads"] = []any{object{"id": "provider-thread", "providerSessionId": "provider-session", "nativeMetadata": object{"modelSelection": object{"instanceId": "fixture-provider", "model": "reported-model", "options": []any{object{"id": "reasoningEffort", "value": "low"}}}, "updatedAt": "2026-10-22T12:00:00Z"}}}
	p["providerSessions"] = []any{object{"id": "provider-session", "model": "session-model"}}
	f.mu.Unlock()
	_, _, a, stream := startFeatureSession(t, f, "thread-a")
	r := featureOK(t, a, "runtime", "")
	for _, term := range []string{"fixture-model", "requested-model", "reported-model", "session-model", "not proof"} {
		if !strings.Contains(r.Detail, term) {
			t.Fatalf("missing runtime provenance %s", term)
		}
	}
	f.mu.Lock()
	pt := p["providerThreads"].([]any)[0].(object)
	pt["nativeMetadata"].(object)["modelSelection"] = object{"instanceId": "fixture-provider", "model": "fresh-provider-report"}
	copy := clone(pt)
	f.mu.Unlock()
	f.broadcast("thread-a", "provider-thread.updated", copy, 0)
	stream.until(t, func(e client.Event) bool {
		return e.Snapshot != nil && e.Snapshot.Metadata.RuntimeDetails != nil && e.Snapshot.Metadata.RuntimeDetails.ProviderReportedSelection != nil && e.Snapshot.Metadata.RuntimeDetails.ProviderReportedSelection.Model == "fresh-provider-report"
	})
	if !strings.Contains(a.Status().ModelName, "fixture-model") {
		t.Fatal("provider report overwrote saved preference status")
	}
	featureOK(t, a, "earlier", "")
	stream.until(t, func(e client.Event) bool { return e.Snapshot != nil && e.Snapshot.Metadata.Historical })
	if r = featureOK(t, a, "runtime", ""); !strings.Contains(r.Detail, "cached or historical") {
		t.Fatal("historical details presented as live")
	}
}
func addFeatureFamily(f *fixture) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, item := range []struct{ id, parent, relationship, status string }{{"child", "thread-a", "subagent", "waiting"}, {"grandchild", "child", "subagent", "completed"}, {"fork", "thread-a", "fork", "failed"}} {
		p := projection(item.id, item.id)
		p["thread"].(object)["lineage"] = object{"relationshipToParent": item.relationship, "parentThreadId": item.parent, "rootThreadId": "thread-a"}
		p["thread"].(object)["status"] = item.status
		f.threads[item.id] = p
	}
}
func TestFeaturesGroupedNavigatorExpandsFiltersAndJumpsToParent(t *testing.T) {
	f := newFixture(t)
	addFeatureFamily(f)
	_, _, a, stream := startFeatureSession(t, f, "thread-a")
	threads, err := a.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 3 {
		t.Fatalf("subagents not initially collapsed: %+v", threads)
	}
	nav := featureCommand(a, "threads", "")
	p := featurePrompt(t, a)
	if !p.Question.Navigation {
		t.Fatal("flat picker used")
	}
	for _, step := range []struct {
		action   string
		expanded bool
	}{{"expand", true}, {"expand", true}, {"collapse", false}, {"collapse", false}, {"toggle", true}} {
		p.Respond(client.Answer{Action: step.action, ChoiceIDs: []string{offeredThreadID(t, p, "thread-a")}})
		p = featurePrompt(t, a)
		hasChild := false
		for _, c := range p.Question.Choices {
			if offeredThreadValue(t, c.ID) == "child" {
				hasChild = strings.Contains(c.Label, "SUB") && strings.Contains(c.Description, "waiting")
			}
		}
		if hasChild != step.expanded {
			t.Fatalf("%s did not preserve expected expansion: %+v", step.action, p.Question.Choices)
		}
	}
	p.Respond(client.Answer{ChoiceIDs: []string{offeredThreadID(t, p, "child")}})
	r := featureReply(t, nav)
	if r.err != nil || r.result.Next == nil {
		t.Fatalf("open child: %v", r.err)
	}
	stream.stop(t)
	child := r.result.Next
	childStream := streamSession(t, child)
	childStream.until(t, func(e client.Event) bool { return e.Snapshot != nil && e.Snapshot.Metadata.Live })
	if !strings.Contains(child.Status().Context, "SUBAGENT") {
		t.Fatal("conversation type absent from context")
	}
	parent, err := child.Parent(context.Background())
	if err != nil || parent.Status().ThreadID != "thread-a" {
		t.Fatalf("parent jump: %v", err)
	}
	parent.Close()
	filter := featureCommand(child, "filter", "forks")
	p = featurePrompt(t, child)
	if len(p.Question.Choices) != 1 || offeredThreadValue(t, p.Question.Choices[0].ID) != "fork" || !strings.Contains(p.Question.Choices[0].Label, "FORK") {
		t.Fatal("forks conflated with subagents")
	}
	p.Respond(client.Answer{Cancelled: true})
	featureReply(t, filter)
	if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
		t.Fatal("navigation mutated T3")
	}
}
func TestFeaturesNavigatorSearchRevealsHiddenSubagentsAndParentContext(t *testing.T) {
	f := newFixture(t)
	addFeatureFamily(f)
	_, _, a, _ := startFeatureSession(t, f, "thread-a")
	nav := featureCommand(a, "threads", "")
	p := featurePrompt(t, a)
	p.Respond(client.Answer{Action: "search"})
	p = featurePrompt(t, a)
	p.Respond(client.Answer{Text: "grandchild"})
	p = featurePrompt(t, a)
	found := map[string]client.Choice{}
	for _, c := range p.Question.Choices {
		found[offeredThreadValue(t, c.ID)] = c
	}
	if len(found) != 3 || !strings.Contains(found["thread-a"].Description, "search context") || !strings.Contains(found["child"].Description, "search context") {
		t.Fatalf("search lost ancestry: %+v", found)
	}
	p.Respond(client.Answer{Action: "parent", ChoiceIDs: []string{offeredThreadID(t, p, "grandchild")}})
	r := featureReply(t, nav)
	if r.err != nil || r.result.Next.Status().ThreadID != "child" {
		t.Fatalf("search parent jump failed: %v", r.err)
	}
	r.result.Next.Close()
}
func TestFeaturesCancelledPickerCannotLeakIntoAnotherThread(t *testing.T) {
	f := newFixture(t)
	m, _, a, stream := startFeatureSession(t, f, "thread-a")
	choose := featureCommand(a, "model", "")
	p := featurePrompt(t, a)
	stream.stop(t)
	if r := featureReply(t, choose); r.err == nil {
		t.Fatal("detached picker command not cancelled")
	}
	if p.Respond(client.Answer{ChoiceIDs: []string{"fixture-other"}}) {
		t.Fatal("retired picker accepted late reply")
	}
	b, err := m.NewSession("fixture", "thread-b")
	if err != nil {
		t.Fatal(err)
	}
	bs := streamSession(t, b)
	bs.until(t, func(e client.Event) bool { return e.Snapshot != nil && e.Snapshot.Metadata.Live })
	if effects, _ := f.counts(); effects != 0 {
		t.Fatal("late model choice mutated another thread")
	}
}
func TestFeaturesLaunchAcknowledgesActualAdapterReceiptAndRetainsSourceDraft(t *testing.T) {
	f := newFixture(t)
	_, state, a, _ := startFeatureSession(t, f, "thread-a")
	if e := a.SetDraft("source composer"); e != nil {
		t.Fatal(e)
	}
	launch := featureCommand(a, "new", "New disposable fixture")
	p := featurePrompt(t, a)
	p.Respond(client.Answer{Action: "create", Launch: &client.LaunchInput{Title: p.Question.LaunchReview.Input.Title, Message: "first new message"}})
	r := featureReply(t, launch)
	if r.err != nil || r.result.Next == nil {
		t.Fatalf("launch acknowledgment: %v", r.err)
	}
	if state.Pending("env-a", false) != nil || state.Draft("env-a", "\x00new/thread-a") != "" || a.Draft() != "source composer" {
		t.Fatal("launch recovery record/draft mismatch")
	}
	raw, _ := json.Marshal(r.result.Next.Status())
	if !strings.Contains(string(raw), "ThreadID") {
		t.Fatal("new conversation target missing")
	}
	r.result.Next.Close()
}

func TestFeaturesTierDefaultClearsLegacySpeedFallback(t *testing.T) {
	f := newFixture(t)
	f.mu.Lock()
	f.threads["thread-a"]["thread"].(object)["modelSelection"] = object{"instanceId": "fixture-provider", "model": "fixture-model", "options": []any{object{"id": "fastMode", "value": true}}}
	f.mu.Unlock()
	_, state, a, _ := startFeatureSession(t, f, "thread-a")
	featureOK(t, a, "speed", "default")
	if fixtureOption(f, "thread-a", "fastMode") != nil || fixtureOption(f, "thread-a", "serviceTier") != nil {
		t.Fatal("provider default retained a hidden legacy speed override")
	}
	featureOK(t, a, "presets", "save Normal")
	if presets := state.Presets("env-a"); len(presets) != 1 || len(presets[0].Selection.Options) != 0 {
		t.Fatal("preset save retained stale options after acknowledged tier reset")
	}
	featureOK(t, a, "speed", "fast")
	featureOK(t, a, "presets", "apply Normal")
	if fixtureOption(f, "thread-a", "serviceTier") != nil {
		t.Fatal("explicit preset apply did not restore saved selection")
	}
}
