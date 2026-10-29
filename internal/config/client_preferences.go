package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// BrowserPreferences stores only validated profile IDs, never environment
// connection details. Profile order is the user's selected scope order.
type BrowserPreferences struct {
	ProfileIDs    []string `json:"profileIds"`
	LastProfileID string   `json:"lastProfileId,omitempty"`
}

// LaunchDraft is a durable, user-reviewed new conversation form keyed by its
// exact environment and source thread. It never triggers a launch on restore.
type LaunchDraft struct {
	Title                  string             `json:"title"`
	Message                string             `json:"message"`
	ProjectID              string             `json:"projectId,omitempty"`
	Workspace              string             `json:"workspace,omitempty"`
	Selection              *ModelSelection    `json:"selection,omitempty"`
	NativeDefaultsRevision string             `json:"nativeDefaultsRevision,omitempty"`
	NativeWorkspaceRoot    string             `json:"nativeWorkspaceRoot,omitempty"`
	WorkspaceStrategy      *WorkspaceStrategy `json:"workspaceStrategy,omitempty"`
	RuntimeMode            string             `json:"runtimeMode,omitempty"`
	InteractionMode        string             `json:"interactionMode,omitempty"`
}

func cloneBrowserPreferences(p BrowserPreferences) BrowserPreferences {
	p.ProfileIDs = append([]string(nil), p.ProfileIDs...)
	return p
}

func cloneLaunchDraft(d LaunchDraft) LaunchDraft {
	if d.WorkspaceStrategy != nil {
		copy := *d.WorkspaceStrategy
		d.WorkspaceStrategy = &copy
	}
	if d.Selection != nil {
		selection := d.Selection.Clone()
		d.Selection = &selection
	}
	return d
}

