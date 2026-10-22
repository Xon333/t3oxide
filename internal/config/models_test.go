package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func modelSelectionFixture() ModelSelection {
	return ModelSelection{InstanceID: "provider", Model: "model", Options: []ModelOption{{ID: "reasoningEffort", Value: "high"}, {ID: "serviceTier", Value: "fast"}}}
}
func openModelState(t *testing.T) *State {
	t.Helper()
	s, e := OpenState(filepath.Join(t.TempDir(), "state.json"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func mustState(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func TestModelSelectionsValidateWithoutInferringRuntime(t *testing.T) {
	s := modelSelectionFixture()
	mustState(t, s.Validate())
	copy := s.Clone()
	copy.Options[0].Value = "low"
	if s.Options[0].Value != "high" {
		t.Fatal("clone aliases original")
	}
	if !s.Equal(ModelSelection{InstanceID: s.InstanceID, Model: s.Model, Options: []ModelOption{s.Options[1], s.Options[0]}}) {
		t.Fatal("order changes semantic equality")
	}
	if s.Equal(copy) {
		t.Fatal("different choices compare equal")
	}
	if s.WithOption("reasoningEffort", nil).Summary() != "model · default effort · fast tier" {
		t.Fatal("default summary guesses explicit effort")
	}
	for _, bad := range []ModelSelection{
		{}, {InstanceID: "p", Model: " bad"}, {InstanceID: "p", Model: strings.Repeat("漢", 86)},
		{InstanceID: "p", Model: "m", Options: []ModelOption{{ID: "x", Value: 1}}},
		{InstanceID: "p", Model: "m", Options: []ModelOption{{ID: "x", Value: true}, {ID: "x", Value: false}}},
		{InstanceID: "p", Model: "m", Options: []ModelOption{{ID: "x", Value: "bad\x1bdata"}}},
	} {
		if bad.Validate() == nil {
			t.Fatalf("invalid accepted: %#v", bad)
		}
	}
}
func TestModelPreferencesPersistAndStayScopedAndUnaliased(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := OpenState(path)
	mustState(t, err)
	selected := modelSelectionFixture()
	mustState(t, s.SetOverride("env", "thread", &ModelOverride{ID: "one", Selection: selected}))
	mustState(t, s.SavePreset("env", "Focused", selected))
	selected.Options[0].Value = "mutated"
	if s.Override("env", "thread").Selection.Options[0].Value != "high" {
		t.Fatal("SetOverride retained caller slice")
	}
	if s.Override("env2", "thread") != nil || s.Override("env", "other") != nil || len(s.Presets("env2")) != 0 {
		t.Fatal("preferences crossed scope")
	}
	o := s.Override("env", "thread")
	o.Selection.Options[0].Value = "mutated"
	p := s.Presets("env")
	p[0].Selection.Options[0].Value = "mutated"
	if s.Override("env", "thread").Selection.Options[0].Value != "high" || s.Presets("env")[0].Selection.Options[0].Value != "high" {
		t.Fatal("read accessor aliases state")
	}
	mustState(t, s.Close())
	s, err = OpenState(path)
	mustState(t, err)
	defer s.Close()
	if s.Override("env", "thread").ID != "one" || s.Presets("env")[0].Name != "Focused" {
		t.Fatal("preferences did not persist")
	}
	mustState(t, s.SetOverride("env", "thread", nil))
	mustState(t, s.DeletePreset("env", "Focused"))
	if s.Override("env", "thread") != nil || len(s.Presets("env")) != 0 {
		t.Fatal("preference not removed")
	}
}
func TestModelPresetBoundsAndFailedSavesRetainExistingState(t *testing.T) {
	s := openModelState(t)
	sel := modelSelectionFixture()
	mustState(t, s.SavePreset("env", "Name", sel))
	if s.SavePreset("env", "name", sel) == nil {
		t.Fatal("duplicate case-folded name accepted")
	}
	for i := 1; i < 32; i++ {
		mustState(t, s.SavePreset("env", fmt.Sprint(i), sel))
	}
	if s.SavePreset("env", "overflow", sel) == nil || len(s.Presets("env")) != 32 {
		t.Fatal("preset bound")
	}
	mustState(t, s.SetOverride("env", "thread", &ModelOverride{ID: "keep", Selection: sel}))
	for i := 1; i < 40; i++ {
		mustState(t, s.SetOverride("env", fmt.Sprint(i), &ModelOverride{ID: "keep", Selection: sel}))
	}
	if s.SetOverride("env", "overflow", &ModelOverride{ID: "extra", Selection: sel}) == nil || s.Override("env", "thread") == nil {
		t.Fatal("override bound or failed commit corrupted state")
	}
}
func TestModelOverrideClearsOnlyExactAcknowledgedSend(t *testing.T) {
	for _, newer := range []bool{false, true} {
		t.Run(fmt.Sprint(newer), func(t *testing.T) {
			s := openModelState(t)
			sel := modelSelectionFixture()
			mustState(t, s.SetDraft("env", "thread", "hello"))
			mustState(t, s.SetOverride("env", "thread", &ModelOverride{ID: "old-override", Selection: sel}))
			action := savedOperation("send-id", "send")
			action["modelSelection"] = sel
			action["modelOverrideId"] = "old-override"
			mustState(t, s.SavePending("env", false, action))
			if newer {
				mustState(t, s.SetOverride("env", "thread", &ModelOverride{ID: "new-override", Selection: sel.WithOption("reasoningEffort", "low")}))
				mustState(t, s.SetDraft("env", "thread", "new draft"))
			}
			if s.Acknowledge("env", false, "wrong-id", "thread", "hello") == nil {
				t.Fatal("wrong ack accepted")
			}
			if s.Override("env", "thread") == nil {
				t.Fatal("unacknowledged override lost")
			}
			mustState(t, s.Acknowledge("env", false, "send-id", "thread", "hello"))
			if newer {
				if s.Override("env", "thread") == nil || s.Draft("env", "thread") != "new draft" {
					t.Fatal("ack discarded later draft/selection")
				}
			} else if s.Override("env", "thread") != nil || s.Draft("env", "thread") != "" {
				t.Fatal("accepted draft selection not cleared")
			}
		})
	}
}
func TestModelMutationAndStopDoNotClearMessageOverride(t *testing.T) {
	s := openModelState(t)
	sel := modelSelectionFixture()
	mustState(t, s.SetOverride("env", "thread", &ModelOverride{ID: "next", Selection: sel}))
	for _, op := range []string{"select-model", "stop"} {
		a := savedOperation(op, op)
		a["modelSelection"] = sel
		a["expectedModelSelection"] = sel
		mustState(t, s.SavePending("env", op == "stop", a))
		mustState(t, s.Acknowledge("env", op == "stop", op, "thread", ""))
		if s.Override("env", "thread") == nil {
			t.Fatal("non-send cleared override")
		}
	}
}
func TestLaunchAcknowledgmentUsesSourceThreadAndIsolatedDraft(t *testing.T) {
	s := openModelState(t)
	mustState(t, s.SetDraft("env", "thread", "source draft"))
	mustState(t, s.SetDraft("env", "\x00new/thread", "hello"))
	a := savedOperation("launch", "launch")
	mustState(t, s.SavePending("env", false, a))
	mustState(t, s.Acknowledge("env", false, "launch", "thread", "hello"))
	if s.Pending("env", false) != nil || s.Draft("env", "\x00new/thread") != "" || s.Draft("env", "thread") != "source draft" {
		t.Fatal("launch acknowledgment failed or cleared the source composer")
	}
}
func TestClearingDraftOverrideNeverAltersSavedUncertainCommand(t *testing.T) {
	s := openModelState(t)
	sel := modelSelectionFixture()
	a := savedOperation("uncertain", "send")
	a["modelSelection"] = sel
	a["modelOverrideId"] = "override"
	mustState(t, s.SetOverride("env", "thread", &ModelOverride{ID: "override", Selection: sel}))
	mustState(t, s.SavePending("env", false, a))
	mustState(t, s.SetOverride("env", "thread", nil))
	saved := s.Pending("env", false)
	if saved["id"] != "uncertain" || saved["modelOverrideId"] != "override" || saved["modelSelection"] == nil {
		t.Fatal("local override edit changed durable operation")
	}
}
