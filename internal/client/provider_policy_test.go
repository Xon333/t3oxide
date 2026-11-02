package client

import (
	"context"
	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
	"testing"
)

func TestProviderPolicyBindsExactNativeSelectionAndPreservesOptions(t *testing.T) {
	env := config.Environment{AllowedProviderInstanceIDs: []string{"pro"}}
	selection := config.ModelSelection{InstanceID: "pro", Model: "model", Options: []config.ModelOption{{ID: "reasoningEffort", Value: "medium"}}}
	snapshot := &wire.Snapshot{}
	snapshot.Metadata.ModelSelection = &selection
	for _, op := range []string{"send", "launch"} {
		fields := map[string]any{}
		if err := guardProviderOperation(env, op, fields, snapshot, true); err != nil {
			t.Fatal(err)
		}
		bound, ok := fields["modelSelection"].(config.ModelSelection)
		if !ok || !bound.Equal(selection) {
			t.Fatal("policy changed native options or failed to bind exact selection")
		}
	}
	if err := guardProviderOperation(env, "send", map[string]any{}, snapshot, false); err == nil {
		t.Fatal("policy silently inherited selection on unsupported host")
	}
	disallowed := selection.Clone()
	disallowed.InstanceID = "other"
	for _, op := range []string{"send", "launch", "select-model"} {
		if err := guardProviderOperation(env, op, map[string]any{"modelSelection": disallowed}, snapshot, true); err == nil {
			t.Fatalf("%s allowed other provider", op)
		}
	}
}

func TestProviderPolicyPreservesDenialsStopsAndRestrictsResume(t *testing.T) {
	env := config.Environment{AllowedProviderInstanceIDs: []string{"pro"}}
	for _, op := range []string{"stop", "thread-action"} {
		if err := guardProviderOperation(env, op, nil, nil, false); err != nil {
			t.Fatal(err)
		}
	}
	for _, decision := range []string{"decline", "cancel"} {
		if err := guardProviderOperation(env, "answer", map[string]any{"decision": decision}, nil, false); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := &wire.Snapshot{}
	for _, selection := range []*config.ModelSelection{nil, {InstanceID: "other", Model: "model"}, {InstanceID: "pro", Model: "model"}} {
		snapshot.Metadata.RuntimeDetails = &wire.RuntimeDetails{RunModelSelection: selection}
		for _, fields := range []map[string]any{{"decision": "accept"}, {"answers": map[string]any{"q": "answer"}}} {
			err := guardProviderOperation(env, "answer", fields, snapshot, false)
			if (err == nil) != (selection != nil && selection.InstanceID == "pro") {
				t.Fatalf("unexpected resume policy: %v", err)
			}
		}
	}
	if err := guardProviderOperation(config.Environment{}, "send", map[string]any{}, nil, false); err != nil {
		t.Fatal("omitted policy changed behavior")
	}
}

func TestProviderPolicyDoesNotReinterpretAlreadyUncertainSend(t *testing.T) {
	session, state, log := recoverySession(t, "out={'result':{'sequence':1.5 if count==1 else 7}}")
	if err := session.SetDraft("original message"); err != nil {
		t.Fatal(err)
	}
	if err := session.Send(context.Background(), "original message"); err == nil {
		t.Fatal("fixture should retain uncertain operation")
	}
	pending := state.Pending("fixture-env", false)
	if pending == nil {
		t.Fatal("original operation missing")
	}
	session.mu.Lock()
	session.environment.AllowedProviderInstanceIDs = []string{"new-allowed-provider"}
	session.mu.Unlock()
	if err := session.Send(context.Background(), "new message"); err == nil {
		t.Fatal("new unknown-provider execution admitted")
	}
	if _, err := session.Retry(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	writes := requestsFromLog(t, log)
	if len(writes) != 2 || writes[0]["id"] != writes[1]["id"] || writes[1]["modelSelection"] != nil {
		t.Fatal("policy changed original recovery payload")
	}
}
