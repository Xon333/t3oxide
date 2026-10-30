package wire

// SnapshotSince avoids copying an identical projection for control/draft wake
// events. The epoch changes for Select (including a same-thread reconnect) and
// every accepted view/delta. It is local bookkeeping, never wire authority.
// A returned snapshot is as defensive as Snapshot; no mutable store data escapes.
func (s *Store) SnapshotSince(previous uint64) (snapshot *Snapshot, epoch uint64, current bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if previous != s.epoch {
		snapshot = cloneSnapshot(s.snapshot)
	}
	return snapshot, s.epoch, s.snapshot != nil
}

// CloneSnapshot gives retained projections the same defensive ownership as a
// Store read. String bytes are immutable; every mutable nested value is copied.
func CloneSnapshot(snapshot *Snapshot) *Snapshot { return cloneSnapshot(snapshot) }

// CloneRequestValues preserves exact JSON numbers, strings, nulls and opaque
// siblings. It replaces serialization round trips on the read path, without
// weakening validation/size checks at the protocol boundary.
func CloneRequestValues(requests []map[string]any) []map[string]any {
	if requests == nil {
		return nil
	}
	next := make([]map[string]any, len(requests))
	for i, request := range requests {
		if request != nil {
			next[i] = cloneJSONValue(request).(map[string]any)
		}
	}
	return next
}

func cloneJSONValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		if value == nil {
			return map[string]any(nil)
		}
		next := make(map[string]any, len(value))
		for key, item := range value {
			next[key] = cloneJSONValue(item)
		}
		return next
	case []any:
		if value == nil {
			return []any(nil)
		}
		next := make([]any, len(value))
		for i, item := range value {
			next[i] = cloneJSONValue(item)
		}
		return next
	case []map[string]any:
		return CloneRequestValues(value)
	default:
		// Wire parsing creates only JSON scalar values (including json.Number),
		// maps and arrays. Scalars are immutable and retain their original type.
		return value
	}
}
