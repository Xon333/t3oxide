package config

import "testing"

func TestProviderPolicyRequiresExactValidUniqueIDs(t *testing.T) {
	for _, ids := range [][]string{{}, {"pro", "pro"}, {""}, {"bad\n"}} {
		if err := (Environment{AllowedProviderInstanceIDs: ids}).validateProviderPolicy(); err == nil {
			t.Fatalf("invalid policy accepted: %q", ids)
		}
	}
	env := Environment{AllowedProviderInstanceIDs: []string{"provider-pro"}}
	if err := env.validateProviderPolicy(); err != nil {
		t.Fatal(err)
	}
	if err := env.CheckProviderInstance("provider-pro"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"", "provider", "PROVIDER-PRO"} {
		if err := env.CheckProviderInstance(id); err == nil {
			t.Fatalf("silently substituted %q", id)
		}
	}
	if err := (Environment{}).CheckProviderInstance("native-default"); err != nil {
		t.Fatal(err)
	}
}
