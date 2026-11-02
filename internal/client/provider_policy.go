package client

import (
	"encoding/json"
	"errors"

	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
)

func checkSelectionPolicy(env config.Environment, value any) error {
	if env.AllowedProviderInstanceIDs == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var selection config.ModelSelection
	if err := json.Unmarshal(raw, &selection); err != nil {
		return err
	}
	if err := selection.Validate(); err != nil {
		return errors.New("provider policy requires an exact native model selection")
	}
	return env.CheckProviderInstance(selection.InstanceID)
}

// Mutates only a new request's fields. Same-ID recovery bypasses this preflight:
// its original journal identity and receipt remain authoritative.
func guardProviderOperation(env config.Environment, op string, fields map[string]any, snapshot *wire.Snapshot, overrideSupported bool) error {
	if env.AllowedProviderInstanceIDs == nil {
		return nil
	}
	switch op {
	case "send", "launch", "select-model":
		selection, explicit := fields["modelSelection"]
		if !explicit && snapshot != nil {
			selection = snapshot.Metadata.ModelSelection
		}
		if err := checkSelectionPolicy(env, selection); err != nil {
			return err
		}
		if (op == "send" || op == "launch") && !explicit {
			if !overrideSupported {
				return errors.New("provider policy requires exact model selection support")
			}
			fields["modelSelection"] = snapshot.Metadata.ModelSelection.Clone()
		}
	case "answer":
		if decision := stringValue(fields["decision"]); decision == "decline" || decision == "cancel" {
			return nil
		}
		if snapshot == nil || snapshot.Metadata.RuntimeDetails == nil || snapshot.Metadata.RuntimeDetails.RunModelSelection == nil {
			return errors.New("provider policy requires a reported run selection before resuming this request")
		}
		return checkSelectionPolicy(env, snapshot.Metadata.RuntimeDetails.RunModelSelection)
	}
	return nil
}

func (a *Session) checkModelProviderPolicy(instance string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.environment == nil {
		return errors.New("select an environment first")
	}
	return a.environment.CheckProviderInstance(instance)
}
