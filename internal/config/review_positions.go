package config

import "errors"

// ReviewPosition stores only local viewport coordinates tied to exact source
// hashes. It never records approval, reviewed files or copied diff content.
type ReviewPosition struct {
	ProfileID     string `json:"profileId"`
	EnvironmentID string `json:"environmentId"`
	ProjectID     string `json:"projectId"`
	ThreadID      string `json:"threadId"`
	Digest        string `json:"digest"`
	Offset        int    `json:"offset"`
}

func reviewPositionIdentity(a, b ReviewPosition) bool {
	return a.ProfileID == b.ProfileID && a.EnvironmentID == b.EnvironmentID && a.ProjectID == b.ProjectID && a.ThreadID == b.ThreadID
}

func validateReviewPositions(positions []ReviewPosition) error {
	if len(positions) > 64 {
		return errors.New("review position limit exceeded")
	}
	for i, v := range positions {
		for _, id := range []string{v.ProfileID, v.EnvironmentID, v.ProjectID, v.ThreadID, v.Digest} {
			if !boundedIdentifier(id, 4096) {
				return errors.New("review position needs exact source/hash identity")
			}
		}
		if len(v.Digest) != 64 || v.Offset < 0 || v.Offset > 1000000 {
			return errors.New("invalid review position")
		}
		for _, old := range positions[:i] {
			if reviewPositionIdentity(old, v) {
				return errors.New("duplicate review position")
			}
		}
	}
	return nil
}

func (s *State) ReviewPosition(source ReviewPosition) (ReviewPosition, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.saved.ReviewPositions {
		if reviewPositionIdentity(v, source) {
			return v, true
		}
	}
	return ReviewPosition{}, false
}

// SaveReviewPosition atomically stores the newest coordinate. Empty digest
// forgets an invalidated position; changed hashes never preserve old progress.
func (s *State) SaveReviewPosition(position ReviewPosition) error {
	if position.Digest != "" {
		if err := validateReviewPositions([]ReviewPosition{position}); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneState(s.saved)
	kept := next.ReviewPositions[:0]
	for _, old := range next.ReviewPositions {
		if !reviewPositionIdentity(old, position) {
			kept = append(kept, old)
		}
	}
	next.ReviewPositions = kept
	if position.Digest != "" {
		next.ReviewPositions = append(next.ReviewPositions, position)
	}
	if len(next.ReviewPositions) > 64 {
		next.ReviewPositions = next.ReviewPositions[len(next.ReviewPositions)-64:]
	}
	return s.commit(next)
}
