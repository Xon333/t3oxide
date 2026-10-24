package client

import (
	"context"
	"strings"
	"testing"

	"github.com/Xon333/t3oxide/internal/config"
)

const modelSettingsResponse = `out={'result': {'threadId': 'thread', 'instanceId': 'provider', 'providerLabel': 'Provider', 'available': True, 'more': False, 'requiresNewThreadForModelChange': False, 'current': {'instanceId': 'provider', 'model': 'model', 'options': [{'id': 'reasoningEffort', 'value': 'high'}]}, 'models': [{'slug': 'model', 'name': 'Model', 'options': [{'id': 'reasoningEffort', 'type': 'select', 'label': 'Reasoning', 'currentValue': 'high', 'options': [{'id': 'high', 'label': 'High'}, {'id': 'low', 'label': 'Low'}]}]}]}} if p['op'] == 'models' else {'result': {'sequence': 10}}`

func settingsAuditSession(t *testing.T, response string) (*Session, string) {
	t.Helper()
	a, _, path := recoverySession(t, response)
	a.mu.Lock()
	a.features = map[string]bool{"modelCatalog": true, "modelSelection": true}
	a.mu.Unlock()
	return a, path
}

func TestCombinedSettingsStagesAndAppliesOneMutation(t *testing.T) {
	a, path := settingsAuditSession(t, modelSettingsResponse)
	a.asker = recoveryAsker(func(_ context.Context, q Question) (Answer, error) {
		if q.ModelSettings == nil || q.ModelSettings.Scope != "Current thread" || q.ModelSettings.Location.ThreadID != "thread" || len(q.ModelSettings.Catalog.Models) != 1 {
			t.Fatalf("combined settings review missing advertised catalog: %#v", q.ModelSettings)
		}
		selection := q.ModelSettings.Selection.WithOption("reasoningEffort", "low")
		return Answer{Action: "apply", ModelSettings: &selection}, nil
	})
	if _, err := a.settings(context.Background()); err != nil {
		t.Fatal(err)
	}
	mutations := 0
	for _, packet := range requestsFromLog(t, path) {
		if packet["op"] == "select-model" {
			mutations++
			selection := packet["modelSelection"].(map[string]any)
			options := selection["options"].([]any)
			if options[0].(map[string]any)["value"] != "low" {
				t.Fatalf("wrong staged selection applied: %#v", selection)
			}
		}
	}
	if mutations != 1 {
		t.Fatalf("Apply sent %d model mutations, want one", mutations)
	}
}

func TestCombinedSettingsCancelSendsNoMutation(t *testing.T) {
	a, path := settingsAuditSession(t, modelSettingsResponse)
	a.asker = recoveryAsker(func(_ context.Context, q Question) (Answer, error) {
		if q.ModelSettings == nil || len(q.ModelSettings.Actions) != 2 {
			t.Fatalf("settings actions missing: %#v", q.ModelSettings)
		}
		return Answer{Action: "cancel", Cancelled: true}, nil
	})
	if _, err := a.settings(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, packet := range requestsFromLog(t, path) {
		if packet["op"] == "select-model" {
			t.Fatal("Cancel mutated the saved model")
		}
	}
}

func TestCombinedSettingsRejectsChangedSavedSelection(t *testing.T) {
	changed := strings.ReplaceAll(modelSettingsResponse, "'value': 'high'", "'value': ('high' if count == 1 else 'low')")
	changed = strings.ReplaceAll(changed, "'currentValue': 'high'", "'currentValue': ('high' if count == 1 else 'low')")
	a, path := settingsAuditSession(t, changed)
	a.asker = recoveryAsker(func(_ context.Context, q Question) (Answer, error) {
		selection := q.ModelSettings.Selection.WithOption("reasoningEffort", "low")
		return Answer{Action: "apply", ModelSettings: &selection}, nil
	})
	_, err := a.settings(context.Background())
	if err == nil || !strings.Contains(err.Error(), "refresh settings") {
		t.Fatalf("intervening saved selection change did not require refresh: %v", err)
	}
	for _, packet := range requestsFromLog(t, path) {
		if packet["op"] == "select-model" {
			t.Fatal("changed saved selection was overwritten")
		}
	}
}

func TestPresetListSaveAndRemoveWorkOfflineWithExactNames(t *testing.T) {
	m, state, env := recoveryManager(t)
	a, err := m.NewSession(env.ID, "thread")
	if err != nil {
		t.Fatal(err)
	}
	selection := config.ModelSelection{InstanceID: "provider", Model: "model", Options: []config.ModelOption{{ID: "reasoningEffort", Value: "high"}}}
	if err = state.SetOverride(env.EnvironmentID, "thread", &config.ModelOverride{ID: "local", Selection: selection}); err != nil {
		t.Fatal(err)
	}
	if _, err = a.presets(context.Background(), "next save Offline name"); err != nil {
		t.Fatal(err)
	}
	if result, err := a.presets(context.Background(), "list"); err != nil || !strings.Contains(result.Text, "Offline name · model · high effort") {
		t.Fatalf("offline local list failed: %#v %v", result, err)
	}
	if result, err := a.presets(context.Background(), "remove Offline"); err != nil || !strings.Contains(result.Text, "no changes made") || len(state.Presets(env.EnvironmentID)) != 1 {
		t.Fatalf("unknown remove was not an exact no-op: %#v %v", result, err)
	}
	if result, err := a.presets(context.Background(), "remove"); err != nil || !strings.Contains(result.Text, "/presets remove Offline name") || len(state.Presets(env.EnvironmentID)) != 1 {
		t.Fatalf("name-less remove was not explicit: %#v %v", result, err)
	}
	if _, err = a.presets(context.Background(), "remove Offline name"); err != nil || len(state.Presets(env.EnvironmentID)) != 0 {
		t.Fatalf("exact offline remove failed: %v", err)
	}
	if _, err = a.presets(context.Background(), "apply Offline name"); err != nil {
		t.Fatalf("unknown apply should be a no-op without catalog access: %v", err)
	}
}
