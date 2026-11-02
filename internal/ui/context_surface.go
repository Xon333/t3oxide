package ui

import (
	"github.com/Xon333/t3oxide/internal/wire"
	"reflect"
)

type contextSurfaceKey struct {
	generation                          uint64
	environment, profile, thread, theme string
	width, height, selection            int
	focused, activity                   bool
	facts                               []string
	items                               *wire.ActivityPage
}
type contextSurface struct {
	key           contextSurfaceKey
	content       string
	first, offset int
	valid         bool
}

func (m *Model) contextPanel(width, height int) (string, int, int) {
	var activity *wire.ActivityPage
	if m.snapshot != nil {
		activity = m.snapshot.Metadata.Activity
	}
	key := contextSurfaceKey{generation: m.generation, environment: m.status.EnvironmentID, profile: m.status.ProfileID, thread: m.status.ThreadID, theme: m.theme, width: width, height: height, selection: m.contextSelection, focused: m.contextFocused, activity: m.status.Activity, facts: m.contextFacts(), items: activity}
	if m.contextSurface.valid && reflect.DeepEqual(key, m.contextSurface.key) {
		return m.contextSurface.content, m.contextSurface.first, m.contextSurface.offset
	}
	content, first, offset := m.renderContextPanel(width, height, key.facts)
	key.items = key.items.Clone()
	m.contextSurface = contextSurface{key: key, content: content, first: first, offset: offset, valid: true}
	return content, first, offset
}
