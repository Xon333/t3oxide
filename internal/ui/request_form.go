package ui

import (
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/textarea"
)

// Recovery belongs to one question and exact editor state. It never responds
// to the native prompt; the existing explicit Submit remains the only sender.
type requestExactInput struct {
	question   string
	before     string
	start, end textarea.Position
	selection  bool
	text       string
}

func positionByte(text string, p textarea.Position) int {
	lines := strings.Split(text, "\n")
	offset := 0
	for i := 0; i < p.Row; i++ {
		offset += len(lines[i]) + 1
	}
	return offset + len(string([]rune(lines[p.Row])[:p.Col]))
}

func (m *Model) retainRequestExactInput(text string) bool {
	d := m.dialog
	if d == nil || d.requestForm == nil || d.preservedInitial != "" || text == "" || !utf8.ValidString(text) {
		return false
	}
	f := d.requestForm
	if f.focus != 1 {
		return false
	}
	start, end, selected := m.modalInput.Selection()
	if !selected {
		start = textarea.Position{Row: m.modalInput.Line(), Col: m.modalInput.Column()}
		end = start
	}
	before := m.modalInput.Value()
	exact := before[:positionByte(before, start)] + text + before[positionByte(before, end):]
	if len(exact) > maxInputBytes || strings.Count(exact, "\n") >= maxInputLines {
		return false
	}
	d.requestExact = &requestExactInput{f.questions[f.index].ID, before, start, end, selected, exact}
	return true
}

func (m *Model) stageRequestExactInput() bool {
	d := m.dialog
	if d == nil || d.requestForm == nil || d.requestExact == nil || !modalPromptLive(d) {
		return false
	}
	f, r := d.requestForm, d.requestExact
	start, end, selected := m.modalInput.Selection()
	if !selected {
		start = textarea.Position{Row: m.modalInput.Line(), Col: m.modalInput.Column()}
		end = start
	}
	if f.focus != 1 || f.questions[f.index].ID != r.question || m.modalInput.Value() != r.before || start != r.start || end != r.end || selected != r.selection || d.preservedInitial != "" {
		d.requestExact = nil
		return false
	}
	d.preservedInitial = r.text
	d.requestExact = nil
	m.modalInput.SetValue("")
	m.modalInput.Blur()
	f.editors[r.question] = requestEditorState{m.modalInput, r.text}
	m.notice = "Exact answer staged locally · Info to review · Ctrl+U to replace · Submit sends"
	m.refreshDialogDetails()
	m.resize()
	return true
}
