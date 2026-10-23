package ui

import (
	"encoding/base64"
	"errors"
	"slices"
	"strconv"
	"strings"
)

// ThemeFor resolves appearance without changing saved settings. Default variants
// depend on the complete configured profile set, never the order of browsing.
// The opaque key includes profile identity so asynchronous renders cannot reuse
// another environment's styled page, even when their base colors coincide.
func (s *Settings) ThemeFor(profileID string, configuredProfileIDs []string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.forcedTheme != "" {
		return s.forcedTheme
	}
	if name := s.environmentThemes[profileID]; ValidTheme(name) {
		return name
	}
	base := s.Theme
	if !ValidTheme(base) {
		base = "violet"
	}
	if base == "terminal" || profileID == "" {
		return base
	}
	ids := slices.Clone(configuredProfileIDs)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	index, found := slices.BinarySearch(ids, profileID)
	if !found {
		return base
	}
	return "environment:" + base + ":" + strconv.Itoa(index%8) + ":" + base64.RawURLEncoding.EncodeToString([]byte(profileID))
}

// SetEnvironmentTheme changes only this profile's appearance. Empty or default
// restores its derived default. Failed persistence leaves every override intact.
func (s *Settings) SetEnvironmentTheme(profileID, name string) error {
	if profileID == "" {
		return errors.New("choose an environment before changing its theme")
	}
	if name != "" && name != "default" && !ValidTheme(name) {
		return errors.New("choose environment default, terminal, violet, cyan or lime")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.environmentThemes
	next := make(map[string]string, len(old)+1)
	for id, theme := range old {
		next[id] = theme
	}
	if name == "" || name == "default" {
		delete(next, profileID)
	} else {
		next[profileID] = name
	}
	s.environmentThemes = next
	if err := s.save(); err != nil {
		s.environmentThemes = old
		return err
	}
	s.forcedTheme = ""
	return nil
}

// OverrideTheme is a nonpersistent CLI selection. A successful explicit theme
// change clears it, so the menu remains usable after starting with --theme.
// Empty clears the override without changing any saved theme.
func (s *Settings) OverrideTheme(name string) error {
	if name != "" && !ValidTheme(name) {
		return errors.New("choose terminal, violet, cyan or lime")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forcedTheme = name
	return nil
}

func environmentVariant(key string) (base string, index int, ok bool) {
	parts := strings.SplitN(key, ":", 4)
	if len(parts) != 4 || parts[0] != "environment" || !ValidTheme(parts[1]) || parts[1] == "terminal" || parts[3] == "" {
		return "", 0, false
	}
	i, err := strconv.Atoi(parts[2])
	if err != nil || i < 0 || i >= 8 {
		return "", 0, false
	}
	return parts[1], i, true
}

func (m *Model) refreshAppearance(profileID string, configuredProfileIDs []string) {
	if m.settings == nil {
		return
	}
	if theme := m.settings.ThemeFor(profileID, configuredProfileIDs); theme != m.theme {
		m.theme = theme
		m.applyTheme()
	}
}
