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
	var profiles, parts []string
	for i, action := range m.browserActions() {
		label := actionLabel(action)
		if strings.HasPrefix(action, "profile:") {
			id := strings.TrimPrefix(action, "profile:")
			page := m.dialog.prompt.Question.BrowserPage
			for _, profile := range page.Profiles {
				if profile.ID == id {
					label = profile.Label
				}
			}
			if page.ProfileID == id {
				label = "● " + label
			}
		}
		if m.dialog.actionFocus == i+1 {
			label = p.selection().Render("[" + safeText(label) + "]")
		} else {
			label = p.plain().Render(safeText(label))
		}
		if strings.HasPrefix(action, "profile:") {
			profiles = append(profiles, label)
		}
	}
	for _, wanted := range []string{"environments", "new", "settings", "resume", "refresh", "types"} {
		for i, action := range m.browserActions() {
			if action == wanted {
				label := actionLabel(action)
				if m.dialog.actionFocus == i+1 {
					parts = append(parts, p.selection().Render("["+label+"]"))
				} else {
					parts = append(parts, p.plain().Render(label))
				}
			}
		}
	}
	detail := ""
	if m.dialog.actionFocus > 0 {
		detail = "→ " + actionLabel(m.browserActions()[m.dialog.actionFocus-1])
	} else if selected, ok := m.dialog.list.SelectedItem().(choiceItem); ok {
		for _, group := range m.dialog.prompt.Question.BrowserPage.Groups {
			for _, id := range group.ChoiceIDs {
				if id == selected.id {
					detail = "Project  " + group.Label
				}
			}
		}
	}
	return padBlock(ansi.Truncate(strings.Join(profiles, "  "), width, "")+"\n"+ansi.Truncate(strings.Join(parts, "  "), width, "")+"\n"+p.active().Render(fitLine(detail, width)), width, 3)
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
	var body string
	if m.dialog != nil && m.dialog.launch != nil {
		header = p.active().Render("t3oxide") + "  " + p.plain().Render("New conversation")
		body = m.launchView(width)
	} else if m.dialog != nil {
		body = m.modalView()
	} else {
		body = p.subtle().Render("Loading…")
	}
	if m.dialog != nil && m.dialog.prompt != nil && m.dialog.prompt.Question.BrowserPage != nil {
		page := m.dialog.prompt.Question.BrowserPage
		header += "  " + p.subtle().Render(fitLine(page.Location, max(1, width-20)))
	}
	content := header + "\n" + p.operational().Render(fitLine(m.authoritativeStatus(), width)) + "\n" + padBlock(body, width, max(1, m.height-4)) + "\n" + m.feedbackView(width) + "\n" + m.hints(width, "Tab", "controls", "Ctrl+L", "details", "Esc", "resume", "Ctrl+S", "Stop/hold")
	canvas := padBlock(content, m.width, m.height)
	if m.detail != "" {
		panel := m.modalView()
		x, y := max(0, (m.width-lipgloss.Width(panel))/2), max(0, (m.height-lipgloss.Height(panel))/2)
		canvas = lipgloss.NewCompositor(lipgloss.NewLayer(canvas), lipgloss.NewLayer(panel).X(x).Y(y).Z(1)).Render()
	}
	return clipBlock(canvas, m.width, m.height)
}

func (m *Model) launchView(width int) string {
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
	v := viewport.New(viewport.WithWidth(width), viewport.WithHeight(max(1, m.height-4)))
	v.SetContent(content)
	if f.focus > 0 {
		v.GotoBottom()
	}
	return v.View()
}
