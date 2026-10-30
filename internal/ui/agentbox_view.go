package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/wire"
	"github.com/charmbracelet/x/ansi"
)

func inboxViewLabel(view string) string {
	switch view {
	case "needs":
		return "Needs you"
	case "working":
		return "Working"
	case "queued":
		return "Queued"
	case "later":
		return "Later"
	case "done":
		return "Reviewed"
	case "unknown":
		return "Unknown"
	default:
		return "All"
	}
}

func (m *Model) agentboxView() tea.View {
	p := colors(m.theme)
	w, h := m.width, m.height
	geometry := m.inboxGeometry()
	left, right := geometry.left, geometry.right
	header := p.active().Render("AgentBox") + "  " + p.plain().Bold(true).Render(inboxViewLabel(m.agentbox.view))
	counts := fmt.Sprintf("%d", len(m.agentbox.rows))
	if m.agentbox.loading {
		counts = "Loading…"
	}
	header = edgeLine(header, p.subtle().Render(counts), w)
	foot := m.agentboxHelp(w)
	feedback := ""
	if m.feedbackVisible() {
		feedback = m.feedbackView(w)
	}
	if m.agentbox.error != "" {
		feedback = p.plain().Foreground(lipgloss.Color(p.error)).Render(fitLine(m.agentbox.error, w))
	}
	op := m.operationLine()
	composer := m.inboxComposerView()
	bodyHeight := geometry.body
	list := m.inboxListView(left, bodyHeight)
	detail := m.inboxDecisionView(right, bodyHeight)
	body := lipgloss.JoinHorizontal(lipgloss.Top, list, "  ", detail)
	parts := []string{header, body}
	if op != "" {
		parts = append(parts, m.operationView(w))
	}
	if feedback != "" {
		parts = append(parts, feedback)
	}
	if controls := m.inboxComposerControls(); controls.content != "" {
		parts = append(parts, controls.content)
	}
	if composer != "" {
		parts = append(parts, composer)
	}
	parts = append(parts, foot)
	canvas := padBlock(strings.Join(parts, "\n"), w, h)
	if m.dialog != nil || m.detail != "" {
		panel := m.modalView()
		x, y := max(0, (w-lipgloss.Width(panel))/2), max(0, (h-lipgloss.Height(panel))/2)
		canvas = lipgloss.NewCompositor(lipgloss.NewLayer(canvas), lipgloss.NewLayer(panel).X(x).Y(y).Z(1)).Render()
	}
	m.hitGeneration, m.layoutDirty = m.generation, false
	if m.dialog != nil || m.detail != "" {
		m.recordModalHits()
	} else {
		m.hitZones = m.agentboxHitZones()
	}
	return m.terminalView(clipBlock(canvas, w, h))
}

func (m *Model) inboxComposerView() string {
	if !m.agentbox.reply {
		return ""
	}
	label := "Reply"
	if m.busyComposer() || m.nextDelivery != "" {
		label += " · " + m.deliveryLabel()
	}
	if m.status.Location.Title != "" {
		label += " · " + m.status.Location.Title
	}
	if m.status.NextModel != "" {
		label += " · " + m.status.NextModel
	}
	w := max(1, m.width-4)
	return framed(safeText(label), padBlock(m.composer.View(), w, min(4, m.composer.Height())), w, colors(m.theme), true)
}

type inboxGeometry struct{ left, right, body, firstRow, rows, start int }

// Rendering and hit testing share the exact visible row allocation.
func (m *Model) inboxGeometry() inboxGeometry {
	g := inboxGeometry{left: min(44, max(30, m.width/3+8)), firstRow: 1}
	g.right = max(1, m.width-g.left-2)
	reserved := 2
	if m.feedbackVisible() || m.agentbox.error != "" {
		reserved++
	}
	if m.operationLine() != "" {
		reserved++
	}
	if composer := m.inboxComposerView(); composer != "" {
		reserved += lipgloss.Height(composer)
	}
	if m.agentbox.reply {
		reserved++
	}
	g.body = max(1, m.height-reserved)
	queryRows := 0
	if m.agentbox.searching || m.agentbox.query.Value() != "" {
		queryRows = 1
		g.firstRow++
	}
	g.rows = max(1, g.body-queryRows-2-m.inboxSelectionExtra(g.body-queryRows))
	g.start = max(0, max(0, m.inboxIndex())-g.rows+1)
	return g
}

