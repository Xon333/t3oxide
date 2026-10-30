package config

import "errors"

// CreationDestination pins identity only. Native defaults, credentials,
// provider/model options and workspace values are resolved for each review.
type CreationDestination struct {
	ProfileID     string `json:"profileId"`
	EnvironmentID string `json:"environmentId"`
	ProjectID     string `json:"projectId,omitempty"`
}

func (v CreationDestination) validate() error {
	if !boundedIdentifier(v.ProfileID, 4096) || !boundedIdentifier(v.EnvironmentID, 4096) || v.ProjectID != "" && !boundedIdentifier(v.ProjectID, 4096) {
		return errors.New("creation destination needs exact profile/environment and optional project identities")
	}
	return nil
}

func (s *State) CreationDestination() (CreationDestination, bool) {
	if s == nil {
		return CreationDestination{}, false
	}
	snapshot := s.presentation.Load()
	if snapshot == nil || snapshot.destination == nil {
		return CreationDestination{}, false
	}
	return *snapshot.destination, true
}

// SetCreationDestination uses the existing private fsynced atomic state owner.
// A failed publication leaves the previous destination and every draft intact.
// nil removes the pin and restores the configured/default/explicit-choice policy.
func (s *State) SetCreationDestination(destination *CreationDestination) error {
	if destination != nil {
		if err := destination.validate(); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneState(s.saved)
	next.CreationDestination = nil
	if destination != nil {
		copy := *destination
		next.CreationDestination = &copy
	}
	return s.commit(next)
}
