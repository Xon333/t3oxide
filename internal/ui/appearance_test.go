package ui

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Xon333/t3oxide/internal/wire"
	"github.com/charmbracelet/x/ansi"
)

func TestEnvironmentDefaultsAreDistinctStableAndKeepGlobalFallback(t *testing.T) {
	ids := []string{"h", "b", "g", "a", "f", "d", "c", "e"}
	for _, base := range []string{"violet", "cyan", "lime"} {
		s := &Settings{Theme: base}
		seen := make(map[string]bool)
		for _, id := range ids {
			key := s.ThemeFor(id, ids)
			p := colors(key)
			if seen[p.accent] || ValidTheme(key) {
				t.Fatalf("environment default reused an accent or public theme: %q", key)
			}
			seen[p.accent] = true
			reversed := slices.Clone(ids)
			slices.Reverse(reversed)
			if s.ThemeFor(id, append(reversed, id)) != key {
				t.Fatal("browse/configuration order or duplicate changed environment identity")
			}
			if p.operation != colors(base).operation || p.warning != colors(base).warning || p.error != colors(base).error {
				t.Fatal("environment identity changed operational semantics")
			}
			if p.operation == p.accent || p.operation == p.secondary || p.operation == p.warning || p.operation == p.error {
				t.Fatal("operational accent is ambiguous")
			}
			if p.accent == p.secondary {
				t.Fatal("environment identity and links share an accent")
			}
		}
		if s.ThemeFor("missing", ids) != base || s.ThemeFor("", ids) != base || s.Theme != base {
			t.Fatal("legacy global fallback changed")
		}
	}
	s := &Settings{Theme: "terminal"}
	for _, id := range ids {
		if key := s.ThemeFor(id, ids); key != "terminal" {
			t.Fatal("legacy explicit terminal selection lost")
		}
	}
}

func TestEnvironmentSettingsLegacyRoundTripAndCLIOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "terminal.json")
	if err := os.WriteFile(path, []byte(`{"theme":"cyan","mouse":false,"sidebar":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{"first", "second"}
	initial := s.ThemeFor("second", ids)
	if err := s.SetEnvironmentTheme("second", "terminal"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	for _, theme := range []string{"violet", "cyan", "lime", "terminal"} {
		if err := s.OverrideTheme(theme); err != nil {
			t.Fatal(err)
		}
		if s.ThemeFor("second", ids) != theme || s.ThemeFor("first", ids) != theme {
			t.Fatal("CLI theme was changed by environment order or stored override")
		}
		if got, mouse, sidebar := s.appearance(); got != theme || mouse || !sidebar {
			t.Fatal("initial appearance ignored CLI or changed legacy settings")
		}
		if err := s.SetMouse(false); err != nil {
			t.Fatal(err)
		}
		after, _ := os.ReadFile(path)
		if !bytes.Equal(before, after) {
			t.Fatal("unrelated settings save persisted CLI theme")
		}
	}
	if err := s.SetEnvironmentTheme("second", "lime"); err != nil {
		t.Fatal(err)
	}
	if s.ThemeFor("second", ids) != "lime" || s.ThemeFor("first", ids) == "terminal" || s.Theme != "cyan" {
		t.Fatal("menu failed to replace CLI override or changed global theme")
	}
	got, err := LoadSettings(path)
	if err != nil || got.ThemeFor("second", ids) != "lime" {
		t.Fatalf("per-environment choice did not round trip: %v", err)
	}
	for _, clear := range []string{"default", ""} {
		if err := got.SetEnvironmentTheme("second", clear); err != nil {
			t.Fatal(err)
		}
		if got.ThemeFor("second", ids) != initial {
			t.Fatal("clearing environment choice did not restore deterministic default")
		}
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("environment settings are not private: %v %v", info, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatal("atomic persistence left staging files")
	}
}

func TestEnvironmentSettingsRollbackPreservesSavedAndForcedAppearance(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "terminal.json")
	s, err := LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnvironmentTheme("profile", "terminal"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if err := s.OverrideTheme("cyan"); err != nil {
		t.Fatal(err)
	}
	// Temp creation, write and fsync succeed; publishing over a directory fails.
	s.path = filepath.Join(dir, "blocked")
	if err := os.Mkdir(s.path, 0700); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func() error{
		func() error { return s.SetEnvironmentTheme("profile", "lime") },
		func() error { return s.SetEnvironmentTheme("profile", "default") },
		func() error { return s.SetTheme("lime") },
		func() error { return s.SetEnvironmentTheme("profile", "invalid") },
		func() error { return s.SetEnvironmentTheme("", "lime") },
	} {
		if err := change(); err == nil {
			t.Fatal("invalid or failed persistence accepted")
		}
		if s.ThemeFor("profile", []string{"profile"}) != "cyan" || s.environmentThemes["profile"] != "terminal" || s.Theme != "violet" {
			t.Fatal("failed save changed in-memory settings or CLI override")
		}
	}
	after, _ := os.ReadFile(path)
	entries, _ := os.ReadDir(dir)
	if !bytes.Equal(before, after) || len(entries) != 2 {
		t.Fatal("failed atomic save changed original or left staging")
	}
	s.path = path
	if err := s.SetEnvironmentTheme(strings.Repeat("x", maxSettingsBytes), "lime"); err == nil {
		t.Fatal("saved settings that cannot be loaded under the size limit")
	}
}

func TestEnvironmentSettingsFitEightMaximumConfiguredProfileIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "terminal.json")
	s, err := LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 8)
	for i := range ids {
		// Quote characters need JSON escaping; profile IDs allow them.
		ids[i] = string(rune('a'+i)) + strings.Repeat(`"`, 4095)
		if err := s.SetEnvironmentTheme(ids[i], "terminal"); err != nil {
			t.Fatal(err)
		}
	}
	got, err := LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if got.ThemeFor(id, ids) != "terminal" {
			t.Fatal("supported maximum profile ID did not round trip")
		}
	}
}

