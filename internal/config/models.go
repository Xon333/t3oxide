package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ModelSelection is the canonical T3 selection, not a locally invented model
// catalog. Values are string/bool provider options, obtained from T3.
type ModelSelection struct {
	InstanceID string        `json:"instanceId"`
	Model      string        `json:"model"`
	Options    []ModelOption `json:"options,omitempty"`
}
type ModelOption struct {
	ID    string `json:"id"`
	Value any    `json:"value"`
}
type ModelOverride struct {
	ID        string         `json:"id"`
	Selection ModelSelection `json:"selection"`
}
type ModelPreset struct {
	Name      string         `json:"name"`
	Selection ModelSelection `json:"selection"`
}

func boundedIdentifier(s string, limit int) bool {
	return s != "" && len(s) <= limit && utf8.ValidString(s) && strings.IndexFunc(s, unicode.IsControl) < 0
}
func (s ModelSelection) Validate() error {
	if !boundedIdentifier(s.InstanceID, 4096) || !boundedIdentifier(s.Model, 256) || strings.TrimSpace(s.Model) != s.Model || len(s.Options) > 64 {
		return errors.New("invalid or oversized model selection")
	}
	seen := map[string]bool{}
	for _, o := range s.Options {
		if !boundedIdentifier(o.ID, 128) || seen[o.ID] {
			return errors.New("invalid or duplicate model option")
		}
		seen[o.ID] = true
		switch v := o.Value.(type) {
		case bool:
		case string:
			if !boundedIdentifier(v, 1024) {
				return errors.New("invalid model option value")
			}
		default:
			return errors.New("unsupported model option value")
		}
	}
	raw, err := json.Marshal(s)
	if err != nil || len(raw) > 16*1024 {
		return errors.New("model selection exceeds 16 KiB")
	}
	return nil
}
func (s ModelSelection) Clone() ModelSelection {
	s.Options = append([]ModelOption(nil), s.Options...)
	return s
}
func (s ModelSelection) Option(id string) (any, bool) {
	for _, o := range s.Options {
		if o.ID == id {
			return o.Value, true
		}
	}
	return nil, false
}
func (s ModelSelection) WithOption(id string, value any) ModelSelection {
	result := s.Clone()
	result.Options = nil
	for _, o := range s.Options {
		if o.ID != id {
			result.Options = append(result.Options, o)
		}
	}
	if value != nil {
		result.Options = append(result.Options, ModelOption{ID: id, Value: value})
	}
	return result
}
func (s ModelSelection) Equal(other ModelSelection) bool {
	if s.InstanceID != other.InstanceID || s.Model != other.Model || len(s.Options) != len(other.Options) {
		return false
	}
	a, b := s.Clone(), other.Clone()
	sort.Slice(a.Options, func(i, j int) bool { return a.Options[i].ID < a.Options[j].ID })
	sort.Slice(b.Options, func(i, j int) bool { return b.Options[i].ID < b.Options[j].ID })
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// Summary labels only saved choices; a missing option is not a measured default.
func (s ModelSelection) Summary() string {
	parts := []string{s.Model}
	effort := "default effort"
	for _, id := range []string{"reasoningEffort", "effort"} {
		if v, ok := s.Option(id); ok {
			effort = fmt.Sprint(v) + " effort"
			break
		}
	}
	parts = append(parts, effort)
	if v, ok := s.Option("serviceTier"); ok {
		parts = append(parts, fmt.Sprint(v)+" tier")
	} else if v, ok := s.Option("fastMode"); ok {
		if v == true {
			parts = append(parts, "fast on")
		} else {
			parts = append(parts, "fast off")
		}
	}
	return strings.Join(parts, " · ")
}

func (s *State) Override(env, thread string) *ModelOverride {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.saved.ModelOverrides[draftKey(env, thread)]
	if !ok {
		return nil
	}
	o.Selection = o.Selection.Clone()
	return &o
}
func (s *State) SetOverride(env, thread string, override *ModelOverride) error {
	if env == "" || thread == "" {
		return errors.New("select a conversation before staging a model")
	}
	if override != nil {
		if !boundedIdentifier(override.ID, 128) {
			return errors.New("invalid override identity")
		}
		if err := override.Selection.Validate(); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := cloneState(s.saved)
	if override == nil {
		delete(n.ModelOverrides, draftKey(env, thread))
	} else {
		n.ModelOverrides[draftKey(env, thread)] = ModelOverride{ID: override.ID, Selection: override.Selection.Clone()}
	}
	return s.commit(n)
}
func (s *State) Presets(env string) []ModelPreset {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.saved.ModelPresets[env]
	result := make([]ModelPreset, len(list))
	for i, p := range list {
		result[i] = p
		result[i].Selection = p.Selection.Clone()
	}
	return result
}
func (s *State) SavePreset(env, name string, selection ModelSelection) error {
	name = strings.TrimSpace(name)
	if env == "" || !boundedIdentifier(name, 80) {
		return errors.New("preset name must contain 1–80 bytes")
	}
	if err := selection.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := cloneState(s.saved)
	list := n.ModelPresets[env]
	for _, p := range list {
		if strings.EqualFold(p.Name, name) {
			return errors.New("a preset with this name already exists; remove it before replacing")
		}
	}
	n.ModelPresets[env] = append(list, ModelPreset{Name: name, Selection: selection.Clone()})
	return s.commit(n)
}
func (s *State) DeletePreset(env, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := cloneState(s.saved)
	var kept []ModelPreset
	for _, p := range n.ModelPresets[env] {
		if p.Name != name {
			kept = append(kept, p)
		}
	}
	if len(kept) == len(n.ModelPresets[env]) {
		return errors.New("preset was not found")
	}
	if len(kept) == 0 {
		delete(n.ModelPresets, env)
	} else {
		n.ModelPresets[env] = kept
	}
	return s.commit(n)
}
func validateModelPreferences(v savedState) error {
	if len(v.ModelOverrides) > 40 || len(v.ModelPresets) > 8 {
		return errors.New("model preference limit exceeded")
	}
	for _, o := range v.ModelOverrides {
		if !boundedIdentifier(o.ID, 128) {
			return errors.New("invalid saved override identity")
		}
		if err := o.Selection.Validate(); err != nil {
			return err
		}
	}
	for env, list := range v.ModelPresets {
		if env == "" || len(list) > 32 {
			return errors.New("preset limit exceeded (32 per environment)")
		}
		seen := map[string]bool{}
		for _, p := range list {
			key := strings.ToLower(p.Name)
			if !boundedIdentifier(p.Name, 80) || seen[key] {
				return errors.New("invalid or duplicate preset")
			}
			seen[key] = true
			if err := p.Selection.Validate(); err != nil {
				return err
			}
		}
	}
	return nil
}
