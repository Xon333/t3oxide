package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// Eight configured profile IDs may each contain 4096 bytes. Allow their
// JSON-escaped appearance keys while keeping settings reads and writes bounded.
const maxSettingsBytes = 128 * 1024

// Settings stores appearance only. T3 retains all conversation authority.
type Settings struct {
	mu                sync.Mutex
	path              string
	Theme             string
	Mouse             bool
	Sidebar           bool
	environmentThemes map[string]string
	forcedTheme       string
}

func LoadSettings(path string) (*Settings, error) {
	s := &Settings{path: path, Theme: "violet", Mouse: true}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxSettingsBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSettingsBytes {
		return nil, errors.New("terminal settings exceed 128 KiB")
	}
	var saved struct {
		Theme             string
		Mouse             bool
		Sidebar           bool
		EnvironmentThemes map[string]string
	}
	saved.Theme, saved.Mouse = s.Theme, s.Mouse
	if err = json.Unmarshal(data, &saved); err != nil {
		return nil, fmt.Errorf("terminal settings: %w", err)
	}
	s.Theme, s.Mouse, s.Sidebar = saved.Theme, saved.Mouse, saved.Sidebar
	s.environmentThemes = saved.EnvironmentThemes
	if !ValidTheme(s.Theme) {
		return nil, fmt.Errorf("unknown saved theme %q; choose terminal, violet, cyan or lime", s.Theme)
	}
	for id, name := range s.environmentThemes {
		if id == "" || !ValidTheme(name) {
			return nil, errors.New("invalid saved environment theme")
		}
	}
	return s, nil
}

func ValidTheme(s string) bool { return s == "terminal" || s == "violet" || s == "cyan" || s == "lime" }

func (s *Settings) save() error {
	if s.path == "" {
		return nil
	} // In-memory settings are useful for embedded/test programs.
	saved := map[string]any{"theme": s.Theme, "mouse": s.Mouse, "sidebar": s.Sidebar}
	if len(s.environmentThemes) > 0 {
		saved["environmentThemes"] = s.environmentThemes
	}
	data, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return err
	}
	if len(data)+1 > maxSettingsBytes {
		return errors.New("terminal settings exceed 128 KiB")
	}
	dir := filepath.Dir(s.path)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".terminal-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(append(data, '\n'))
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, s.path)
}

func (s *Settings) SetTheme(name string) error {
	if !ValidTheme(name) {
		return errors.New("choose terminal, violet, cyan or lime")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.Theme
	s.Theme = name
	if err := s.save(); err != nil {
		s.Theme = old
		return err
	}
	s.forcedTheme = ""
	return nil
}
func (s *Settings) SetMouse(on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.Mouse
	s.Mouse = on
	if err := s.save(); err != nil {
		s.Mouse = old
		return err
	}
	return nil
}
func (s *Settings) SetSidebar(on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.Sidebar
	s.Sidebar = on
	if err := s.save(); err != nil {
		s.Sidebar = old
		return err
	}
	return nil
}

func (s *Settings) appearance() (string, bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	theme := s.Theme
	if s.forcedTheme != "" {
		theme = s.forcedTheme
	}
	return theme, s.Mouse, s.Sidebar
}
