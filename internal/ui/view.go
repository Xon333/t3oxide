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
	if m.mainWidth() < 60 {
		margin = 2
	}
	if m.contextVisible() {
		limit += 30
	}
	return max(1, min(m.mainWidth()-margin, limit))
}

func (m *Model) resize() {
	inner := m.workspaceWidth()
	m.contentWidth = inner
	if m.contextVisible() {
		m.contentWidth -= 30
	}
	// Bubbles computes wrapped visual rows. Its high MaxHeight remains intact
	// so the display cap cannot truncate a long human draft (see components.go).
	m.composer.SetWidth(max(1, m.contentWidth-4))
	m.composer.SetHeight(min(m.composer.Height(), max(1, min(5, m.height/6))))
	m.resizeTranscript()
	m.searchInput.SetWidth(max(1, inner-4))
	m.resizeDialog()
	m.resizeNavigator()
}

func (m *Model) resizeDialog() {
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
	m.surfaceHelpHeight = m.surfaceHelpRows()
	surfaceHeight := m.height - 2 - m.surfaceHelpHeight
	if m.feedbackVisible() {
		surfaceHeight -= lipgloss.Height(m.feedbackView(m.workspaceWidth()))
	}
	if m.browserPage || m.launchPage {
		// Full-page allocation pays for the modal's border and horizontal padding.
		m.modalWidth = max(1, min(m.modalWidth, m.workspaceWidth()-4))
		m.modalHeight = max(1, min(22, surfaceHeight-2))
	}
	m.detailViewport.SetWidth(m.modalWidth)
	helpHeight := 1
	if m.browserPage || m.launchPage {
		helpHeight = 0
	}
	m.detailViewport.SetHeight(max(1, m.modalHeight-helpHeight))
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
	m.pickerSelectionShown = m.pickerSelectionView(m.modalWidth) != ""
	if m.dialog != nil && m.detail == "" {
		if m.dialog.details {
			m.questionViewport.SetHeight(max(1, m.modalHeight-helpHeight))
		} else if m.dialog.settingsForm != nil {
			m.modalHeight = min(m.modalHeight, m.modelSettingsPreferredHeight())
			return
		} else if m.dialog.requestForm != nil {
			m.modalHeight = min(m.modalHeight, m.requestFormPreferredHeight())
			m.modalInput.SetWidth(m.modalWidth)
			m.modalInput.SetHeight(max(1, min(4, m.modalHeight-7)))
			return
		} else if m.dialog.launch != nil {
			m.modalInput.SetWidth(max(1, m.workspaceWidth()-6))
			m.modalInput.SetHeight(max(1, min(8, surfaceHeight-8)))
			m.launchTitle.SetWidth(max(1, m.workspaceWidth()-12))
			return
		} else if m.dialog.kind == client.EnterText {
			m.modalHeight = min(m.modalHeight, questionHeight+8)
		} else {
			m.styleList(&m.dialog.list)
			// Query, current selection and help are fixed rows. Completion is
			// outside the native filterable list, so even zero matches can finish.
			fixed := questionHeight + 1 + helpHeight
			if m.pickerSelectionShown {
				fixed++
			}
			if m.dialog.kind == client.ChooseMany {
				fixed++
			}
			if m.dialog.surface == client.SurfaceBrowser && m.dialog.kind != client.ChooseMany {
				fixed += m.browserControlsHeight(m.modalWidth)
			}
			// Native pagination has its own styled rows. Reserving pagination in
			// a panel sized to exactly N choices used to leave only one item.
			count := max(1, len(m.dialog.list.VisibleItems()))
			available := max(1, m.modalHeight-fixed)
			paginated := count > available && m.height >= 24
			m.dialog.list.SetShowPagination(paginated)
			if paginated {
				count += 2
			}
			m.modalHeight = min(m.modalHeight, fixed+count)
			m.dialog.list.SetSize(m.modalWidth, max(1, m.modalHeight-fixed))
			// The query is rendered independently of the native list's title
			// and spinner. Give its native textinput the entire remaining width.
			m.dialog.list.FilterInput.SetWidth(max(1, m.modalWidth-ansi.StringWidth(m.dialog.list.FilterInput.Prompt)-1))
		}
	}
	m.modalInput.SetWidth(m.modalWidth)
	m.modalInput.SetHeight(max(1, m.modalHeight-questionHeight-helpHeight))
	if m.dialog != nil && m.dialog.prompt != nil && m.dialog.prompt.Question.SingleLine {
		m.modalInput.SetHeight(1)
	}
}

