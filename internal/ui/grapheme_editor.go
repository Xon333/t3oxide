package ui

import (
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/clipperhouse/uax29/v2/graphemes"
)

func graphemeColumns(line string) []int {
	columns := []int{0}
	n := 0
	iter := graphemes.FromString(line)
	for iter.Next() {
		n += utf8.RuneCountInString(iter.Value())
		columns = append(columns, n)
	}
	return columns
}

func snapGrapheme(editor *textarea.Model, p textarea.Position, forward bool) textarea.Position {
	lines := strings.Split(editor.Value(), "\n")
	p.Row = max(0, min(p.Row, len(lines)-1))
	bounds := graphemeColumns(lines[p.Row])
	previous := 0
	for _, b := range bounds {
		if b == p.Col {
			return p
		}
		if b > p.Col {
			if forward {
				p.Col = b
			} else {
				p.Col = previous
			}
			return p
		}
		previous = b
	}
	p.Col = previous
	return p
}

func editorOffset(editor *textarea.Model, p textarea.Position) int {
	lines := strings.Split(editor.Value(), "\n")
	offset := p.Col
	for i := 0; i < p.Row; i++ {
		offset += utf8.RuneCountInString(lines[i]) + 1
	}
	return offset
}

func editorKey(editor *textarea.Model, code rune, mod tea.KeyMod) {
	*editor, _ = editor.Update(tea.KeyPressMsg{Code: code, Mod: mod})
}

// Bubbles exposes selection through keys, rather than buffer-position setters.
// Move from the current position to avoid replaying the whole buffer per drag.
func selectEditorRange(editor *textarea.Model, anchor, head textarea.Position) {
	if !editor.Focused() {
		editor.Focus()
		defer editor.Blur()
	}
	editor.ClearSelection()
	delta := editorOffset(editor, anchor) - editorOffset(editor, textarea.Position{Row: editor.Line(), Col: editor.Column()})
	code := rune(tea.KeyRight)
	if delta < 0 {
		code = tea.KeyLeft
		delta = -delta
	}
	for range delta {
		editorKey(editor, code, 0)
	}
	delta = editorOffset(editor, head) - editorOffset(editor, anchor)
	code = tea.KeyRight
	if delta < 0 {
		code = tea.KeyLeft
		delta = -delta
	}
	for range delta {
		editorKey(editor, code, tea.ModShift)
	}
}

// Expand partial selection endpoints before any copy, replacement or deletion.
func normalizeEditorSelection(editor *textarea.Model) {
	start, end, ok := editor.Selection()
	if !ok {
		return
	}
	a, b := snapGrapheme(editor, start, false), snapGrapheme(editor, end, true)
	if a == start && b == end {
		return
	}
	if editor.Line() == start.Row && editor.Column() == start.Col {
		selectEditorRange(editor, b, a)
	} else {
		selectEditorRange(editor, a, b)
	}
}

func updateGraphemeEditor(editor *textarea.Model, message tea.Msg) (bool, tea.Cmd) {
	normalizeEditorSelection(editor)
	press, ok := message.(tea.KeyPressMsg)
	if !ok {
		updated, cmd := editor.Update(message)
		*editor = updated
		normalizeEditorSelection(editor)
		if !editor.HasSelection() {
			p := snapGrapheme(editor, textarea.Position{Row: editor.Line(), Col: editor.Column()}, true)
			editor.SetCursorColumn(p.Col)
		}
		return true, cmd
	}
	action := press.String()
	if key.Matches(press, editor.KeyMap.DeleteCharacterBackward) {
		action = "backspace"
	}
	if key.Matches(press, editor.KeyMap.DeleteCharacterForward) {
		action = "delete"
	}
	shift := action == "shift+left" || action == "shift+right"
	if shift {
		action = strings.TrimPrefix(action, "shift+")
	}
	if action != "left" && action != "right" && action != "backspace" && action != "delete" {
		updated, cmd := editor.Update(message)
		*editor = updated
		normalizeEditorSelection(editor)
		p := snapGrapheme(editor, textarea.Position{Row: editor.Line(), Col: editor.Column()}, false)
		if !editor.HasSelection() {
			editor.SetCursorColumn(p.Col)
		}
		return true, cmd
	}
	if editor.HasSelection() && !shift {
		updated, cmd := editor.Update(message)
		*editor = updated
		return true, cmd
	}
	before := textarea.Position{Row: editor.Line(), Col: editor.Column()}
	anchor := before
	if start, end, selected := editor.Selection(); selected {
		anchor = start
		if before == start {
			anchor = end
		}
	}
	if action == "backspace" || action == "delete" {
		probe := before
		if action == "backspace" {
			probe.Col--
		} else {
			probe.Col++
		}
		if probe.Col >= 0 && probe.Col <= utf8.RuneCountInString(strings.Split(editor.Value(), "\n")[before.Row]) {
			target := snapGrapheme(editor, probe, action == "delete")
			selectEditorRange(editor, before, target)
		}
		updated, cmd := editor.Update(message)
		*editor = updated
		return true, cmd
	}
	*editor, _ = editor.Update(message)
	after := textarea.Position{Row: editor.Line(), Col: editor.Column()}
	target := snapGrapheme(editor, after, action == "right")
	if shift {
		selectEditorRange(editor, anchor, target)
	} else {
		selectEditorRange(editor, target, target)
	}
	return true, nil
}
