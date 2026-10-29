package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// InboxPreferences contains presentation ranks only. Native T3 owns task,
// thread, run, review, snooze and execution state. No work is dispatched by this
// file, including on startup or after a client exits.
type InboxPreferences struct {
	Version  int            `json:"version"`
	Revision uint64         `json:"revision"`
	Tasks    map[string]int `json:"taskPriorities"`
	Projects map[string]int `json:"projectRanks"`
}

const maxInboxPreferencesBytes = 256 * 1024

func InboxIdentity(environment, project, thread string) string {
	data, _ := json.Marshal([]string{environment, project, thread})
	return string(data)
}

func emptyInboxPreferences() InboxPreferences {
	return InboxPreferences{Version: 1, Tasks: map[string]int{}, Projects: map[string]int{}}
}

func (v InboxPreferences) Clone() InboxPreferences {
	n := emptyInboxPreferences()
	n.Version = v.Version
	n.Revision = v.Revision
	for key, value := range v.Tasks {
		n.Tasks[key] = value
	}
	for key, value := range v.Projects {
		n.Projects[key] = value
	}
	return n
}

func validateInboxPreferences(v InboxPreferences) error {
	if v.Version != 1 || v.Tasks == nil || v.Projects == nil || len(v.Tasks) > 1000 || len(v.Projects) > 200 || v.Revision > 9007199254740991 {
		return errors.New("unsupported or oversized inbox presentation preferences")
	}
	for _, group := range []struct {
		values  map[string]int
		project bool
	}{{v.Tasks, false}, {v.Projects, true}} {
		for key, value := range group.values {
			var identity []string
			if json.Unmarshal([]byte(key), &identity) != nil || len(identity) != 3 || !boundedIdentifier(identity[0], 4096) || !boundedIdentifier(identity[1], 4096) || (!group.project && !boundedIdentifier(identity[2], 4096)) || (group.project && identity[2] != "") {
				return errors.New("inbox preference needs exact environment/project/thread identity")
			}
			if key != InboxIdentity(identity[0], identity[1], identity[2]) {
				return errors.New("noncanonical inbox preference identity")
			}
			if value < 0 || (!group.project && value > 3) || (group.project && value > 200) {
				return errors.New("invalid inbox presentation rank")
			}
		}
	}
	return nil
}

func (s *State) readInboxPreferences() (InboxPreferences, error) {
	v := emptyInboxPreferences()
	f, err := os.Open(s.path + ".inbox-v1.json")
	if errors.Is(err, os.ErrNotExist) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxInboxPreferencesBytes+1))
	if err != nil {
		return v, err
	}
	if len(data) > maxInboxPreferencesBytes {
		return v, errors.New("inbox preferences exceed 256 KiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&v); err != nil {
		return v, fmt.Errorf("inbox preferences preserved: %w", err)
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return v, errors.New("inbox preferences have trailing content; file preserved")
	}
	return v, validateInboxPreferences(v)
}

// InboxPreferences reads the bounded sidecar under the existing state owner's
// process lock. A second client cannot overwrite this owner's recovery/ranks.
func (s *State) InboxPreferences() (InboxPreferences, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return InboxPreferences{}, errors.New("client state is closed")
	}
	return s.readInboxPreferences()
}

// SaveInboxPreferences is an explicit compare-and-swap, including for Undo.
// A stale action never overwrites a newer priority change. The sidecar keeps
// old clients and the retained Swift rollback from dropping these preferences.
func (s *State) SaveInboxPreferences(expected uint64, next InboxPreferences) (InboxPreferences, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return InboxPreferences{}, errors.New("client state is closed")
	}
	current, err := s.readInboxPreferences()
	if err != nil {
		return InboxPreferences{}, err
	}
	if current.Revision != expected {
		return current, errors.New("inbox priorities changed; refresh before retrying")
	}
	next = next.Clone()
	next.Revision = current.Revision + 1
	if err = validateInboxPreferences(next); err != nil {
		return current, err
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return current, err
	}
	data = append(data, '\n')
	if len(data) > maxInboxPreferencesBytes {
		return current, errors.New("inbox preferences exceed 256 KiB")
	}
	if err = writeStateFile(s.path+".inbox-v1.json", data); err != nil {
		return current, err
	}
	return next, nil
}
