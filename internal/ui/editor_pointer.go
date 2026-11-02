package ui

import (
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

type editorGesture struct {
	anchor     textarea.Position
	zone       hitZone
	generation uint64
	modal      uint64
}

// Editor bounds are taken from this frame's hit geometry, never estimated
// from terminal dimensions after input has already changed the layout.
func (m *Model) drawnEditor() (*textarea.Model, hitZone, bool) {
	if m.layoutDirty || m.hitGeneration != m.generation {
		return nil, hitZone{}, false
	}
	for _, z := range m.hitZones {
		if m.dialog != nil && m.detail == "" && m.modalInput.Focused() && m.dialog.preservedInitial == "" && (z.action == "text-field" || z.action == "request-field" && m.requestEditorVisible()) {
			if z.action == "request-field" {
				z.y++
				z.height--
			}
			return &m.modalInput, z, true
		}
		if m.dialog == nil && m.detail == "" && z.action == "composer" {
			z.x += 2
			z.y++
			z.width -= 4
			z.height -= 2
			return &m.composer, z, true
		}
	}
	return nil, hitZone{}, false
}
func (m *Model) cancelEditorGesture() {
	m.composer.EndSelection()
	m.modalInput.EndSelection()
	m.editorGesture = nil
}
func (m *Model) editorPointer(msg tea.Msg) (bool, tea.Cmd) {
	if !m.mouse {
		m.cancelEditorGesture()
		return false, nil
	}
	editor, z, ok := m.drawnEditor()
	if !ok {
		m.cancelEditorGesture()
		return false, nil
	}
	switch p := msg.(type) {
	case tea.MouseClickMsg:
		if p.Button != tea.MouseLeft || !z.contains(p.X, p.Y) {
			m.cancelEditorGesture()
			return false, nil
		}
		if editor == &m.modalInput {
			anchor := snapGrapheme(editor, editor.PositionAt(p.X-z.x, p.Y-z.y), false)
			selectEditorRange(editor, anchor, anchor)
			m.editorGesture = &editorGesture{anchor: anchor, zone: z, generation: m.generation, modal: m.dialog.serial}
			return true, nil
		}
		m.activityFocused = false
		m.navigator.focused = false
		m.contextFocused = false
		m.transcriptFocus = false
		anchor := snapGrapheme(editor, editor.PositionAt(p.X-z.x, p.Y-z.y), false)
		selectEditorRange(editor, anchor, anchor)
		m.editorGesture = &editorGesture{anchor: anchor, zone: z, generation: m.generation}
		return true, m.resizeWorkspaceFocus()
	case tea.MouseMotionMsg:
		g := m.editorGesture
		if g == nil {
			return false, nil
		}
		if g.generation != m.generation || g.zone != z || (g.modal != 0 && (m.dialog == nil || m.dialog.serial != g.modal)) {
			m.cancelEditorGesture()
			return true, nil
		}
		head := editor.PositionAt(p.X-z.x, p.Y-z.y)
		forward := editorOffset(editor, head) >= editorOffset(editor, g.anchor)
		head = snapGrapheme(editor, head, forward)
		selectEditorRange(editor, g.anchor, head)
		return true, nil
	case tea.MouseReleaseMsg:
		if m.editorGesture != nil {
			m.cancelEditorGesture()
			return true, nil
		}
	}
	return false, nil
}