func TestEnvironmentSettingsRejectInvalidSavedChoices(t *testing.T) {
	for _, content := range []string{`{"environmentThemes":{"profile":"default"}}`, `{"environmentThemes":{"":"lime"}}`, `{"environmentThemes":{"profile":"future"}}`} {
		path := filepath.Join(t.TempDir(), "terminal.json")
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadSettings(path); err == nil {
			t.Fatal("invalid saved environment choice accepted")
		}
	}
}

func TestEnvironmentSettingsConcurrentAccess(t *testing.T) {
	s := &Settings{Theme: "violet"}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			for range 10 {
				_ = s.SetEnvironmentTheme("profile", []string{"terminal", "lime", "default"}[i%3])
				_ = s.OverrideTheme([]string{"", "cyan"}[i%2])
				_ = s.ThemeFor("profile", []string{"profile"})
				_, _, _ = s.appearance()
			}
		})
	}
	wg.Wait()
}

func TestEnvironmentRenderIdentityInvalidatesMarkdownAndKeyedRows(t *testing.T) {
	s := &Settings{Theme: "violet"}
	ids := []string{"alpha", "beta"}
	var r transcriptRenderer
	req := renderRequest{snapshot: &wire.Snapshot{Items: []wire.Row{{Key: "same-row", Kind: "assistant_message", Text: "# Context\n\n[link](https://example.com)"}}}, width: 60, theme: s.ThemeFor("alpha", ids)}
	first, err := r.render(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	formatter := r.markdown
	req.theme = s.ThemeFor("beta", ids)
	second, err := r.render(context.Background(), req)
	if err != nil || first == second || formatter == r.markdown || r.theme != req.theme {
		t.Fatalf("environment switch reused rendered Markdown: %v", err)
	}
	// Identity changes must invalidate even if two contexts receive equal colors.
	formatter = r.markdown
	req.theme = strings.Replace(req.theme, ":YmV0YQ", ":b3RoZXI", 1)
	if _, err := r.render(context.Background(), req); err != nil || formatter == r.markdown {
		t.Fatal("equal-color identity switch reused Markdown")
	}
}

func TestAgentLabelsUseOnlyMatchingAuthoritativeLineageAndRefreshCache(t *testing.T) {
	var r transcriptRenderer
	subagent, fork, parent := "subagent", "fork", "parent"
	snapshot := &wire.Snapshot{Metadata: wire.Metadata{ThreadID: "current"}, Items: []wire.Row{{Key: "unchanged", SourceThreadID: "current", Kind: "assistant_message", Text: "Unchanged body"}}}
	req := renderRequest{snapshot: snapshot, theme: "terminal", width: 80}
	for _, tc := range []struct {
		lineage *wire.Lineage
		source  string
		want    string
	}{
		{nil, "current", "Agent"},
		{&wire.Lineage{ParentThreadID: &parent}, "current", "Agent"},
		{&wire.Lineage{RelationshipToParent: &subagent, ParentThreadID: &parent}, "current", "Subagent"},
		{&wire.Lineage{RelationshipToParent: &fork, ParentThreadID: &parent}, "current", "Agent · fork"},
		{&wire.Lineage{RelationshipToParent: &subagent, ParentThreadID: &parent}, "other", "Agent"},
		{&wire.Lineage{RelationshipToParent: &subagent, ParentThreadID: &parent}, "", "Agent"},
	} {
		snapshot.Metadata.Lineage, snapshot.Items[0].SourceThreadID = tc.lineage, tc.source
		out, err := r.render(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		first := strings.TrimSpace(ansi.Strip(strings.Split(out, "\n")[0]))
		if first != "● "+tc.want || snapshot.Items[0].Kind != "assistant_message" {
			t.Fatalf("role was inferred or stale: %q, wanted %q", first, tc.want)
		}
	}
	snapshot.Items[0].Kind = "future_agent_kind"
	out, err := r.render(context.Background(), req)
	if err != nil || !strings.Contains(ansi.Strip(out), "future_agent_kind") {
		t.Fatal("unknown wire kind renamed")
	}
}
