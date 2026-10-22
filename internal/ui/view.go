package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
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

	// Lip Gloss owns the overlay frame/composition. Bubbles owns the content:
	// allocate its usable dimensions after accounting for the frame and footer.
	m.modalWidth = max(1, min(76, m.width-8))
	m.modalHeight = max(3, min(22, m.height-6))
	if m.height < 14 {
		m.modalWidth = max(1, m.width-6)
		m.modalHeight = max(3, m.height-4)
	}
	m.detailViewport.SetWidth(m.modalWidth)
	m.detailViewport.SetHeight(max(1, m.modalHeight-2))
	m.detailViewport.MouseWheelEnabled = m.mouse
	questionHeight := 0
	if m.dialog != nil && m.dialog.description != "" {
		questionHeight = min(3, max(1, strings.Count(ansi.Hardwrap(safeText(m.dialog.description), m.modalWidth, true), "\n")+1))
		questionHeight = min(questionHeight, max(1, m.modalHeight/4))
	}
	m.questionViewport.SetWidth(m.modalWidth)
	m.questionViewport.SetHeight(questionHeight)
	m.questionViewport.MouseWheelEnabled = m.mouse && m.dialog != nil && m.dialog.details
	if m.dialog != nil && m.detail == "" {
		if m.dialog.details {
			m.questionViewport.SetHeight(max(1, m.modalHeight-2))
		} else if m.dialog.kind == client.EnterText {
			m.modalHeight = min(m.modalHeight, questionHeight+8)
		} else {
			m.styleList(&m.dialog.list)
			rowHeight := 1
			if m.height >= 24 && m.modalWidth >= 48 {
				rowHeight = 2
			}
			// Include native filter and pagination rows; short pickers stay short.
			m.modalHeight = min(m.modalHeight, max(7, questionHeight+4+rowHeight*len(m.dialog.list.Items())))
			m.dialog.list.SetSize(m.modalWidth, max(1, m.modalHeight-questionHeight-2))
		}
	}
	m.modalInput.SetWidth(m.modalWidth)
	m.modalInput.SetHeight(max(1, m.modalHeight-questionHeight-2))
}

func (m *Model) View() tea.View {
	p := colors(m.theme)
	if m.closed {
		return tea.NewView("")
	}
	if m.width < 20 || m.height < 8 {
		return m.terminalView(clipBlock(p.active().Render("t3oxide")+"\n"+p.subtle().Render("Resize terminal · Ctrl+D exits"), m.width, m.height))
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
		position = "history · Alt+→ latest"
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
		label = "Saved draft · F2 review · Ctrl+Y copy"
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
		text := p.active().Render("Your conversations, one focused workspace.") + "\n\n" + p.plain().Render("Ctrl+T  Open a conversation") + "\n" + p.plain().Render("Ctrl+P  Find a command") + "\n" + p.subtle().Render("Type a message below. Ctrl+J adds a line.")
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
		text = m.streamProblem
		style = p.plain().Foreground(lipgloss.Color(p.error))
	}
	if m.problem != "" {
		text = m.problem
		style = p.plain().Foreground(lipgloss.Color(p.error))
	}
	return style.Render(fitLine(text, width))
}

