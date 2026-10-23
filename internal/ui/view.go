package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/charmbracelet/x/ansi"
)

// Keep the reading column comfortable on a wide terminal. The optional context
// panel gets its own space instead of stretching every paragraph across it.
func (m *Model) workspaceWidth() int {
	margin, limit := 4, 112
	if m.width < 60 {
		margin = 2
	}
	if m.sidebar && m.width >= 110 {
		limit += 30
	}
	return max(1, min(m.width-margin, limit))
}

func (m *Model) resize() {
	inner := m.workspaceWidth()
	m.contentWidth = inner
	if m.sidebar && m.width >= 110 {
		m.contentWidth -= 30
	}
	wasBottom := m.viewport.AtBottom()
	// Bubbles computes wrapped visual rows. Its high MaxHeight remains intact
	// so the display cap cannot truncate a long human draft (see components.go).
	m.composer.SetWidth(max(1, inner-4))
	m.composer.SetHeight(min(m.composer.Height(), max(1, min(5, m.height/6))))
	m.bodyHeight = max(1, m.height-m.composer.Height()-7)
	m.viewport.SetWidth(m.contentWidth)
	m.viewport.SetHeight(m.bodyHeight)
	m.viewport.MouseWheelEnabled = m.mouse
	if m.follow && wasBottom {
		m.viewport.GotoBottom()
	}
	m.searchInput.SetWidth(max(1, inner-24))

	// Supported layouts: 60×16 compact, 80×24 normal, 120×35 roomy.
	// Below compact, drop optional question/description rows before reducing
	// the query, current selection or portable completion control.
	m.modalWidth = max(1, min(76, m.width-8))
	m.modalHeight = max(1, min(22, m.height-4))
	if m.width <= 60 {
		m.modalWidth = max(1, m.width-4)
	}
	if m.height < 16 {
		m.modalHeight = max(1, m.height-2)
	}
	m.detailViewport.SetWidth(m.modalWidth)
	m.detailViewport.SetHeight(max(1, m.modalHeight-1))
	m.detailViewport.MouseWheelEnabled = m.mouse
	questionHeight := 0
	if m.dialog != nil && m.dialog.description != "" && m.height >= 16 {
		questionHeight = min(3, max(1, strings.Count(ansi.Hardwrap(safeText(m.dialog.description), m.modalWidth, true), "\n")+1))
		if m.height < 24 {
			questionHeight = 1
		}
	}
	m.questionViewport.SetWidth(m.modalWidth)
	m.questionViewport.SetHeight(questionHeight)
	m.questionViewport.MouseWheelEnabled = m.mouse && m.dialog != nil && m.dialog.details
	if m.dialog != nil && m.detail == "" {
		if m.dialog.details {
			m.questionViewport.SetHeight(max(1, m.modalHeight-1))
		} else if m.dialog.kind == client.EnterText {
			m.modalHeight = min(m.modalHeight, questionHeight+8)
		} else {
			m.styleList(&m.dialog.list)
			rowHeight := 1
			if m.height >= 24 && m.modalWidth >= 48 {
				rowHeight = 2
			}
			// Query, current selection and help are fixed rows. Completion is
			// outside the native filterable list, so even zero matches can finish.
			fixed := questionHeight + 3
			if m.dialog.kind == client.ChooseMany {
				fixed++
			}
			m.modalHeight = min(m.modalHeight, fixed+max(1, rowHeight*len(m.dialog.list.Items())))
			m.dialog.list.SetShowPagination(m.height >= 24)
			m.dialog.list.SetSize(m.modalWidth, max(1, m.modalHeight-fixed))
			// The query is rendered independently of the native list's title
			// and spinner. Give its native textinput the entire remaining width.
			m.dialog.list.FilterInput.SetWidth(max(1, m.modalWidth-ansi.StringWidth(m.dialog.list.FilterInput.Prompt)-1))
		}
	}
	m.modalInput.SetWidth(m.modalWidth)
	m.modalInput.SetHeight(max(1, m.modalHeight-questionHeight-1))
}

