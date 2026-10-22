package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testEnvironment() Environment {
	return Environment{ID: "test", Label: "Test", EnvironmentID: "test-env", Node: "/usr/bin/node", Adapter: "/opt/t3oxide/main.mjs", HostConfig: "/home/test/.config/t3oxide/host.json", Enabled: true}
}
func ptr(s string) *string { return &s }

func TestSSHCommandKeepsHostVerificationAndQuotesPaths(t *testing.T) {
	e := testEnvironment()
	e.SSHDestination = ptr("existing-alias")
	e.Adapter = "/path with spaces/it's/main.mjs"
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
	exe, args := e.Command()
	if exe != "/usr/bin/ssh" {
		t.Fatal(exe)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "StrictHostKeyChecking=yes") || !strings.Contains(joined, "BatchMode=yes") || !strings.Contains(args[len(args)-1], `it'\''s`) {
		t.Fatalf("unsafe SSH command: %q", args)
	}
	if args[len(args)-2] != "existing-alias" {
		t.Fatalf("SSH destination not separate argv: %q", args)
	}
}
func TestProfileRejectsUnsafeAliasesAndCredentials(t *testing.T) {
	for _, alias := range []string{"-oProxyCommand=bad", "host;echo bad", "a b", "a\nhost", ""} {
		e := testEnvironment()
		e.SSHDestination = &alias
		if e.Validate() == nil {
			t.Fatalf("accepted %q", alias)
		}
	}
	for _, web := range []string{"https://u:p@example.com", "https://example.com/?secret=x", "file:///tmp/x", "https://example.com/#secret"} {
		e := testEnvironment()
		e.WebURL = &web
		if e.Validate() == nil {
			t.Fatalf("accepted %q", web)
		}
	}
	e := testEnvironment()
	e.Adapter = "/opt/x\nsecret"
	if e.Validate() == nil {
		t.Fatal("accepted newline in host path")
	}
}
func TestLoadExistingFormatUniqueAndEnabled(t *testing.T) {
	e := testEnvironment()
	disabled := e
	disabled.ID = "other"
	disabled.EnvironmentID = "other-env"
	disabled.Enabled = false
	path := filepath.Join(t.TempDir(), "environments.json")
	write := func(envs []Environment) {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"version": 1, "environments": envs})
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write([]Environment{e, disabled})
	got, err := Load(path)
	if err != nil || len(got) != 1 || got[0].ID != e.ID {
		t.Fatalf("got %v, %v", got, err)
	}
	disabled.EnvironmentID = e.EnvironmentID
	write([]Environment{e, disabled})
	if _, err = Load(path); err == nil {
		t.Fatal("duplicate environment identity accepted")
	}
	if err = os.WriteFile(path, []byte(strings.Repeat(" ", MaxConfigBytes+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(path); err == nil {
		t.Fatal("oversized config accepted")
	}
}