func (m *Model) inboxHint(id, label string) key.Binding {
	b := m.actionBinding(id)
	b.SetHelp(b.Help().Key, label)
	return b
}

func (m *Model) inboxListView(width, height int) string {
	p := colors(m.theme)
	a := &m.agentbox
	var lines []string
	if a.searching {
		a.query.SetWidth(width)
		lines = append(lines, a.query.View())
	} else if a.query.Value() != "" {
		lines = append(lines, p.subtle().Render(fitLine("/ "+a.query.Value(), width)))
	}
	extra := m.inboxSelectionExtra(height - len(lines))
	available := max(1, height-len(lines)-2-extra)
	index := max(0, m.inboxIndex())
	start := max(0, index-available+1)
	end := min(len(a.rows), start+available)
	for i := start; i < end; i++ {
		row := a.rows[i]
		priority := ""
		if n, ok := a.prefs.Tasks[row.Target.Key()]; ok {
			priority = []string{"L", "M", "H", "U"}[n]
		}
		badge := threadStatusBadge(row.Thread)
		prefix := "  "
		if m.inboxAttached(row) {
			prefix = "* "
		}
		identity := projectBubble(row.Thread.Project, m.inboxMachineKind(row.Target.ProfileID))
		if priority != "" {
			prefix += priority + " "
		}
		if row.Target.Key() == a.selected {
			prefix = ">" + strings.TrimPrefix(prefix, " ")
		}
		if width < 28 {
			prefix = ""
			if row.Target.Key() == a.selected {
				prefix = ">"
			}
		}
		cue := ""
		if row.Thread.Inbox != nil {
			if row.Thread.Inbox.PendingRequestCount > 0 {
				cue = fmt.Sprintf("?%d ", row.Thread.Inbox.PendingRequestCount)
			} else if row.Thread.Inbox.PendingChildRequest {
				cue = "? "
			}
		}
		lead := prefix + identity + " " + cue
		leftWidth := max(1, width-ansi.StringWidth(badge.label)-2)
		titleWidth := max(0, leftWidth-ansi.StringWidth(lead))
		title := fitLine(safeText(row.Thread.Title), titleWidth)
		if titleWidth <= 1 {
			title = ansi.Truncate(safeText(row.Thread.Title), titleWidth, "")
		}
		left := fitLine(lead+title, leftWidth)
		line := edgeLine(left, badge.label, width)
		style := p.plain()
		if row.Target.Key() == a.selected {
			style = p.selection()
		}
		line = fitLine(line, width)
		marker := lipgloss.NewStyle().Foreground(lipgloss.Color(p.bg)).Background(lipgloss.Color(projectAccent(row.Target.EnvironmentID, row.Target.ProjectID, m.theme))).Bold(true)
		if p.name == "terminal" {
			marker = p.plain().Reverse(true)
		}
		rendered := renderStatusRow(line, identity, badge, p, style, marker)
		lines = append(lines, rendered)
		if row.Target.Key() == a.selected && extra > 0 {
			titleLines := expandedTitleLines(row.Thread.Title, max(1, width-2))
			if extra == 3 {
				lines = append(lines, p.selection().Render(fitLine("  "+titleLines[0], width)), p.selection().Render(fitLine("  "+titleLines[1], width)))
			} else {
				title := titleLines[1]
				if strings.TrimSpace(title) == "" {
					title = titleLines[0]
				}
				lines = append(lines, p.selection().Render(fitLine("  "+title, width)))
			}
			context := row.Thread.Project + " · " + row.Environment + " / " + row.Target.ProfileID
			if row.Thread.ProviderInstanceID != "" {
				context += " · " + row.Thread.ProviderInstanceID
			}
			lines = append(lines, p.subtle().Render(fitLine("  "+safeText(context), width)))
		}
	}
	if len(a.rows) == 0 {
		text := "No matching tasks"
		if !a.loaded && a.loading {
			text = "Loading metadata…"
		} else if a.view == "needs" && a.query.Value() == "" {
			text = "No needs-you items on this page"
			for _, source := range a.page.Sources {
				if source.Error != "" || !source.NativeWorkflows {
					text = "Native state unavailable"
					break
				}
			}
		}
		lines = append(lines, p.subtle().Render(text))
		if search := m.inboxEmptySearchSurface(width); search.content != "" {
			lines = append(lines, search.content)
		}
	}
	for len(lines) < height-2 {
		lines = append(lines, "")
	}
	state := m.inboxSourceHealth()
	lines = append(lines, p.subtle().Render(fitLine(state, width)), m.bindingHelp(width, contextHelp{short: []key.Binding{m.actionBinding("agentbox.view"), m.actionBinding("agentbox.search")}}))
	return padBlock(strings.Join(lines, "\n"), width, height)
}

