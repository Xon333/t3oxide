package config

import "fmt"

func (e Environment) validateProviderPolicy() error {
	if e.AllowedProviderInstanceIDs == nil {
		return nil
	}
	if len(e.AllowedProviderInstanceIDs) == 0 || len(e.AllowedProviderInstanceIDs) > 64 {
		return fmt.Errorf("provider policy needs 1–64 exact instance IDs")
	}
	seen := map[string]bool{}
	for _, id := range e.AllowedProviderInstanceIDs {
		if !boundedIdentifier(id, 4096) || seen[id] {
			return fmt.Errorf("invalid or duplicate allowed provider instance ID")
		}
		seen[id] = true
	}
	return nil
}

// CheckProviderInstance restricts future execution without rewriting native
// settings or historical identities. An omitted policy preserves native behavior.
func (e Environment) CheckProviderInstance(id string) error {
	if e.AllowedProviderInstanceIDs == nil {
		return nil
	}
	for _, allowed := range e.AllowedProviderInstanceIDs {
		if id == allowed {
			return nil
		}
	}
	return fmt.Errorf("provider instance %q is not allowed by this environment profile", id)
}