func (m *Model) View() tea.View {
	p := colors(m.theme)
	if m.closed {
		return tea.NewView("")
	}
	if m.width < 20 || m.height < 8 {
		return m.terminalView(clipBlock(p.active().Render("t3oxide")+"\n"+p.subtle().Render("Resize terminal · Ctrl+Q exits"), m.width, m.height))
	}
	inner := m.workspaceWidth()
	contextLine := m.status.Context
	if contextLine == "" {
		contextLine = "Your T3 workspace"
	}
	header := p.active().Render("t3oxide") + "  " + p.plain().Render(fitLine(contextLine, max(1, inner-9)))
	model := p.subtle().Render(fitLine(modelStatusLine(m.status.ModelName), inner))
	state := m.status.State
	if state == "" {
		state = "connecting"
	}
	if m.stopBusy {
		state = commandProgress("stop")
	} else if m.actionBusy {
		if m.dialog != nil {
			state = "Choose an option · draft preserved"
		} else {
			state = commandProgress(m.actionName)
		}
	}
	position := "latest"
	if m.snapshot != nil && m.snapshot.Metadata.Historical {
		position = "history · /latest"
	} else if !m.follow {
		position = fmt.Sprintf("reading %d%% · End latest", int(m.viewport.ScrollPercent()*100))
	}
	status := edgeLine(p.subtle().Render(fitLine(state, max(1, inner-ansi.StringWidth(position)-3))), p.subtle().Render(position), inner)
	body := m.bodyView()
	if m.sidebar && m.width >= 110 {
		body = lipgloss.JoinHorizontal(lipgloss.Top, body, "  ", m.sidebarView(28, m.bodyHeight))
	}
	feedback := m.feedbackView(inner)
	label := "Message"
	if m.status.Delivery != "" && m.status.Delivery != "auto" {
		label += " · " + m.status.Delivery
	}
	if m.status.NextModel != "" {
		label = "Next send · " + m.status.NextModel
	}
	if m.preservedDraft != "" {
		label = "Exact draft"
	}
	if m.transcriptFocus {
		label = "Reading transcript · Tab to write"
	}
	if m.searching {
		label = "Find in current page"
	}
	composer := m.composer.View()
	if m.searching {
		composer = m.searchInput.View()
	}
	frame := framed(label, padBlock(composer, max(1, inner-4), m.composer.Height()), inner-4, p, m.composer.Focused() || m.searching)
	content := strings.Join([]string{header, model, status, padBlock(body, inner, m.bodyHeight), feedback, frame, m.footerView(inner)}, "\n")
	content = padBlock(content, inner, m.height)
	margin := max(0, (m.width-inner)/2)
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = strings.Repeat(" ", margin) + line
	}
	canvas := padBlock(strings.Join(lines, "\n"), m.width, m.height)
	if m.completionVisible() && m.height >= 12 && m.width >= 32 {
		panel := m.completionView(inner)
		x := margin
		y := max(3, 3+m.bodyHeight-lipgloss.Height(panel))
		canvas = lipgloss.NewCompositor(lipgloss.NewLayer(canvas), lipgloss.NewLayer(panel).X(x).Y(y).Z(1)).Render()
	}
	// Use the pinned native compositor instead of string slicing ANSI cells.
	if m.dialog != nil || m.detail != "" {
		panel := m.modalView()
		x, y := max(0, (m.width-lipgloss.Width(panel))/2), max(0, (m.height-lipgloss.Height(panel))/2)
		canvas = lipgloss.NewCompositor(lipgloss.NewLayer(canvas), lipgloss.NewLayer(panel).X(x).Y(y).Z(1)).Render()
	}
	return m.terminalView(clipBlock(canvas, m.width, m.height))
}

