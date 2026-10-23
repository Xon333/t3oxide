package ui

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestSettingsPersistAppearanceOnlyAndKeepPrivateMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "terminal.json")
	s, err := LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if theme, mouse, sidebar := s.appearance(); theme != "violet" || !mouse || sidebar {
		t.Fatalf("unexpected defaults: %q %v %v", theme, mouse, sidebar)
	}
	for _, change := range []func() error{func() error { return s.SetTheme("cyan") }, func() error { return s.SetTheme("terminal") }, func() error { return s.SetMouse(false) }, func() error { return s.SetSidebar(true) }} {
		if err := change(); err != nil {
			t.Fatal(err)
		}
	}
	got, err := LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if theme, mouse, sidebar := got.appearance(); theme != "terminal" || mouse || !sidebar {
		t.Fatalf("appearance was not restored: %q %v %v", theme, mouse, sidebar)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("settings mode: %v, %v", info, err)
	}
	data, _ := os.ReadFile(path)
	for _, forbidden := range []string{"thread", "message", "environment", "token", "operation"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("appearance file contains %q", forbidden)
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("temporary settings files left behind: %v", entries)
	}
}

func TestSettingsRejectInvalidAndRollbackFailedSave(t *testing.T) {
	for name, content := range map[string]string{"invalid-json": "{", "unknown-theme": `{"theme":"solar"}`, "too-large": strings.Repeat(" ", maxSettingsBytes+1)} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "terminal.json")
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadSettings(path); err == nil {
				t.Fatal("invalid settings were accepted")
			}
		})
	}
	path := filepath.Join(t.TempDir(), "file-instead-of-directory")
	if err := os.WriteFile(path, []byte("blocking file"), 0600); err != nil {
		t.Fatal(err)
	}
	s := &Settings{path: filepath.Join(path, "terminal.json"), Theme: "violet", Mouse: true}
	if err := s.SetTheme("lime"); err == nil {
		t.Fatal("save unexpectedly succeeded")
	}
	if theme, _, _ := s.appearance(); theme != "violet" {
		t.Fatal("failed persistence changed in-memory theme")
	}
	if err := s.SetTheme("unknown"); err == nil {
		t.Fatal("unknown theme accepted")
	}
}

func TestSettingsConcurrentAccess(t *testing.T) {
	s := &Settings{Theme: "violet", Mouse: true}
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				_ = s.SetTheme([]string{"violet", "cyan", "lime"}[i%3])
				_ = s.SetMouse(i%2 == 0)
				_, _, _ = s.appearance()
			}
		}()
	}
	wg.Wait()
}
