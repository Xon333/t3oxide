package client

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
)

func clientCatalogFixture() map[string]any {
	return map[string]any{"threadId": "thread", "instanceId": "provider", "providerLabel": "Provider", "available": true, "more": false,
		"current": map[string]any{"instanceId": "provider", "model": "model", "options": []any{map[string]any{"id": "reasoningEffort", "value": "high"}, map[string]any{"id": "opaque", "value": "keep"}}},
		"models": []any{map[string]any{"slug": "model", "name": "Model", "options": []any{map[string]any{"id": "reasoningEffort", "type": "select", "label": "Reasoning", "options": []any{map[string]any{"id": "high", "label": "High"}, map[string]any{"id": "low", "label": "Low"}}}}},
			map[string]any{"slug": "basic", "name": "Basic", "options": []any{}},
			map[string]any{"slug": "fast", "name": "Fast", "options": []any{map[string]any{"id": "fastMode", "type": "boolean", "currentValue": false}}}}}
}
func cloneCatalog(v map[string]any) map[string]any {
	raw, _ := json.Marshal(v)
	var r map[string]any
	json.Unmarshal(raw, &r)
	return r
}
func TestCatalogDecodeValidationAndSafeCrossModelOptionCarry(t *testing.T) {
	c, err := DecodeCatalog(clientCatalogFixture())
	if err != nil {
		t.Fatal(err)
	}
	changed := c.Current.WithOption("reasoningEffort", "low")
	if e := c.validate(changed); e != nil {
		t.Fatal(e)
	}
	if e := c.validate(c.Current.WithOption("opaque", "changed")); e == nil {
		t.Fatal("unknown changed option allowed")
	}
	next, e := c.withModel(c.Current, "basic")
	if e != nil || len(next.Options) != 0 {
		t.Fatalf("unsupported options carried into new model: %+v %v", next, e)
	}
	if e = c.validate(config.ModelSelection{InstanceID: "provider", Model: "fast", Options: []config.ModelOption{{ID: "fastMode", Value: false}}}); e != nil {
		t.Fatal(e)
	}
	if e = c.validate(config.ModelSelection{InstanceID: "other", Model: "model"}); e == nil {
		t.Fatal("provider switching allowed")
	}
}
func TestCatalogRejectsMalformedOrIncompatibleControls(t *testing.T) {
	changes := map[string]func(map[string]any){
		"missing availability": func(v map[string]any) { delete(v, "available") },
		"wrong provider":       func(v map[string]any) { v["instanceId"] = "other" },
		"duplicate model":      func(v map[string]any) { v["models"] = []any{v["models"].([]any)[0], v["models"].([]any)[0]} },
		"unknown descriptor": func(v map[string]any) {
			v["models"].([]any)[0].(map[string]any)["options"] = []any{map[string]any{"id": "x", "type": "range"}}
		},
		"wrong default": func(v map[string]any) {
			v["models"].([]any)[0].(map[string]any)["options"].([]any)[0].(map[string]any)["currentValue"] = "fake"
		},
		"wrong boolean": func(v map[string]any) {
			v["models"].([]any)[2].(map[string]any)["options"].([]any)[0].(map[string]any)["currentValue"] = "false"
		},
	}
	for name, edit := range changes {
		t.Run(name, func(t *testing.T) {
			v := cloneCatalog(clientCatalogFixture())
			edit(v)
			if _, err := DecodeCatalog(v); err == nil {
				t.Fatal("invalid catalog accepted")
			}
		})
	}
}

func TestCatalogHonorsNewConversationModelRestriction(t *testing.T) {
	fixture := clientCatalogFixture()
	fixture["requiresNewThreadForModelChange"] = true
	c, err := DecodeCatalog(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.withModel(c.Current, "basic"); err == nil || !strings.Contains(err.Error(), "new conversation") {
		t.Fatalf("restricted model change offered: %v", err)
	}
	if err := c.validate(c.Current.WithOption("reasoningEffort", "low")); err != nil {
		t.Fatalf("same-model effort change rejected: %v", err)
	}
}
func TestThreadDisplayUsesLineageAndNeverNameHeuristics(t *testing.T) {
	sub, fork, parent := "subagent", "fork", "parent"
	choices := ThreadChoices([]Thread{{ID: "a", Title: "Human subagent title", Status: "idle"},
		{ID: "b", Title: "Worker", Status: "waiting", Lineage: &wire.Lineage{RelationshipToParent: &sub, ParentThreadID: &parent}, Depth: 1},
		{ID: "c", Title: "Fork", Status: "completed", Lineage: &wire.Lineage{RelationshipToParent: &fork, ParentThreadID: &parent}},
		{ID: "d", Title: "Main", Status: "running", Lineage: &wire.Lineage{}, ChildCount: 4, Expanded: true}})
	if !strings.HasPrefix(choices[0].Label, "? ") || !strings.Contains(choices[1].Label, "SUB") || !strings.HasPrefix(choices[2].Label, "FORK") || !strings.Contains(choices[3].Label, "▾ MAIN") {
		t.Fatalf("wrong lineage labels: %+v", choices)
	}
	if !strings.Contains(choices[1].Description, "waiting") || !strings.Contains(choices[1].Description, "parent: parent") {
		t.Fatal("lost authoritative status/parent context")
	}
}
func TestNavigationPreferencesDoNotAliasOrCrossEnvironments(t *testing.T) {
	m := NewManager(nil, nil)
	n := NavigationState{Filter: "subagents", Expanded: map[string]bool{"one": true}}
	m.saveNavigation("a", n)
	n.Expanded["one"] = false
	read := m.navigation("a")
	if !read.Expanded["one"] || read.Filter != "subagents" {
		t.Fatal("navigation settings aliased")
	}
	read.Expanded["one"] = false
	if !m.navigation("a").Expanded["one"] || m.navigation("b").Filter != "all" || len(m.navigation("b").Expanded) != 0 {
		t.Fatal("scope/clone error")
	}
}
func TestThreadListDecodesOptionalLineageWithoutTrustingTypeLabel(t *testing.T) {
	rows := []any{map[string]any{"id": "a", "title": "A", "project": "P", "status": "failed", "model": "M", "threadType": "subagent"}, map[string]any{"id": "b", "title": "B", "project": "P", "status": "completed", "model": "M", "threadType": "subagent", "lineage": map[string]any{"relationshipToParent": "fork", "parentThreadId": "a"}}}
	got, _, err := DecodeThreads(map[string]any{"more": false, "threads": rows})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Lineage.Type() != "unknown" || got[1].Lineage.Type() != "fork" {
		t.Fatal("untrusted type label used over lineage")
	}
}