func (m *Model) terminalView(content string) tea.View {
	p := colors(m.theme)
	v := tea.NewView(content)
	v.AltScreen = true
	v.ReportFocus = true
	// Bubble Tea owns OSC color setup/reset across theme changes, suspend and
	// exit. Nil releases fixed overrides back to the terminal's own defaults.
	if m.theme != "terminal" {
		v.BackgroundColor = lipgloss.Color(p.bg)
		v.ForegroundColor = lipgloss.Color(p.fg)
	}
	if m.mouse {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

func (m *Model) bodyView() string {
	p := colors(m.theme)
	if m.transcript == "" {
		text := p.plain().Render("F6  Conversations") + "\n" + p.plain().Render("F3  Commands")
		if m.snapshot != nil {
			text = p.subtle().Render("Preparing conversation…")
		}
		return padBlock("\n"+text, m.contentWidth, m.bodyHeight)
	}
	return m.viewport.View()
}

func (m *Model) feedbackView(width int) string {
	p := colors(m.theme)
	text, style := m.notice, p.subtle()
	if m.status.Requests > 0 && text == "" {
		text = fmt.Sprintf("%d request(s) need you · Ctrl+R review", m.status.Requests)
		style = p.active()
	}
	if m.streamProblem != "" {
		text = m.feedbackSummary(width)
		style = p.plain().Foreground(lipgloss.Color(p.error))
	}
	if m.problem != "" {
		text = m.feedbackSummary(width)
		style = p.plain().Foreground(lipgloss.Color(p.error))
	}
	return style.Render(fitLine(text, width))
}

func (m *Model) modalView() string {
	p := colors(m.theme)
	w, h := m.modalWidth, m.modalHeight
	if m.detail != "" {
		return framed(m.detailTitle, padBlock(m.detailViewport.View(), w, max(1, h-1))+"\n"+m.hints(w, "Esc", "back", "Ctrl+Y", "copy", "↑/↓", "scroll"), w, p, true)
	}
	d := m.dialog
	if d == nil {
		return ""
	}
	if d.details {
		return framed(d.title, padBlock(m.questionViewport.View(), w, max(1, h-1))+"\n"+m.hints(w, "F2/Esc", "back", "↑/↓", "scroll"), w, p, true)
	}
	var parts []string
	if m.questionViewport.Height() > 0 {
		parts = append(parts, m.questionViewport.View())
	}
	detailLabel := "details"
	if m.problem != "" || m.streamProblem != "" {
		detailLabel = "error"
	}
	controls := m.hints(w, "Enter", "select", "type", "filter", "F2", detailLabel, "Esc", "close")
	if d.kind == client.EnterText {
		controls = m.hints(w, "Enter", "answer", "Ctrl+J", "newline", "F2", detailLabel, "Esc", "close")
		if d.preservedInitial != "" {
			controls = m.hints(w, "F2", "review", "Ctrl+Y", "copy", "Ctrl+U", "replace", "Esc", "close")
		}
		parts = append(parts, padBlock(m.modalInput.View(), w, max(1, h-m.questionViewport.Height()-1)), controls)
		return framed(d.title, strings.Join(parts, "\n"), w, p, true)
	}
	if d.prompt != nil && d.prompt.Question.Navigation {
		controls = m.hints(w, "Enter", "open", "type", "filter", "Space", "expand", "F2", detailLabel, "Esc", "close")
	}
	if d.kind == client.ChooseMany {
		if d.finishFocused {
			controls = m.hints(w, "Enter", "continue", "Tab", "choices", "F2", detailLabel, "Esc", "close")
		} else {
			controls = m.hints(w, "Tab", "continue", "Enter", "toggle", "type", "filter", "F2", detailLabel, "Esc", "close")
		}
	}
	// Keep the native query editor visible before typing and when completion
	// owns focus. The selected-result row stays visible even when Bubbles dims
	// candidates during filtering, and F2 retains complete offered text.
	parts = append(parts, ansi.Truncate(d.list.FilterInput.View(), w, ""), padBlock(d.list.View(), w, d.list.Height()), m.pickerSelectionView(w))
	if d.kind == client.ChooseMany {
		count := 0
		for _, selected := range d.selected {
			if selected {
				count++
			}
		}
		label := fmt.Sprintf("Continue · %d selected", count)
		if w < 25 {
			label = fmt.Sprintf("Continue (%d)", count)
		}
		style := p.subtle()
		if d.finishFocused {
			label = "> " + label
			style = p.selection()
		}
		parts = append(parts, style.Render(fitLine(label, w)))
	}
	parts = append(parts, controls)
	return framed(d.title, strings.Join(parts, "\n"), w, p, true)
}

func (m *Model) pickerSelectionView(width int) string {
	d := m.dialog
	if item, ok := d.list.SelectedItem().(choiceItem); ok {
		return colors(m.theme).active().Render(fitLine("→ "+item.title, width))
	}
	return colors(m.theme).subtle().Render(fitLine("No matches", width))
}

func framed(title, content string, width int, p palette, focused bool) string {
	border := p.muted
	if focused {
		border = p.accent
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(border)).Background(lipgloss.Color(p.bg)).Padding(0, 1).Render(content)
	label := p.active().Background(lipgloss.Color(p.bg)).Render(" " + fitLine(title, max(1, width-2)) + " ")
	return lipgloss.NewCompositor(lipgloss.NewLayer(box), lipgloss.NewLayer(label).X(2).Y(0).Z(1)).Render()
}

func (m *Model) footerView(width int) string {
	if m.dialog != nil || m.detail != "" {
		// The modal already renders its local controls. Keep only the shared
		// global bindings below it, with Stop first at compact widths.
		k := m.keyMap()
		return m.bindingHelp(width, contextHelp{short: []key.Binding{k.Stop, k.Quit, k.Help, k.Error}})
	}
	if width < 40 {
		k := m.keyMap()
		palette, stop := k.Palette, k.Stop
		palette.SetHelp("F3", "commands")
		stop.SetHelp("^S", "Stop")
		return m.bindingHelp(width, contextHelp{short: []key.Binding{palette, stop}})
	}
	return m.bindingHelp(width, m.helpMap())
}

func (m *Model) bindingHelp(width int, keys help.KeyMap) string {
	p := colors(m.theme)
	h := help.New()
	h.SetWidth(width)
	h.ShortSeparator = "  "
	h.Styles.ShortKey = p.plain()
	h.Styles.ShortDesc = p.subtle()
	h.Styles.ShortSeparator = p.subtle()
	h.Styles.Ellipsis = p.subtle()
	return h.View(keys)
}

// Bubbles help handles width-aware packing and truncation of whole bindings.
func (m *Model) hints(width int, pairs ...string) string {
	p := colors(m.theme)
	h := help.New()
	h.SetWidth(width)
	h.ShortSeparator = "  "
	h.Styles.ShortKey = p.plain()
	h.Styles.ShortDesc = p.subtle()
	h.Styles.ShortSeparator = p.subtle()
	if p.name == "terminal" {
		h.Styles.Ellipsis = p.subtle()
	}
	keys := make([]key.Binding, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		keys = append(keys, key.NewBinding(key.WithKeys(pairs[i]), key.WithHelp(pairs[i], pairs[i+1])))
	}
	return h.ShortHelpView(keys)
}

func (m *Model) sidebarView(width, height int) string {
	p := colors(m.theme)
	connected := "Reconnecting"
	if m.status.Connected {
		connected = "Connected"
	}
	access := "Read only"
	if m.status.Writable {
		access = "Writes enabled"
	}
	lines := []string{p.active().Render("Workspace"), "", fitLine(m.status.ProfileID, width), connected, access, "", fmt.Sprintf("%d pending request(s)", m.status.Requests), "", "F6      Conversations", "/env    Environments", "Ctrl+R  Requests", optionKey("M") + "      Model", optionKey("E") + "      Effort", optionKey("S") + "      Speed", "/sidebar off"}
	return padBlock(p.subtle().Render(strings.Join(lines, "\n")), width, height)
}

func edgeLine(left, right string, width int) string {
	gap := width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 2 {
		return ansi.Truncate(left, width, "")
	}
	return left + strings.Repeat(" ", gap) + right
}

// ANSI-aware clipping preserves grapheme widths and never splits escape codes.
func clipBlock(s string, width, height int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, max(0, width), "")
	}
	return strings.Join(lines, "\n")
}
func padBlock(s string, width, height int) string {
	lines := strings.Split(clipBlock(s, width, height), "\n")
	for i, line := range lines {
		lines[i] = line + strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))
	}
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", max(0, width)))
	}
	return strings.Join(lines, "\n")
}
func modelStatusLine(selection string) string {
	if selection == "" {
		return "Model unreported"
	}
	return "Thread: " + selection
}
