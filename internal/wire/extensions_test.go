package wire

import (
	"github.com/Xon333/t3oxide/internal/config"
	"testing"
)

func extensionView() map[string]any {
	v := modelView(1, modelRow("one", "body"))
	v["modelSelection"] = map[string]any{"instanceId": "p", "model": "saved", "options": []any{map[string]any{"id": "reasoningEffort", "value": "high"}}}
	v["lineage"] = map[string]any{"relationshipToParent": "subagent", "parentThreadId": "parent", "rootThreadId": "root"}
	v["runtimeDetails"] = map[string]any{"scope": "active-run", "runId": "run", "status": "running", "providerThreadId": "pt", "providerSessionId": "ps",
		"runModelSelection": map[string]any{"instanceId": "p", "model": "requested"}, "providerReportedSelection": map[string]any{"instanceId": "p", "model": "reported"}, "sessionReportedModel": "session", "providerReportedAt": "2026-10-22T12:00:00Z"}
	return v
}
func TestModelAndLineageMetadataAreDeepClonedAndAuthoritative(t *testing.T) {
	var s Store
	applyOK(t, &s, "view", extensionView())
	v := s.Snapshot()
	if v.Metadata.ModelSelection.Model != "saved" || v.Metadata.RuntimeDetails.ProviderReportedSelection.Model != "reported" || v.Metadata.RuntimeDetails.RunModelSelection.Model != "requested" || v.Metadata.Lineage.Type() != "subagent" {
		t.Fatal("evidence conflated")
	}
	v.Metadata.ModelSelection.Options[0].Value = "mutated"
	*v.Metadata.Lineage.ParentThreadID = "changed"
	v.Metadata.RuntimeDetails.RunModelSelection.Model = "changed"
	v.Metadata.RuntimeDetails.ProviderReportedSelection.Model = "changed"
	*v.Metadata.RuntimeDetails.SessionReportedModel = "changed"
	fresh := s.Snapshot()
	if fresh.Metadata.ModelSelection.Options[0].Value != "high" || *fresh.Metadata.Lineage.ParentThreadID != "parent" || fresh.Metadata.RuntimeDetails.RunModelSelection.Model != "requested" || fresh.Metadata.RuntimeDetails.ProviderReportedSelection.Model != "reported" || *fresh.Metadata.RuntimeDetails.SessionReportedModel != "session" {
		t.Fatal("snapshot extension aliases internal state")
	}
}
func TestModelMetadataReplacementsClearOmittedAndRejectStaleDeltas(t *testing.T) {
	var s Store
	s.Select("thread")
	applyOK(t, &s, "view", extensionView())
	d := modelDelta(1, 2, nil, nil, []string{"thread/one"})
	meta := d["metadata"].(map[string]any)
	meta["modelSelection"] = config.ModelSelection{InstanceID: "p", Model: "new"}
	applyOK(t, &s, "delta", d)
	v := s.Snapshot()
	if v.Metadata.ModelSelection.Model != "new" || v.Metadata.RuntimeDetails != nil || v.Metadata.Lineage != nil {
		t.Fatal("metadata merged stale evidence")
	}
	stale := extensionView()
	if changed, err := s.Apply("view", modelRaw(stale)); changed || err != nil {
		t.Fatal("stale view should be ignored")
	}
	if s.Snapshot().Metadata.ModelSelection.Model != "new" {
		t.Fatal("stale model leaked")
	}
}
func TestMissingLineageIsUnknownNotMainAndMissingRuntimeNotGuessed(t *testing.T) {
	for _, l := range []any{nil, map[string]any{}, map[string]any{"parentThreadId": nil}} {
		var s Store
		v := modelView(1)
		v["lineage"] = l
		applyOK(t, &s, "view", v)
		if s.Snapshot().Metadata.Lineage.Type() != "unknown" || s.Snapshot().Metadata.RuntimeDetails != nil {
			t.Fatal("missing metadata inferred")
		}
	}
	var s Store
	v := modelView(1)
	v["lineage"] = map[string]any{"parentThreadId": nil, "relationshipToParent": nil}
	applyOK(t, &s, "view", v)
	if s.Snapshot().Metadata.Lineage.Type() != "main" {
		t.Fatal("explicit main not recognized")
	}
}
func TestInvalidExtensionFailsAtomically(t *testing.T) {
	for _, field := range []string{"modelSelection", "lineage", "runtimeDetails"} {
		t.Run(field, func(t *testing.T) {
			var s Store
			applyOK(t, &s, "view", extensionView())
			d := modelDelta(1, 2, nil, nil, []string{"thread/one"})
			meta := d["metadata"].(map[string]any)
			switch field {
			case "modelSelection":
				meta[field] = map[string]any{"instanceId": "p", "model": "m", "options": []any{map[string]any{"id": "bad", "value": 7}}}
			case "lineage":
				meta[field] = map[string]any{"relationshipToParent": "subagent", "parentThreadId": "bad\x00parent"}
			case "runtimeDetails":
				meta[field] = map[string]any{"scope": "guessed"}
			}
			if _, err := s.Apply("delta", modelRaw(d)); err == nil {
				t.Fatal("invalid extension accepted")
			}
			if s.Snapshot().Metadata.ModelSelection.Model != "saved" {
				t.Fatal("failed extension changed state")
			}
		})
	}
}