func (m *Model) surfaceHelpRows() int {
	if m.browserPage || m.launchPage {
		return lipgloss.Height(m.dialogHelp(m.workspaceWidth(), true))
	}
	return 0
}

func (m *Model) resizeTranscript() {
	wasBottom := m.viewport.AtBottom()
	m.feedbackShown = m.feedbackVisible()
	feedbackHeight := 0
	if m.feedbackShown {
		feedbackHeight = 1
	}
	m.bodyHeight = max(1, m.height-m.composer.Height()-3-m.transcriptTop()-feedbackHeight-m.activityStripRows())
	m.viewport.SetWidth(m.contentWidth)
	m.viewport.SetHeight(m.bodyHeight)
	// SetHeight does not clamp an existing offset. Preserve valid anchors and
	// let the native viewport bound the last page when reclaimed space grows it.
	m.viewport.SetYOffset(m.viewport.YOffset())
	m.viewport.MouseWheelEnabled = m.mouse
	if m.follow && wasBottom {
		m.viewport.GotoBottom()
	}
}

func (m *Model) View() tea.View {
	p := colors(m.theme)
	if m.closed {
		return tea.NewView("")
	}
	if m.width < 20 || m.height < 8 {
		return m.terminalView(clipBlock(p.active().Render("t3oxide")+"\n"+p.subtle().Render("Resize terminal · Ctrl+Q exits"), m.width, m.height))
	}
	if m.browserPage || m.launchPage {
		m.recordWorkspaceHits()
		return m.terminalView(m.fullSurfaceView())
	}
	if m.mode == "agentbox" {
		return m.agentboxView()
	}
	if m.session != nil && m.status.ThreadID == "" || m.navigationVisible() && m.navigatorWidth() == m.width {
		canvas := m.emptyWorkspaceView()
		if m.navigationVisible() {
			canvas = m.navigatorView(m.navigatorWidth(), m.height)
			if m.navigatorWidth() < m.width {
				canvas = lipgloss.JoinHorizontal(lipgloss.Top, canvas, m.emptyWorkspaceView())
			}
		}
		if m.dialog != nil || m.detail != "" {
			panel := m.modalView()
			canvas = lipgloss.NewCompositor(lipgloss.NewLayer(canvas), lipgloss.NewLayer(panel).X(max(0, (m.width-lipgloss.Width(panel))/2)).Y(max(0, (m.height-lipgloss.Height(panel))/2)).Z(1)).Render()
		}
		m.recordWorkspaceHits()
		return m.terminalView(clipBlock(canvas, m.width, m.height))
	}
	inner := m.workspaceWidth()
	contextLine := m.status.Context
	if m.status.Location.Environment != "" {
		var parts []string
		for _, value := range []string{m.status.Location.Environment, m.status.Location.Title} {
			if value != "" {
				parts = append(parts, value)
			}
		}
		contextLine = strings.Join(parts, " → ")
	}
	header := p.active().Render("t3oxide") + "  " + p.plain().Render(fitLine(contextLine, max(1, inner-9)))
	if m.navigationVisible() {
		header = p.active().Render("t3oxide") + "  " + p.plain().Bold(true).Render(fitLine(contextLine, max(1, inner-9)))
	}
	state := m.authoritativeStatus()
	if state == "" {
		state = "connecting"
	}
	if conn := m.status.ConnectionState; conn != "" && conn != "connected" && conn != "synchronized" {
		state = conn + " · " + state
	}
	if m.status.ThreadID != "" && !m.status.Writable {
		state = "Read only · " + state
	}
	stateWidth := min(ansi.StringWidth(state), max(16, inner*2/3))
	model := p.plain().Bold(true).Render(fitLine(modelStatusLine(m.status.ModelName), max(1, inner-stateWidth-2)))
	status := edgeLine(model, p.operational().Render(fitLine(state, stateWidth)), inner)
	toolbar := m.toolbarView(inner)
	body := m.bodyView()
	if m.contextVisible() {
		body = lipgloss.JoinHorizontal(lipgloss.Top, body, "  ", m.contextView(28, m.bodyHeight))
	}
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
	if m.searching {
		label = "Find in current page"
	}
	if m.composer.Focused() || m.searching {
		label = "> " + label
	}
	composer := m.composer.View()
	if m.searching {
		composer = m.searchInput.View()
	}
	frame := framed(label, padBlock(composer, max(1, m.contentWidth-4), m.composer.Height()), m.contentWidth-4, p, m.composer.Focused() || m.searching)
	parts := []string{header, status}
	if toolbar != "" {
		parts = append(parts, toolbar)
	}
	if operation := m.operationLine(); operation != "" {
		parts = append(parts, m.operationView(inner))
	}
	parts = append(parts, padBlock(body, inner, m.bodyHeight))
	if m.activityStripRows() > 0 {
		parts = append(parts, "")
	}
	if strip := m.activityStripView(inner); strip != "" {
		parts = append(parts, strip)
	}
	if m.feedbackVisible() {
		parts = append(parts, m.feedbackView(inner))
	}
	parts = append(parts, frame, m.footerView(inner))
	content := strings.Join(parts, "\n")
	content = padBlock(content, inner, m.height)
	margin := max(0, (m.mainWidth()-inner)/2)
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = strings.Repeat(" ", margin) + line
	}
	canvas := padBlock(strings.Join(lines, "\n"), m.mainWidth(), m.height)
	if m.navigationVisible() {
		canvas = lipgloss.JoinHorizontal(lipgloss.Top, m.navigatorView(m.navigatorWidth(), m.height), canvas)
	}
	if m.completionVisible() && m.height >= 12 && m.width >= 32 {
		panel := m.completionView(inner)
		x := m.mainOriginX()
		y := max(m.transcriptTop(), m.transcriptTop()+m.bodyHeight-lipgloss.Height(panel))
		canvas = lipgloss.NewCompositor(lipgloss.NewLayer(canvas), lipgloss.NewLayer(panel).X(x).Y(y).Z(1)).Render()
	}
	// Use the pinned native compositor instead of string slicing ANSI cells.
	if m.dialog != nil || m.detail != "" {
		panel := m.modalView()
		x, y := max(0, (m.width-lipgloss.Width(panel))/2), max(0, (m.height-lipgloss.Height(panel))/2)
		canvas = lipgloss.NewCompositor(lipgloss.NewLayer(canvas), lipgloss.NewLayer(panel).X(x).Y(y).Z(1)).Render()
	}
	m.recordWorkspaceHits()
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
		text := p.plain().Render("Ctrl+O  Browser") + "\n" + p.plain().Render("Ctrl+P  Commands")
		if m.snapshot != nil {
			text = p.subtle().Render("Preparing conversation…")
		}
		return padBlock("\n"+text, m.contentWidth, m.bodyHeight)
	}
	return m.viewport.View()
}

