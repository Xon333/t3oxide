package config

import "sort"

// PendingRecord also includes sources that have since been removed from the
// profile configuration. Losing a route must never hide an uncertain action.
type PendingRecord struct {
	EnvironmentID string
	Stop          bool
	Record        map[string]any
}

func (s *State) HasPendingRecords() bool {
	if s == nil {
		return false
	}
	snapshot := s.presentation.Load()
	return snapshot != nil && snapshot.hasPending
}

func (s *State) PendingRecords() []PendingRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []PendingRecord
	for _, stop := range []bool{false, true} {
		lane := s.saved.Pending
		if stop {
			lane = s.saved.PendingStops
		}
		for env, record := range lane {
			result = append(result, PendingRecord{EnvironmentID: env, Stop: stop, Record: cloneRecord(record)})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].EnvironmentID == result[j].EnvironmentID {
			return !result[i].Stop && result[j].Stop
		}
		return result[i].EnvironmentID < result[j].EnvironmentID
	})
	return result
}
