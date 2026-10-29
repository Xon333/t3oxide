package ui

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/charmbracelet/x/ansi"
)

type requestEditorState struct {
	editor    textarea.Model
	preserved string
}

type requestForm struct {
	editors              map[string]requestEditorState
	custom               map[string]string
	questions            []client.RequestQuestion
	index, focus, option int
	answers              map[string]any
	problem              string
}

func (f *requestForm) progress() *client.RequestProgress {
	answers := make(map[string]any, len(f.answers))
	for id, value := range f.answers {
		if items, ok := value.([]string); ok {
			value = slices.Clone(items)
		}
		answers[id] = value
	}
	return &client.RequestProgress{Index: f.index, Answers: answers}
}
func (m *Model) initRequestForm(q client.Question) {
	f := &requestForm{questions: q.RequestQuestions, answers: map[string]any{}, editors: map[string]requestEditorState{}, custom: map[string]string{}}
	if q.RequestProgress != nil {
		f.index = max(0, min(len(f.questions)-1, q.RequestProgress.Index))
		for id, value := range q.RequestProgress.Answers {
			if items, ok := value.([]any); ok {
				stringsOnly := []string{}
				for _, item := range items {
					if text, ok := item.(string); ok {
						stringsOnly = append(stringsOnly, text)
					}
				}
				value = stringsOnly
			}
			if items, ok := value.([]string); ok {
				value = slices.Clone(items)
			}
			f.answers[id] = value
		}
	}
	for _, question := range f.questions {
		if _, ok := f.answers[question.ID]; !ok && question.InitialAnswer != "" {
			if question.MultiSelect && len(question.Options) > 0 {
				f.answers[question.ID] = []string{question.InitialAnswer}
			} else {
				f.answers[question.ID] = question.InitialAnswer
			}
		}
	}
	m.dialog.requestForm = f
	m.dialog.kind = client.EnterText
	m.loadRequestAnswer()
}
func (m *Model) loadRequestAnswer() {
	d := m.dialog
	f := d.requestForm
	q := f.questions[f.index]
	f.option = 0
	for i, option := range q.Options {
		if requestOptionSelected(f.answers[q.ID], option.Value) {
			f.option = i
			break
		}
	}
	f.problem = ""
	if len(q.Options) == 0 {
		f.focus = 1
	} else {
		f.focus = 0
	}
	if state, ok := f.editors[q.ID]; ok {
		m.modalInput = state.editor
		d.preservedInitial = state.preserved
		m.refreshDialogDetails()
		m.resize()
		return
	}
	d.preservedInitial = ""
	text, _ := f.answers[q.ID].(string)
	// Offered choices and custom text are independent controls. A chosen
	// option is never prefilled into the custom editor where merely visiting
	// that editor could turn it into a second, hidden answer.
	for _, option := range q.Options {
		if text == option.Value {
			text = ""
			break
		}
	}
	if q.AllowCustomAnswer && len(q.Options) > 0 && text != "" {
		f.custom[q.ID] = text
	}
	if custom, ok := f.custom[q.ID]; ok {
		text = custom
	}
	// Native editors must never silently change the original answer.
	m.modalInput.SetValue("")
	if len(text) <= maxInputBytes && strings.Count(text, "\n") < maxInputLines {
		m.modalInput.SetValue(text)
	}
	if m.modalInput.Value() != text {
		d.preservedInitial = text
		m.modalInput.SetValue("")
	}
	m.modalInput.CursorEnd()
	m.modalInput.Placeholder = "Your answer"
	m.refreshDialogDetails()
	m.resize()
}
func (m *Model) saveRequestAnswer() bool {
	d := m.dialog
	f := d.requestForm
	q := f.questions[f.index]
	if f.focus == 1 || len(q.Options) == 0 {
		text := m.modalInput.Value()
		if d.preservedInitial != "" {
			text = d.preservedInitial
		}
		if !utf8.ValidString(text) || len(text) > maxInputBytes {
			f.problem = "Answer exceeds 64 KiB or contains invalid UTF-8"
			return false
		}
		f.editors[q.ID] = requestEditorState{m.modalInput, d.preservedInitial}
		if text == "" && len(q.Options) > 0 && f.custom[q.ID] != "" {
			previous := f.custom[q.ID]
			offered := false
			for _, option := range q.Options {
				if option.Value == previous {
					offered = true
					break
				}
			}
			if !offered {
				if q.MultiSelect {
					values, _ := f.answers[q.ID].([]string)
					if i := slices.Index(values, previous); i >= 0 {
						f.answers[q.ID] = slices.Delete(slices.Clone(values), i, i+1)
					}
				} else if current, ok := f.answers[q.ID].(string); ok && current == previous {
					delete(f.answers, q.ID)
				}
			}
			delete(f.custom, q.ID)
		}
		// Visiting an empty custom editor does not erase selected options.
		if text != "" || len(q.Options) == 0 {
			if q.MultiSelect && len(q.Options) > 0 {
				values, _ := f.answers[q.ID].([]string)
				values = slices.Clone(values)
				if previous := f.custom[q.ID]; previous != "" && previous != text {
					if i := slices.Index(values, previous); i >= 0 {
						values = slices.Delete(values, i, i+1)
					}
				}
				if !slices.Contains(values, text) {
					values = append(values, text)
				}
				f.custom[q.ID] = text
				f.answers[q.ID] = values
			} else {
				f.answers[q.ID] = text
				if len(q.Options) > 0 {
					f.custom[q.ID] = text
				}
			}
		}
	}
	return true
}
func requestAnswered(value any) bool {
	switch value := value.(type) {
	case string:
		return strings.TrimSpace(value) != ""
	case []string:
		return len(value) > 0
	case []any:
		return len(value) > 0
	}
	return false
}
func (m *Model) advanceRequest(back bool) tea.Cmd {
	d := m.dialog
	f := d.requestForm
	if !m.saveRequestAnswer() {
		return nil
	}
	q := f.questions[f.index]
	if !back && q.Required && !requestAnswered(f.answers[q.ID]) {
		f.problem = "An answer is required"
		return nil
	}
	if back && f.index > 0 {
		f.index--
		m.loadRequestAnswer()
		return m.focusRequestForm()
	}
	if !back && f.index < len(f.questions)-1 {
		f.index++
		m.loadRequestAnswer()
		return m.focusRequestForm()
	}
	if back {
		return nil
	}
	for i, question := range f.questions {
		if question.Required && !requestAnswered(f.answers[question.ID]) {
			f.index = i
			m.loadRequestAnswer()
			f.problem = "An answer is required"
			return m.focusRequestForm()
		}
	}
	if !d.prompt.Respond(client.Answer{RequestAction: "submit", RequestProgress: f.progress()}) {
		m.notice = "This request was retired; no answers sent"
	}
	m.dialog = nil
	m.resize()
	return m.restoreFocus()
}
func (m *Model) focusRequestForm() tea.Cmd {
	d := m.dialog
	if d == nil || d.requestForm == nil || d.requestForm.focus != 1 || d.preservedInitial != "" {
		return nil
	}
	return m.componentCmd("modal", d.serial, m.modalInput.Focus())
}
func (m *Model) requestFormKey(msg tea.KeyPressMsg) tea.Cmd {
	defer m.resizeDialog()
	d := m.dialog
	f := d.requestForm
	q := f.questions[f.index]
	if key.Matches(msg, m.actionBinding("info")) {
		return m.dispatch("info", "")
	}
	if key.Matches(msg, m.actionBinding("request-submit")) {
		return m.advanceRequest(false)
	}
	switch msg.String() {
	case "esc", "ctrl+c":
		return m.cancelForm()
	case "tab", "shift+tab":
		if !m.saveRequestAnswer() {
			return nil
		}
		delta := 1
		if msg.String() == "shift+tab" {
			delta = -1
		}
		for range 5 {
			f.focus = (f.focus + delta + 5) % 5
			if f.focus == 0 && len(q.Options) == 0 || f.focus == 1 && len(q.Options) > 0 && !q.AllowCustomAnswer || f.focus == 2 && f.index == 0 {
				continue
			}
			break
		}
		m.modalInput.Blur()
		return m.focusRequestForm()
	case "ctrl+u":
		if d.preservedInitial != "" && f.focus == 1 {
			d.preservedInitial = ""
			m.modalInput.SetValue("")
			m.modalInput.Placeholder = "Type the replacement answer"
			m.notice = "Original answer cleared locally; type a replacement before submitting"
			return m.focusRequestForm()
		}
	case "enter":
		switch f.focus {
		case 0:
			m.toggleRequestOption()
			return nil
		case 2:
			return m.advanceRequest(true)
		case 3:
			return m.advanceRequest(false)
		case 4:
			return m.cancelForm()
		}
	case "up":
		if f.focus == 0 && len(q.Options) > 0 {
			f.option = (f.option + len(q.Options) - 1) % len(q.Options)
			return nil
		}
	case "down":
		if f.focus == 0 && len(q.Options) > 0 {
			f.option = (f.option + 1) % len(q.Options)
			return nil
		}
	case "space", " ":
		if f.focus == 0 {
			m.toggleRequestOption()
			return nil
		}
	}
	if f.focus == 1 {
		return m.updateComponent("modal", d.serial, msg)
	}
	return nil
}
func (m *Model) toggleRequestOption() {
	f := m.dialog.requestForm
	q := f.questions[f.index]
	if len(q.Options) == 0 {
		return
	}
	value := q.Options[f.option].Value
	f.problem = ""
	if !q.MultiSelect {
		f.answers[q.ID] = value
		m.modalInput.SetValue("")
		m.dialog.preservedInitial = ""
		delete(f.editors, q.ID)
		delete(f.custom, q.ID)
		return
	}
	values, _ := f.answers[q.ID].([]string)
	if i := slices.Index(values, value); i >= 0 {
		values = slices.Delete(slices.Clone(values), i, i+1)
	} else {
		values = append(slices.Clone(values), value)
	}
	f.answers[q.ID] = values
	if !q.AllowCustomAnswer {
		m.modalInput.SetValue("")
		m.dialog.preservedInitial = ""
		delete(f.editors, q.ID)
	}
}

