package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
)

// The native defaults capability is read-only at the pinned contract. Keep
// every reported value, inheritance source and exact limitation in a scrolling
// viewport, with scope/refresh/back controls visible at every supported size.
func (m *Model) nativeDefaultsControls(width int) renderedSurface {
	d := m.dialog
	var controls []surfaceControl
	if d == nil || d.prompt == nil {
		return renderedSurface{}
	}
	for i, choice := range d.prompt.Question.Choices {
		label := "[" + safeText(choice.Label) + "]"
		if d.actionFocus == i+1 {
			label = colors(m.theme).selection().Render(label)
		}
		controls = append(controls, surfaceControl{label: label, action: "defaults-action", args: choice.ID})
	}
	return controlSurface(width, controls)
}

func (m *Model) resizeNativeDefaults(width, height int) {
	buttons := m.nativeDefaultsControls(width)
	buttonRows := strings.Count(buttons.content, "\n") + 1
	m.questionViewport.SetWidth(width)
	m.questionViewport.SetHeight(max(1, height-1-buttonRows))
	m.questionViewport.MouseWheelEnabled = m.mouse
}

func (m *Model) nativeDefaultsLayout(width, height int) renderedSurface {
	buttons := m.nativeDefaultsControls(width)
	heading := colors(m.theme).operational().Render(fitLine("Read only · native Settings for edits", width))
	rows := m.questionViewport.Height()
	content := heading + "\n" + m.questionViewport.View() + "\n" + buttons.content
	hits := []hitZone{{x: 0, y: 1, width: width, height: rows, action: "defaults-body"}}
	hits = append(hits, moveHits(buttons.hits, 0, rows+1)...)
	return renderedSurface{content: clipBlock(content, width, height), hits: clipHits(hits, 0, 0, width, height)}
}

func (m *Model) nativeDefaultsAction(id string) tea.Cmd {
	d := m.dialog
	if d == nil || d.surface != client.SurfaceNativeDefaults || d.prompt == nil || !modalPromptLive(d) {
		return nil
	}
	for i, offered := range d.prompt.Question.Choices {
		if offered.ID == id {
			d.list.Select(i)
			return m.chooseDialog()
		}
	}
	return nil
}

func (m *Model) nativeDefaultsKey(msg tea.KeyPressMsg) tea.Cmd {
	d := m.dialog
	switch msg.String() {
	case "tab", "shift+tab":
		delta := 1
		if msg.String() == "shift+tab" {
			delta = -1
		}
		count := len(d.prompt.Question.Choices) + 1
		d.actionFocus = (d.actionFocus + delta + count) % count
		return m.restoreFocus()
	case "enter":
		if i := d.actionFocus - 1; i >= 0 && i < len(d.prompt.Question.Choices) {
			return m.nativeDefaultsAction(d.prompt.Question.Choices[i].ID)
		}
	case "up", "ctrl+up", "k":
		m.questionViewport.ScrollUp(1)
	case "down", "ctrl+down", "j":
		m.questionViewport.ScrollDown(1)
	case "pgup":
		m.questionViewport.ScrollUp(m.questionViewport.Height())
	case "pgdown":
		m.questionViewport.ScrollDown(m.questionViewport.Height())
	case "home", "g":
		m.questionViewport.GotoTop()
	case "end", "G":
		m.questionViewport.GotoBottom()
	}
	return nil
}

func (m *Model) nativeDefaultsWheel(msg tea.MouseWheelMsg) tea.Cmd {
	if m.layoutDirty || m.hitGeneration != m.generation || !modalPromptLive(m.dialog) || m.drawnModal.authority == "" || m.drawnModal.authority != m.modalAuthority() {
		return nil
	}
	for _, hit := range m.modalHitZones() {
		if hit.action != "defaults-body" || !hit.contains(msg.X, msg.Y) {
			continue
		}
		before := m.questionViewport.YOffset()
		if msg.Button == tea.MouseWheelUp {
			m.questionViewport.ScrollUp(3)
		} else if msg.Button == tea.MouseWheelDown {
			m.questionViewport.ScrollDown(3)
		}
		m.layoutDirty = m.questionViewport.YOffset() != before
		return nil
	}
	return nil
}
