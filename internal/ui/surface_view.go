package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/list"
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
	return m.hasSavedActions() || m.status.DraftSaveFailed
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
	detail := "Filter loaded"
	if m.dialog.resultsFocused || m.dialog.list.FilterState() != list.Filtering {
		detail = "Loaded conversations"
	}
	if selected, ok := m.dialog.list.SelectedItem().(choiceItem); ok && m.dialog.actionFocus == 0 {
		for _, group := range m.dialog.prompt.Question.BrowserPage.Groups {
			for _, id := range group.ChoiceIDs {
				if id == selected.id {
					detail += " · " + group.Label
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
	surface := m.fullSurfaceLayout()
	m.captureModalFrame(surface, true)
	return surface.content
}

func (m *Model) fullSurfaceLayout() renderedSurface {
	p := colors(m.theme)
	width := m.workspaceWidth()
	header := p.active().Render("t3oxide") + "  " + p.plain().Render("Conversations")
	if m.launchPage && !m.browserPage {
		header = p.active().Render("t3oxide") + "  " + p.plain().Render("New conversation")
	}
	if m.dialog != nil && m.dialog.launch != nil {
		header = p.active().Render("t3oxide") + "  " + p.plain().Render("New conversation")
	}
	if m.dialog != nil && m.dialog.prompt != nil && m.dialog.prompt.Question.BrowserPage != nil {
		page := m.dialog.prompt.Question.BrowserPage
		header += "  " + p.subtle().Render(fitLine(page.Location, max(1, width-25)))
	}
	help := m.dialogHelp(width, true)
	parts := []string{header, p.operational().Render(fitLine(m.attachedTargetStatus(), width))}
	remaining := m.height - lipgloss.Height(strings.Join(parts, "\n")) - lipgloss.Height(help)
	feedback := ""
	if m.feedbackVisible() {
		feedback = m.feedbackView(width)
		remaining -= lipgloss.Height(feedback)
	}
	var body renderedSurface
	if m.detail != "" {
		body = m.modalLayout()
	} else if m.dialog != nil && m.dialog.launch != nil {
		body = m.launchLayout(width, max(1, remaining))
	} else if m.dialog != nil {
		body = m.modalLayout()
	} else {
		body.content = p.subtle().Render("Loading…")
	}
	hits := moveHits(clipHits(body.hits, 0, 0, width, max(1, remaining)), 0, lipgloss.Height(strings.Join(parts, "\n")))
	parts = append(parts, padBlock(body.content, width, max(1, remaining)))
	if feedback != "" {
		parts = append(parts, feedback)
	}
	parts = append(parts, help)
	content := strings.Join(parts, "\n")
	canvas := padBlock(content, m.width, m.height)
	return renderedSurface{content: clipBlock(canvas, m.width, m.height), hits: clipHits(hits, 0, 0, m.width, m.height)}
}

func (m *Model) launchView(width, height int) string {
	return m.launchLayout(width, height).content
}

func (m *Model) launchLayout(width, height int) renderedSurface {
	d := m.dialog
	f := d.launch
	p := colors(m.theme)
	if d.details {
		return m.modalLayout()
	}
	location := launchLocation(f.review.Location)
	message := m.modalInput.View()
	if f.preservedMessage != "" {
		message = p.subtle().Render("Exact first message preserved · " + m.actionBinding("info").Help().Key + " Info · Editor")
	}
	title := m.launchTitle.View()
	if f.preservedTitle != "" {
		title = p.subtle().Render("Exact title preserved · " + m.actionBinding("info").Help().Key + " Info")
	}
	var controls []surfaceControl
	for i, action := range m.launchActions() {
		label := m.launchActionLabel(action)
		if f.focus == i+2 {
			label = p.selection().Render("[" + label + "]")
		} else if action == "create" {
			label = p.active().Bold(true).Render(label)
		} else {
			label = p.plain().Render(label)
		}
		controls = append(controls, surfaceControl{label: label, action: "launch-action", args: action, disabled: action == "create" && f.review.Unavailable != ""})
	}
	buttons := controlSurface(width, controls)
	messageFrame := framed("First message", padBlock(message, width-6, m.modalInput.Height()), width-6, p, f.focus == 0)
	titleY := 2 + lipgloss.Height(messageFrame)
	parts := []string{p.subtle().Render(fitLine("New in "+location, width)), p.plain().Bold(true).Render(fitLine(f.review.Summary, width)), messageFrame, "Title  " + title, buttons.content}
	hits := []hitZone{{x: 0, y: 2, width: lipgloss.Width(messageFrame), height: lipgloss.Height(messageFrame), action: "launch-field", args: "message"}, {x: 0, y: titleY, width: width, height: 1, action: "launch-field", args: "title"}}
	hits = append(hits, moveHits(buttons.hits, 0, titleY+1)...)
	if f.review.Unavailable != "" {
		parts = append(parts, p.subtle().Render(fitLine(f.review.Unavailable, width)))
	}
	content := strings.Join(parts, "\n")
	// On compact terminals Tab scrolls the native form viewport to the focused
	// title/control region; the message textarea owns its own reading position.
	v := viewport.New(viewport.WithWidth(width), viewport.WithHeight(height))
	v.SetContent(content)
	// Reveal the actual focused row, including wrapped actions. Keeping the
	// entire tail at the bottom can hide the selected first action on short
	// terminals when optional controls occupy several rows.
	focusY := 2
	if f.focus == 1 {
		focusY = titleY
	} else if f.focus >= 2 && f.focus-2 < len(controls) {
		for _, hit := range buttons.hits {
			if hit.args == controls[f.focus-2].args {
				focusY = titleY + 1 + hit.y
				break
			}
		}
	}
	if focusY >= height {
		v.SetYOffset(focusY - height + 1)
	}
	return (renderedSurface{content: content, hits: hits}).inViewport(v)
}