func requestOptionSelected(answer any, value string) bool {
	switch answer := answer.(type) {
	case string:
		return answer == value
	case []string:
		return slices.Contains(answer, value)
	}
	return false
}

func (m *Model) requestEditorVisible() bool {
	d := m.dialog
	f := d.requestForm
	q := f.questions[f.index]
	return len(q.Options) == 0 || q.AllowCustomAnswer && (f.focus == 1 || m.modalInput.Value() != "" || d.preservedInitial != "")
}

func (m *Model) requestFormPreferredHeight() int {
	f := m.dialog.requestForm
	q := f.questions[f.index]
	width := max(1, m.modalWidth)
	rows := 1 + strings.Count(ansi.Hardwrap(safeText(q.Question), width, true), "\n")
	if q.Header != "" {
		rows++
	}
	rows += len(q.Options)
	if len(q.Options) == 0 || q.AllowCustomAnswer {
		rows++ // answer/custom-answer label
		if m.requestEditorVisible() {
			rows += 3
		}
	}
	if f.problem != "" {
		rows++
	}
	return rows + 3 // pinned question/progress, actions, outer help
}

func (m *Model) requestFormView(width, height int) string {
	d := m.dialog
	f := d.requestForm
	q := f.questions[f.index]
	required := "Optional"
	if q.Required {
		required = "Required"
	}
	kind := "Text answer"
	if len(q.Options) > 0 {
		kind = "Choose one"
		if q.MultiSelect {
			kind = "Choose any"
		}
	}
	heading := fmt.Sprintf("Question %d/%d · %s · %s", f.index+1, len(f.questions), required, kind)
	question := ansi.Hardwrap(safeText(q.Question), width, true)
	rows := []string{}
	if q.Header != "" {
		rows = append(rows, colors(m.theme).plain().Bold(true).Render(fitLine(safeText(q.Header), width)))
	}
	rows = append(rows, question)
	optionStart := len(rows)
	for i, option := range q.Options {
		prefix := "○ "
		if requestOptionSelected(f.answers[q.ID], option.Value) {
			prefix = "✓ "
		}
		label := fitLine(prefix+safeText(option.Label), max(1, width-2))
		if f.focus == 0 && i == f.option {
			label = colors(m.theme).selection().Render("› " + label)
		} else {
			label = "  " + label
		}
		rows = append(rows, label)
	}
	if len(q.Options) == 0 || q.AllowCustomAnswer {
		label := "Answer"
		if len(q.Options) > 0 {
			label = "Custom answer"
		}
		if f.focus == 1 {
			label = "› " + label
		} else if !m.requestEditorVisible() {
			label += " · Tab to write"
		}
		label = fitLine(label, width)
		if f.focus == 1 {
			label = colors(m.theme).active().Render(label)
		}
		rows = append(rows, label)
		if m.requestEditorVisible() {
			if d.preservedInitial != "" {
				rows = append(rows, fitLine("Exact answer preserved · "+m.actionBinding("info").Help().Key+" Info", width))
			} else {
				rows = append(rows, m.modalInput.View())
			}
		}
	}
	body := strings.Join(rows, "\n")
	next := "Next"
	if f.index == len(f.questions)-1 {
		next = "Submit answers"
	}
	controls := []string{"Back", next, "Cancel"}
	for i := range controls {
		if f.focus == i+2 {
			controls[i] = "[" + controls[i] + "]"
		}
	}
	if f.index == 0 {
		controls = controls[1:]
	}
	footer := fitLine(strings.Join(controls, "  "), width)
	if f.problem != "" {
		footer = ansi.Truncate(f.problem, width, "…") + "\n" + footer
	}
	footerRows := strings.Count(footer, "\n") + 1
	v := viewport.New(viewport.WithWidth(width), viewport.WithHeight(max(1, height-footerRows-1)))
	v.SetContent(body)
	if f.focus == 0 && len(q.Options) > 0 {
		// Question wraps and option rows contribute their actual display height.
		before := strings.Join(rows[:optionStart+f.option], "\n")
		row := strings.Count(before, "\n") + 1
		if row >= v.Height() {
			v.SetYOffset(row - v.Height() + 1)
		}
	} else if f.focus == 1 {
		v.GotoBottom()
	}
	return colors(m.theme).subtle().Render(fitLine(heading, width)) + "\n" + v.View() + "\n" + footer
}