func (m *Model) inboxDecisionView(width, height int) string {
	return m.inboxDecisionLayout(width, height).content
}

func (m *Model) inboxChoiceLabels() []string {
	if m.status.Requests > 0 {
		return []string{"Requests", "Conversation", "Later"}
	}
	if m.agentbox.ack != nil {
		return []string{"Next", "Back to inbox"}
	}
	return []string{"Changes", "Reply", "Reviewed"}
}

func (m *Model) inboxDecisionLayout(width, height int) renderedSurface {
	p := colors(m.theme)
	row, ok := m.selectedInbox()
	if !ok {
		return renderedSurface{content: padBlock("", width, height)}
	}
	var lines []string
	lines = append(lines, p.active().Bold(true).Render(fitLine(safeText(row.Thread.Title), width)))
	identity := row.Environment
	if row.Thread.Project != "" {
		identity += " · " + row.Thread.Project
	}
	lines = append(lines, p.subtle().Render(fitLine(safeText(identity), width)))
	badge := threadStatusBadge(row.Thread)
	status := badge.label
	if !m.inboxAttached(row) && row.Thread.Inbox != nil {
		if row.Thread.Inbox.PendingRequestCount > 0 {
			status += fmt.Sprintf(" · %d request(s)", row.Thread.Inbox.PendingRequestCount)
		} else if row.Thread.Inbox.PendingChildRequest {
			status += " · child request"
		}
	}
	if m.inboxAttached(row) {
		badge = threadStatusBadge(client.Thread{Status: m.status.RunState, Inbox: &wire.InboxMetadata{PendingRequestCount: m.status.Requests}})
		status = badge.label
		if m.status.Requests > 0 {
			label := "request"
			if m.status.Requests != 1 {
				label += "s"
			}
			status += fmt.Sprintf(" · %d %s", m.status.Requests, label)
		}
		if m.status.Historical {
			status = "earlier · live paused · " + status
		} else if connection := m.status.ConnectionState; connection != "" && connection != "connected" && connection != "live" {
			status = connection + " · " + status
		} else if !m.status.Connected {
			status = "reconnecting · " + status
		}
	}
	lines = append(lines, badge.style(p, p.plain()).Render(fitLine(safeText(status), width)))
	if m.agentbox.ack != nil && m.agentbox.ack.row.Target.Key() == row.Target.Key() {
		lines = append(lines, p.active().Render(fitLine(m.agentbox.ack.label+" acknowledged", width)))
	}
	if !m.agentbox.decision {
		lines = append(lines, "", m.bindingHelp(width, contextHelp{short: []key.Binding{m.actionBinding("agentbox.open")}}))
		return renderedSurface{content: padBlock(strings.Join(lines, "\n"), width, height)}
	}
	if !m.inboxAttached(row) {
		lines = append(lines, "", p.subtle().Render("Opening exact conversation…"))
		return renderedSurface{content: padBlock(strings.Join(lines, "\n"), width, height)}
	}
	if v := row.Thread.Inbox; v != nil {
		if v.Workspace != "" && v.Workspace != "root" {
			lines = append(lines, p.subtle().Render(fitLine("Worktree · "+safeText(v.Workspace), width)))
		}
		if len(v.PullRequests) > 0 {
			pr := v.PullRequests[0]
			lines = append(lines, p.subtle().Render(fitLine(fmt.Sprintf("PR #%d · %s", pr.Number, pr.State), width)))
		}
	}
	lines = append(lines, "")
	var controls []surfaceControl
	if !m.agentbox.reply {
		for i, label := range m.inboxChoiceLabels() {
			binding := m.inboxHint(fmt.Sprintf("agentbox.choice-%d", i+1), label).Help()
			text := p.subtle().Render(label)
			if binding.Key != "" {
				text = p.plain().Render(binding.Key) + " " + text
			}
			controls = append(controls, surfaceControl{label: text, action: "inbox-choice", args: strconv.Itoa(i + 1)})
		}
	}
	choices := controlSurface(width, controls)
	choiceRows := lipgloss.Height(choices.content)
	if m.agentbox.summary {
		text := client.InboxSummary(row)
		if m.status.Requests > 0 {
			text = fmt.Sprintf("%d formal request(s) need your exact answer.", m.status.Requests)
		} else if m.snapshot != nil && m.snapshot.Metadata.ThreadID == row.Target.ThreadID {
			for i := len(m.snapshot.Items) - 1; i >= 0; i-- {
				item := m.snapshot.Items[i]
				if item.Kind == "assistant_message" && item.SourceThreadID == row.Target.ThreadID && strings.TrimSpace(item.Text) != "" {
					text = item.Text
					if item.Truncated {
						text += "\n[Bounded message; inspect full history in T3]"
					}
					break
				}
			}
		}
		remaining := max(1, height-len(lines)-choiceRows-1)
		lines = append(lines, clipBlock(ansi.Wrap(safeText(text), width, ""), width, remaining))
	}
	used := lipgloss.Height(strings.Join(lines, "\n"))
	for used < height-choiceRows-1 {
		lines = append(lines, "")
		used++
	}
	hits := moveHits(choices.hits, 0, used)
	labels := m.inboxChoiceLabels()
	for i := range hits {
		index, _ := strconv.Atoi(hits[i].args)
		hits[i].target = row.Target
		hits[i].owner = fmt.Sprintf("%s/%d", labels[index-1], m.revision)
	}
	lines = append(lines, choices.content)
	if m.agentbox.reviewLoading {
		lines = append(lines, p.subtle().Render("Loading native changes…"))
	} else {
		lines = append(lines, m.bindingHelp(width, contextHelp{short: []key.Binding{m.actionBinding("agentbox.summary"), m.actionBinding("agentbox.developer")}}))
	}
	return renderedSurface{content: padBlock(strings.Join(lines, "\n"), width, height), hits: clipHits(hits, 0, 0, width, height)}
}