func (m *Model) modalView() string {
	p := colors(m.theme)
	w, h := m.modalWidth, m.modalHeight
	if m.detail != "" {
		return framed(m.detailTitle, padBlock(m.detailViewport.View(), w, max(1, h-2))+"\n"+m.hints(w, "↑/↓", "scroll", "Esc", "back"), w, p, true)
	}
	d := m.dialog
	if d == nil {
		return ""
	}
	if d.details {
		return framed(d.title, padBlock(m.questionViewport.View(), w, max(1, h-2))+"\n"+m.hints(w, "↑/↓", "scroll", "F2/Esc", "back"), w, p, true)
	}
	var parts []string
	if m.questionViewport.Height() > 0 {
		parts = append(parts, m.questionViewport.View())
	}
	controls := m.hints(w, "↑/↓", "choose", "Enter", "select", "type", "filter", "F2", "details", "Esc", "close")
	if d.prompt != nil && d.prompt.Question.Navigation {
		controls = m.hints(w, "Enter", "open", "Space", "expand", "F6", "types", "F2", "details", "Esc", "close")
	}
	content := d.list.View()
	if d.kind == client.EnterText {
		content = m.modalInput.View()
		controls = m.hints(w, "Enter", "answer", "Ctrl+J", "newline", "F2", "details", "Esc", "close")
	} else if len(d.list.VisibleItems()) == 0 {
		content = d.list.FilterInput.View() + "\n\n" + p.subtle().Render("No matches. Change the filter or Esc to close.")
	}
	if d.kind == client.ChooseMany {
		controls = m.hints(w, "Space", "toggle", "Ctrl+Enter", "continue", "F2", "details", "Esc", "close")
	}
	if d.preservedInitial != "" {
		controls = m.hints(w, "F2", "review", "Ctrl+Y", "copy", "Ctrl+U", "replace", "Esc", "close")
	}
	if d.list.FilterState() == list.Filtering && d.kind != client.EnterText {
		controls = m.hints(w, "↑/↓", "choose", "Enter", "select", "Ctrl+U", "clear", "Esc", "close")
	}
	if w < 55 {
		controls = m.hints(w, "F2", "details", "Enter", "select", "Esc", "close")
		if d.kind == client.EnterText {
			controls = m.hints(w, "F2", "details", "Ctrl+J", "newline", "Enter", "answer")
		}
	}
	if m.questionViewport.TotalLineCount() > m.questionViewport.Height() {
		controls = m.hints(w, "F2", "full details", "Enter", "select", "Esc", "close")
	}
	if d.kind == client.ChooseMany {
		controls = m.hints(w, "Ctrl+Enter", "continue", "Space", "toggle", "F2", "details", "Esc", "close")
		if w < 55 {
			controls = m.hints(w, "^Enter", "continue", "F2", "details")
		}
	}
	parts = append(parts, padBlock(content, w, max(1, h-m.questionViewport.Height()-2)), controls)
	return framed(d.title, strings.Join(parts, "\n"), w, p, true)
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
	if width < 40 {
		return m.hints(width, "^P", "commands", "^S", "Stop")
	}
	if m.detail != "" {
		return m.hints(width, "Esc", "back", "Ctrl+S", "Stop/hold", "Ctrl+D", "exit")
	}
	if m.dialog != nil {
		return m.hints(width, "Esc", "close dialog", "Ctrl+S", "Stop/hold", "Ctrl+P", "help")
	}
	if m.searching {
		return m.hints(width, "Enter", "find", "Esc", "cancel", "Ctrl+S", "Stop/hold")
	}
	if m.transcriptFocus {
		return m.hints(width, "Tab", "write", "↑/↓", "scroll", "n/N", "matches", "Ctrl+P", "commands", "Ctrl+S", "Stop/hold")
	}
	if width < 76 {
		return m.hints(width, "Ctrl+P", "commands", "Ctrl+S", "Stop/hold", "Ctrl+J", "newline")
	}
	return m.hints(width, "Enter", "send", "Ctrl+J", "newline", "Ctrl+P", "commands", "Ctrl+T", "threads", "Ctrl+S", "Stop/hold")
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
	lines := []string{p.active().Render("Workspace"), "", fitLine(m.status.ProfileID, width), connected, access, "", fmt.Sprintf("%d pending request(s)", m.status.Requests), "", "Ctrl+T  Conversations", "Ctrl+E  Environments", "Ctrl+R  Requests", "Alt+M   Model", "Alt+E   Effort", "Alt+S   Speed", "Ctrl+B  Hide sidebar"}
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
		return "Model unreported · Alt+M model · Ctrl+P commands"
	}
	return "Thread: " + selection + " · Alt+M model"
}