func launchSelectionMatches(saved *ModelSelection, value any) bool {
	if saved == nil {
		return false
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var selection ModelSelection
	if decoder.Decode(&selection) != nil {
		return false
	}
	return saved.Equal(selection)
}

func launchWorkspaceMatches(saved *WorkspaceStrategy, value any) bool {
	if saved == nil {
		return false
	}
	raw, err := json.Marshal(value)
	var strategy WorkspaceStrategy
	return err == nil && json.Unmarshal(raw, &strategy) == nil && *saved == strategy
}

func validateClientPreferences(v savedState) error {
	if v.BrowserPrefs != nil {
		prefs := v.BrowserPrefs
		if len(prefs.ProfileIDs) > 40 || len(prefs.LastProfileID) > 4096 || !utf8.ValidString(prefs.LastProfileID) {
			return errors.New("browser preference limit exceeded")
		}
		seen := make(map[string]bool, len(prefs.ProfileIDs))
		lastFound := prefs.LastProfileID == ""
		for _, id := range prefs.ProfileIDs {
			if !boundedIdentifier(id, 4096) || seen[id] {
				return errors.New("invalid or duplicate browser profile preference")
			}
			seen[id] = true
			lastFound = lastFound || id == prefs.LastProfileID
		}
		if !lastFound {
			return errors.New("last browser profile must belong to the selected profile list")
		}
	}
	if len(v.LaunchDrafts) > 40 {
		return errors.New("launch draft limit exceeded")
	}
	for key, draft := range v.LaunchDrafts {
		if draft.WorkspaceStrategy != nil {
			if err := draft.WorkspaceStrategy.Validate(); err != nil {
				return err
			}
		}
		if draft.NativeDefaultsRevision != "" && (len(draft.NativeDefaultsRevision) != 64 || strings.Trim(draft.NativeDefaultsRevision, "0123456789abcdef") != "") || len(draft.RuntimeMode) > 64 || len(draft.InteractionMode) > 64 || len(draft.NativeWorkspaceRoot) > 4096 || strings.ContainsAny(draft.NativeWorkspaceRoot, "\x00\n\r") || draft.NativeWorkspaceRoot != "" && !strings.HasPrefix(draft.NativeWorkspaceRoot, "/") {
			return errors.New("invalid native launch draft")
		}
		if key == "" || len(key) > 8192 || !utf8.ValidString(key) || len(draft.Title) > 512 || len(draft.Message) > 64*1024 || len(draft.ProjectID) > 4096 || len(draft.Workspace) > 4096 || !utf8.ValidString(draft.Title) || !utf8.ValidString(draft.Message) || !utf8.ValidString(draft.ProjectID) || !utf8.ValidString(draft.Workspace) {
			return errors.New("invalid or oversized launch draft")
		}
		if draft.Selection != nil {
			if err := draft.Selection.Validate(); err != nil {
				return fmt.Errorf("invalid saved launch selection: %w", err)
			}
		}
	}
	return nil
}

func (s *State) BrowserPreferences() BrowserPreferences {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saved.BrowserPrefs == nil {
		return BrowserPreferences{}
	}
	return cloneBrowserPreferences(*s.saved.BrowserPrefs)
}

func (s *State) SetBrowserPreferences(preferences BrowserPreferences) error {
	preferences = cloneBrowserPreferences(preferences)
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneState(s.saved)
	next.BrowserPrefs = &preferences
	return s.commit(next)
}

func (s *State) LaunchDraft(env, sourceThread string) (LaunchDraft, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	draft, ok := s.saved.LaunchDrafts[draftKey(env, sourceThread)]
	return cloneLaunchDraft(draft), ok
}

// SetLaunchDraft commits the full reviewed form and its compatibility message
// draft in one state transaction.
func (s *State) SetLaunchDraft(env, sourceThread string, draft LaunchDraft) error {
	if !boundedIdentifier(env, 4096) || !boundedIdentifier(sourceThread, 4096) {
		return errors.New("launch draft needs an exact environment and source thread")
	}
	if len(draft.Title) > 512 || len(draft.Message) > 64*1024 || len(draft.ProjectID) > 4096 || len(draft.Workspace) > 4096 || !utf8.ValidString(draft.Title) || !utf8.ValidString(draft.Message) || !utf8.ValidString(draft.ProjectID) || !utf8.ValidString(draft.Workspace) {
		return errors.New("launch title or first message exceeds its byte limit or is not UTF-8")
	}
	if draft.Selection != nil {
		if err := draft.Selection.Validate(); err != nil {
			return fmt.Errorf("invalid launch selection: %w", err)
		}
	}
	draft = cloneLaunchDraft(draft)
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneState(s.saved)
	key := draftKey(env, sourceThread)
	next.LaunchDrafts[key] = draft
	draftKey := draftKey(env, "\x00new/"+sourceThread)
	if draft.Message == "" {
		delete(next.Drafts, draftKey)
	} else {
		next.Drafts[draftKey] = draft.Message
	}
	return s.commit(next)
}

// ClearLaunchDraft removes only the exact form acknowledged by T3. A newer
// edit remains available if it differs from the submitted form.
func (s *State) ClearLaunchDraft(env, sourceThread string, expected LaunchDraft) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := draftKey(env, sourceThread)
	current, ok := s.saved.LaunchDrafts[key]
	if !ok || !launchDraftEqual(current, expected) {
		return nil
	}
	next := cloneState(s.saved)
	delete(next.LaunchDrafts, key)
	messageKey := draftKey(env, "\x00new/"+sourceThread)
	if next.Drafts[messageKey] == expected.Message {
		delete(next.Drafts, messageKey)
	}
	return s.commit(next)
}

func launchDraftEqual(a, b LaunchDraft) bool {
	if a.NativeDefaultsRevision != b.NativeDefaultsRevision || a.NativeWorkspaceRoot != b.NativeWorkspaceRoot || a.RuntimeMode != b.RuntimeMode || a.InteractionMode != b.InteractionMode {
		return false
	}
	if (a.WorkspaceStrategy == nil) != (b.WorkspaceStrategy == nil) {
		return false
	}
	if a.WorkspaceStrategy != nil && *a.WorkspaceStrategy != *b.WorkspaceStrategy {
		return false
	}
	if a.Title != b.Title || a.Message != b.Message || a.ProjectID != b.ProjectID || a.Workspace != b.Workspace {
		return false
	}
	if a.Selection == nil || b.Selection == nil {
		return a.Selection == nil && b.Selection == nil
	}
	return a.Selection.Equal(*b.Selection)
}