func (m *Model) agentboxHelp(width int) string {
	k := m.keyMap()
	if m.dialog != nil || m.detail != "" {
		return m.bindingHelp(width, contextHelp{short: []key.Binding{k.Stop, k.Quit, k.Help}})
	}
	if m.agentbox.reply {
		return m.footerView(width)
	}
	if m.agentbox.searching {
		if m.inboxServerSearchOffered() {
			return m.hints(width, "Enter", "server search", "Esc", "back")
		}
		return m.hints(width, "Enter", "search", "Esc", "back")
	}
	bindings := []key.Binding{m.actionBinding("agentbox.open"), m.actionBinding("agentbox.reply"), m.actionBinding("agentbox.file"), m.actionBinding("agentbox.view"), k.Stop}
	return m.bindingHelp(width, contextHelp{short: bindings})
}

func (m *Model) agentboxMouse(msg tea.MouseClickMsg) (bool, tea.Cmd) {
	if m.mode != "agentbox" || m.dialog != nil || m.detail != "" || m.browserPage || m.launchPage {
		return false, nil
	}
	if !m.mouse || msg.Button != tea.MouseLeft || m.layoutDirty || m.hitGeneration != m.generation {
		return true, nil
	}
	current := m.agentboxHitZones()
	for _, hit := range m.hitZones {
		if !hit.contains(msg.X, msg.Y) {
			continue
		}
		if !slices.Contains(current, hit) {
			return true, nil
		}
		m.layoutDirty = true
		switch hit.action {
		case "inbox-server-search":
			return true, m.searchInboxServer()
		case "inbox-compose":
			return true, m.toolbarAction(hit.args)
		case "inbox-recover":
			return true, m.execute("recover", "")
		case "inbox-row":
			m.agentbox.selected = hit.target.Key()
			return true, m.openInboxThread(false)
		case "inbox-choice":
			index, _ := strconv.Atoi(hit.args)
			return true, m.inboxOfferedChoice(index)
		}
		return true, nil
	}
	return true, nil
}

