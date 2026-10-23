package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) authoritativeStatus() string {
	var parts []string
	if m.status.RunState != "" {
		parts = append(parts, m.status.RunState)
	}
	if m.status.Requests > 0 {
		parts = append(parts, fmt.Sprintf("Awaiting input (%d)", m.status.Requests))
	}
	if len(parts) == 0 {
		if m.status.ThreadID != "" {
			return "Run unreported"
		}
		return "No conversation open"
	}
	return strings.Join(parts, " · ")
}

func (m *Model) connectionStatus() string {
	connection := m.status.ConnectionState
	if connection == "" {
		connection = "connecting"
	}
	parts := []string{"Connection: " + connection}
	if m.status.Delivery != "" {
		parts = append(parts, "Delivery: "+m.status.Delivery)
	}
	if receipt := m.status.Receipt; receipt != nil && receipt.Phase != "" {
		parts = append(parts, "Receipt: "+receipt.Phase)
	}
	if m.status.PendingAction {
		parts = append(parts, "Saved action")
	}
	if m.status.PendingStop {
		parts = append(parts, "Saved Stop")
	}
	if m.status.DraftSaveFailed {
		parts = append(parts, "Draft not saved")
	}
	return strings.Join(parts, " · ")
}

func (m *Model) durableProblem() bool {
	return m.status.PendingAction || m.status.PendingStop || m.status.DraftSaveFailed
}

func (m *Model) browserControls(width int) string {
	p := colors(m.theme)
	width = max(1, width)
	var profiles, actions strings.Builder
	focusedProfileRow, focusedActionRow := 0, 0
	for i, control := range m.visibleBrowserControls() {
		label := safeText(control.label)
		focused := m.dialog.actionFocus == i+1
		if focused {
			label = p.selection().Render("[" + label + "]")
		} else {
			label = p.plain().Render(label)
		}
		block, focusRow := &actions, &focusedActionRow
		if !control.local && strings.HasPrefix(control.action, "profile:") {
			block, focusRow = &profiles, &focusedProfileRow
		}
		// Wrap before a control when it fits on a fresh row. Long labels use
		// the maintained ANSI wrapper, then the native viewport reveals focus.
		if block.Len() > 0 {
			last := strings.Split(block.String(), "\n")
			if ansi.StringWidth(last[len(last)-1])+2+ansi.StringWidth(label) > width {
				block.WriteString("\n")
			} else {
				block.WriteString("  ")
			}
		}
		if focused {
			*focusRow = strings.Count(block.String(), "\n")
		}
		block.WriteString(ansi.Hardwrap(label, width, true))
	}
	var parts []string
	if profiles.Len() > 0 {
		rows := 1
		if m.height >= 24 {
			rows = 2
		}
		parts = append(parts, browserControlViewport(profiles.String(), width, rows, focusedProfileRow))
	}
	if actions.Len() > 0 {
		parts = append(parts, browserControlViewport(actions.String(), width, 2, focusedActionRow))
	}
	detail := ""
	if selected, ok := m.dialog.list.SelectedItem().(choiceItem); ok && m.dialog.actionFocus == 0 {
		for _, group := range m.dialog.prompt.Question.BrowserPage.Groups {
			for _, id := range group.ChoiceIDs {
				if id == selected.id {
					detail = "Project  " + group.Label
				}
			}
		}
	}
	// Reserve the same group row while traversing controls, so focus does not
	// move the query or selected conversation up and down.
	parts = append(parts, p.active().Render(fitLine(detail, width)))
	return strings.Join(parts, "\n")
}

func browserControlViewport(content string, width, maxRows, focusedRow int) string {
	v := viewport.New(viewport.WithWidth(width), viewport.WithHeight(min(maxRows, lipgloss.Height(content))))
	v.SetContent(content)
	v.SetYOffset(focusedRow)
	return v.View()
}

func (m *Model) browserControlsHeight(width int) int {
	if len(m.visibleBrowserControls()) == 0 {
		return 0
	}
	return lipgloss.Height(m.browserControls(width))
}

// Browser/launch replace the page. Underlying native editors and viewports are
// retained untouched until an explicit offered open returns another session.
func (m *Model) fullSurfaceView() string {
	p := colors(m.theme)
	width := m.workspaceWidth()
	header := p.active().Render("t3oxide") + "  " + p.plain().Render("Browser")
	if m.launchPage && !m.browserPage {
		header = p.active().Render("t3oxide") + "  " + p.plain().Render("New conversation")
	}
	if m.dialog != nil && m.dialog.launch != nil {
		header = p.active().Render("t3oxide") + "  " + p.plain().Render("New conversation")
	}
	if m.dialog != nil && m.dialog.prompt != nil && m.dialog.prompt.Question.BrowserPage != nil {
		page := m.dialog.prompt.Question.BrowserPage
		header += "  " + p.subtle().Render(fitLine(page.Location, max(1, width-20)))
	}
	help := m.dialogHelp(width, true)
	parts := []string{header, p.operational().Render(fitLine(m.authoritativeStatus(), width))}
	remaining := m.height - lipgloss.Height(strings.Join(parts, "\n")) - lipgloss.Height(help)
	feedback := ""
	if m.feedbackVisible() {
		feedback = m.feedbackView(width)
		remaining -= lipgloss.Height(feedback)
	}
	var body string
	if m.detail != "" {
		body = m.modalView()
	} else if m.dialog != nil && m.dialog.launch != nil {
		body = m.launchView(width, max(1, remaining))
	} else if m.dialog != nil {
		body = m.modalView()
	} else {
		body = p.subtle().Render("Loading…")
	}
	parts = append(parts, padBlock(body, width, max(1, remaining)))
	if feedback != "" {
		parts = append(parts, feedback)
	}
	parts = append(parts, help)
	content := strings.Join(parts, "\n")
	canvas := padBlock(content, m.width, m.height)
	return clipBlock(canvas, m.width, m.height)
}

func (m *Model) launchView(width, height int) string {
	d := m.dialog
	f := d.launch
	p := colors(m.theme)
	if d.details {
		return m.modalView()
	}
	location := launchLocation(f.review.Location)
	message := m.modalInput.View()
	if f.preservedMessage != "" {
		message = p.subtle().Render("Exact first message preserved · Ctrl+L review · Editor")
	}
	title := m.launchTitle.View()
	if f.preservedTitle != "" {
		title = p.subtle().Render("Exact title preserved · Ctrl+L review")
	}
	var controls []string
	for i, action := range m.launchActions() {
		label := actionLabel(action)
		if f.focus == i+2 {
			label = p.selection().Render("[" + label + "]")
		} else {
			label = p.plain().Render(label)
		}
		controls = append(controls, label)
	}
	parts := []string{p.subtle().Render(fitLine(location, width)), p.plain().Bold(true).Render(fitLine(f.review.Summary, width)), framed("First message", padBlock(message, width-6, m.modalInput.Height()), width-6, p, f.focus == 0), "Title  " + title, ansi.Hardwrap(strings.Join(controls, "  "), width, true)}
	if f.review.Unavailable != "" {
		parts = append(parts, p.subtle().Render(fitLine(f.review.Unavailable, width)))
	}
	content := strings.Join(parts, "\n")
	// On compact terminals Tab scrolls the native form viewport to the focused
	// title/control region; the message textarea owns its own reading position.
	v := viewport.New(viewport.WithWidth(width), viewport.WithHeight(height))
	v.SetContent(content)
	if f.focus > 0 {
		v.GotoBottom()
	}
	return v.View()
}