func (m *Model) feedbackView(width int) string {
	if !m.feedbackVisible() {
		return ""
	}
	p := colors(m.theme)
	text, style := m.notice, p.subtle()
	if text == "" && m.actionBusy {
		text = commandProgress(m.actionName)
	}
	if m.stopBusy {
		text = commandProgress("stop")
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

func (m *Model) feedbackVisible() bool {
	return m.notice != "" || m.actionBusy || m.stopBusy || m.problem != "" || m.streamProblem != ""
}

func (m *Model) modalView() string {
	p := colors(m.theme)
	w, h := m.modalWidth, m.modalHeight
	localHelp := ""
	if !m.browserPage && !m.launchPage {
		localHelp = m.dialogHelp(w, false)
	}
	helpHeight := 0
	if localHelp != "" {
		helpHeight = 1
		localHelp = "\n" + localHelp
	}
	if m.detail != "" {
		return framed(m.detailTitle, padBlock(m.detailViewport.View(), w, max(1, h-helpHeight))+localHelp, w, p, true)
	}
	d := m.dialog
	if d == nil {
		return ""
	}
	if d.settingsForm != nil && !d.details {
		return framed(d.title, m.modelSettingsView(w, h-helpHeight)+localHelp, w, p, true)
	}
	if d.requestForm != nil && !d.details {
		return framed(d.title, m.requestFormView(w, h-helpHeight)+localHelp, w, p, true)
	}
	if d.details {
		return framed(d.title, padBlock(m.questionViewport.View(), w, max(1, h-helpHeight))+localHelp, w, p, true)
	}
	var parts []string
	if d.surface == client.SurfaceBrowser && d.kind != client.ChooseMany {
		parts = append(parts, m.browserControls(w))
	}
	if m.questionViewport.Height() > 0 {
		parts = append(parts, m.questionViewport.View())
	}
	if d.kind == client.EnterText {
		parts = append(parts, padBlock(m.modalInput.View(), w, max(1, h-m.questionViewport.Height()-helpHeight)))
		return framed(d.title, strings.Join(parts, "\n")+localHelp, w, p, true)
	}
	// Keep the native query editor visible before typing and when completion
	// owns focus. The selected-result row stays visible even when Bubbles dims
	// candidates during filtering, and F2 retains complete offered text.
	parts = append(parts, ansi.Truncate(d.list.FilterInput.View(), w, ""), padBlock(d.list.View(), w, d.list.Height()))
	if selection := m.pickerSelectionView(w); selection != "" {
		parts = append(parts, selection)
	}
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
	return framed(d.title, strings.Join(parts, "\n")+localHelp, w, p, true)
}

func (m *Model) pickerSelectionView(width int) string {
	d := m.dialog
	if d == nil || d.kind == client.EnterText || d.details || m.detail != "" {
		return ""
	}
	if item, ok := d.list.SelectedItem().(choiceItem); ok {
		title := safeText(item.title)
		matches := 0
		for _, candidate := range d.list.Items() {
			other, ok := candidate.(choiceItem)
			if ok && safeText(other.title) == title {
				matches++
			}
		}
		duplicate := matches > 1
		// The native delegate reserves two cells before the title. Preserve a
		// footer when that title is clipped, ambiguous, or has useful context.
		truncated := strings.Contains(title, "\n") || ansi.StringWidth(item.Title()) > max(0, width-2)
		description := ""
		if item.description != "" {
			description, _, _ = strings.Cut(safeText(item.description), "\n")
		}
		if !truncated && !duplicate && description == "" && item.args == "" && !item.disabled {
			return ""
		}
		text := "→ " + title
		if duplicate {
			identity := item.id
			if identity == "" {
				identity = strings.TrimSpace("/" + item.command + " " + item.args)
			}
			text = "→ " + safeText(identity) + " · " + title
		}
		if description != "" {
			text += " · " + description
		} else if item.args != "" {
			text += " · " + safeText(item.args)
		}
		return colors(m.theme).active().Render(fitLine(text, width))
	}
	return colors(m.theme).subtle().Render(fitLine("No matches", width))
}

// dialogHelp is the sole local-controls contract for modals and full pages.
// Full pages include the independent global lanes and wrap whole bindings so
// compact widths retain activation and Stop before secondary controls.
func (m *Model) dialogHelp(width int, fullSurface bool) string {
	detailLabel, escape := "Info", "close"
	info, copyKey, dismiss := m.actionBinding("info").Help().Key, m.actionBinding("copy").Help().Key, m.actionBinding("dismiss").Help().Key
	if m.problem != "" || m.streamProblem != "" {
		detailLabel = "error"
	}
	if fullSurface {
		escape = "resume"
	}
	pairs := []string{"Enter", "select", info, detailLabel, dismiss, escape}
	d := m.dialog
	switch {
	case m.detail != "":
		pairs = []string{dismiss, "back", copyKey, "copy", "↑/↓", "scroll"}
	case d != nil && d.details:
		pairs = []string{info + "/" + dismiss, "back", "↑/↓", "scroll"}
	case d != nil && (d.settingsForm != nil || d.requestForm != nil):
		pairs = m.formDialogHelpPairs()
	case d != nil && d.launch != nil:
		activation := "newline"
		if d.launch.focus == 1 {
			activation = "continue"
		} else if d.launch.focus >= 2 {
			actions := m.launchActions()
			if index := d.launch.focus - 2; index < len(actions) {
				activation = strings.ToLower(actionLabel(actions[index]))
			}
		}
		pairs = []string{"Enter", activation, "Tab", "controls", info, detailLabel, dismiss, escape}
	case d != nil && d.kind == client.EnterText:
		pairs = []string{"Enter", "answer", "Ctrl+J", "newline", info, detailLabel, dismiss, escape}
		if d.preservedInitial != "" {
			pairs = []string{info, "Info", copyKey, "copy", "Ctrl+U", "replace", dismiss, escape}
		}
	case d != nil && d.kind == client.ChooseMany:
		activation, tab := "toggle", "continue"
		if d.finishFocused {
			activation, tab = "continue", "choices"
		}
		pairs = []string{"Enter", activation, "Tab", tab, info, detailLabel, dismiss, escape}
		if !fullSurface && !d.finishFocused {
			// Keep the only portable completion route visible when native
			// single-row help truncates secondary bindings on narrow dialogs.
			pairs = []string{"Tab", tab, "Enter", activation, info, detailLabel, dismiss, escape}
		}
	case d != nil && d.surface == client.SurfaceBrowser:
		pairs = []string{"Enter", "open", "Tab", "controls", info, detailLabel, dismiss, escape}
	case d != nil && d.prompt != nil && d.prompt.Question.Navigation:
		pairs[1] = "open"
	}
	if !fullSurface {
		return m.hints(width, pairs...)
	}
	if d == nil && m.detail == "" {
		pairs = []string{dismiss, "resume"}
	}
	k := m.keyMap()
	stop, quit := k.Stop.Help(), k.Quit.Help()
	pairs = append([]string{pairs[0], pairs[1], stop.Key, stop.Desc}, pairs[2:]...)
	pairs = append(pairs, quit.Key, quit.Desc)
	var rows []string
	var row []string
	rowWidth := 0
	for i := 0; i+1 < len(pairs); i += 2 {
		bindingWidth := ansi.StringWidth(pairs[i]) + 1 + ansi.StringWidth(pairs[i+1])
		if len(row) > 0 && rowWidth+2+bindingWidth > width {
			rows = append(rows, m.hints(width, row...))
			row, rowWidth = nil, 0
		}
		if len(row) > 0 {
			rowWidth += 2
		}
		row = append(row, pairs[i:i+2]...)
		rowWidth += bindingWidth
	}
	if len(row) > 0 {
		rows = append(rows, m.hints(width, row...))
	}
	return strings.Join(rows, "\n")
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
	return ansi.Truncate(h.ShortHelpView(keys), width, "")
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
	lines := []string{p.active().Render("Workspace"), "", fitLine(m.status.ProfileID, width), connected, access, "", "Ctrl+O  Browser", "/env    Environments", "Ctrl+R  Requests", optionKey("M") + "      Model", optionKey("E") + "      Effort", optionKey("S") + "      Speed", "/sidebar off"}
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
	return selection
}