func (m *Model) agentboxHitZones() []hitZone {
	g := m.inboxGeometry()
	var hits []hitZone
	y := g.firstRow
	if len(m.agentbox.rows) == 0 {
		hits = append(hits, moveHits(m.inboxEmptySearchSurface(g.left).hits, 0, g.firstRow+1)...)
	}
	extra := m.inboxSelectionExtra(g.body - (g.firstRow - 1))
	for i := g.start; i < min(len(m.agentbox.rows), g.start+g.rows); i++ {
		row := m.agentbox.rows[i]
		height := 1
		if row.Target.Key() == m.agentbox.selected {
			height += extra
		}
		if !m.inboxSourceLoading(row.Target.ProfileID) {
			hits = append(hits, hitZone{x: 0, y: y, width: g.left, height: height, action: "inbox-row", target: row.Target})
		}
		y += height
	}
	if row, ok := m.selectedInbox(); ok && !m.inboxSourceLoading(row.Target.ProfileID) {
		hits = append(hits, moveHits(m.inboxDecisionLayout(g.right, g.body).hits, g.left+2, 1)...)
	}
	controlsY := 1 + g.body
	if m.operationLine() != "" {
		if m.hasSavedActions() {
			hits = append(hits, hitZone{x: 0, y: controlsY, width: m.width, height: 1, action: "inbox-recover"})
		}
		controlsY++
	}
	if m.feedbackVisible() || m.agentbox.error != "" {
		controlsY++
	}
	hits = append(hits, moveHits(m.inboxComposerControls().hits, 0, controlsY)...)
	for i := range hits {
		hits[i].serial = m.agentbox.serial
	}
	return clipHits(hits, 0, 0, m.width, m.height)
}

func (m *Model) inboxComposerControls() renderedSurface {
	if !m.agentbox.reply {
		return renderedSurface{}
	}
	label := strings.ToUpper(m.selectedDelivery()[:1]) + m.selectedDelivery()[1:] + " ▾"
	controls := controlSurface(m.width, []surfaceControl{
		{label: "Send", action: "inbox-compose", args: "send"},
		{label: label, action: "inbox-compose", args: "delivery-menu"},
	})
	for i := range controls.hits {
		controls.hits[i].owner = m.status.ProfileID + "\x00" + m.status.ThreadID + "\x00" + m.selectedDelivery()
	}
	return controls
}

func (m *Model) inboxSelectionExtra(height int) int {
	if height >= 8 && m.agentbox.selected != "" {
		return 3
	}
	if height >= 7 && m.agentbox.selected != "" {
		return 2
	}
	return 0
}
func (m *Model) inboxMachineKind(profile string) string {
	for _, source := range m.agentbox.page.Sources {
		if source.ProfileID == profile {
			return source.Page.MachineKind
		}
	}
	return "unknown"
}
