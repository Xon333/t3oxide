package ui

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/charmbracelet/x/ansi"
)

type navigatorPreviewMsg struct {
	generation, serial, pageSerial uint64
	target                         string
	preview                        client.ThreadPreview
	err                            error
}

func machineMark(kind string) string {
	switch kind {
	case "server", "linux":
		return "▤"
	case "cloud":
		return "☁"
	case "desktop", "mac-mini", "mac-studio":
		return "▣"
	case "laptop":
		return "▱"
	default:
		return "◇"
	}
}

func projectBubble(project, kind string) string {
	var letters []rune
	for _, r := range safeText(project) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			letters = append(letters, unicode.ToUpper(r))
			if len(letters) == 2 {
				break
			}
		}
	}
	label := string(letters)
	if label == "" {
		label = "??"
	} else if len(letters) == 1 {
		label += "·"
	}
	return "[" + label + machineMark(kind) + "]"
}

func nativeShelf(row client.Thread) string {
	if row.Inbox == nil {
		return "Conversations"
	}
	if row.Inbox.PinnedAt != "" {
		return "Pinned"
	}
	if row.Inbox.Working != nil && *row.Inbox.Working {
		return "Working"
	}
	if row.Inbox.PendingRequestCount > 0 || row.Inbox.PendingChildRequest || !row.Inbox.Settled && row.Inbox.SnoozedUntil == "" {
		return "Needs you"
	}
	return "Other"
}
func shelfRank(shelf string) int {
	switch shelf {
	case "Pinned":
		return 0
	case "Working":
		return 1
	case "Needs you":
		return 2
	case "Conversations":
		return 3
	default:
		return 4
	}
}

func (m *Model) freezeNavigatorRows(canonical []navigatorItem) []navigatorItem {
	n := &m.navigator
	if !n.focused || n.reorderNow || len(n.ordered) == 0 {
		n.reorderNow, n.orderDeferred = false, false
		return canonical
	}
	fresh := make(map[string]navigatorItem, len(canonical))
	for _, row := range canonical {
		fresh[row.identity()] = row
	}
	out := make([]navigatorItem, 0, len(n.ordered)+len(canonical))
	nativeSeen := make(map[string]bool)
	for _, old := range n.ordered {
		row, ok := fresh[old.identity()]
		if ok {
			row.shelf = old.shelf
			delete(fresh, old.identity())
		} else {
			// Keep the drawn slot until leaving/Refresh. It is metadata only and
			// cannot acquire a current offered target after removal from its page.
			row = old
			row.stale = true
		}
		out = append(out, row)
		nativeSeen[row.nativeIdentity()] = true
	}
	// Existing hit positions are a stable prefix. A new result for an earlier
	// shelf is appended as a continued section until canonical order is applied.
	// Retain at most two bounded 200-row pages per known source while focused.
	limit := max(len(n.ordered), 400*max(1, len(n.pages)+len(n.pending)))
	for _, row := range canonical {
		if _, ok := fresh[row.identity()]; ok && !nativeSeen[row.nativeIdentity()] && len(out) < limit {
			out = append(out, row)
			nativeSeen[row.nativeIdentity()] = true
		}
	}
	n.orderDeferred = len(out) != len(canonical)
	if !n.orderDeferred {
		for i := range out {
			if out[i].identity() != canonical[i].identity() || out[i].shelf != canonical[i].shelf || out[i].stale {
				n.orderDeferred = true
				break
			}
		}
	}
	return out
}

func (m *Model) applyNavigatorOrder() {
	if len(m.navigator.pages) == 0 {
		return
	}
	m.navigator.reorderNow = true
	m.rebuildUnifiedNavigator()
	m.layoutDirty = true
}

func (m *Model) toggleNavigatorShelf(shelf string) tea.Cmd {
	n := &m.navigator
	if n.collapsed == nil {
		n.collapsed = map[string]bool{}
	}
	n.collapsed[shelf] = !n.collapsed[shelf]
	m.rebuildUnifiedNavigator()
	m.layoutDirty = true
	m.resizeNavigator()
	return m.restoreFocus()
}

func (m *Model) creationDestinationLabel() string {
	if m.session != nil {
		pin, policy := m.session.CreationDestinationHint()
		if pin.ProfileID != "" {
			label := "→ " + safeText(pin.ProfileID)
			if pin.ProjectID != "" {
				label += " / " + safeText(pin.ProjectID)
			}
			if strings.HasPrefix(policy, "explicit") {
				label += " (CLI)"
			}
			if strings.HasSuffix(policy, "unavailable") {
				label += " · unavailable"
			}
			return label
		}
		if policy == "conflicting-defaults" {
			return "→ choose · conflicting defaults"
		}
	}
	return "→ native default"
}

func (m *Model) navigatorRowsTop() int {
	if m.status.ThreadID != "" && m.navigatorWidth() >= m.width {
		return 5
	}
	return 4
}

func (m *Model) compactNavigatorOperations(width int) renderedSurface {
	if m.navigatorRowsTop() == 4 {
		return renderedSurface{}
	}
	var controls []surfaceControl
	if m.status.Requests > 0 {
		controls = append(controls, surfaceControl{label: fmt.Sprintf("Requests (%d)", m.status.Requests), action: "toolbar", args: "requests"})
	}
	if m.status.ActiveRunID != "" {
		controls = append(controls, surfaceControl{label: "Stop", action: "toolbar", args: "stop"})
	}
	result := controlSurface(width, controls)
	connection := m.status.ConnectionState
	if connection == "" {
		connection = "connecting"
	}
	badge := threadStatusBadge(client.Thread{Status: m.status.RunState})
	status := badge.label + " · " + connection
	budget := max(0, width-ansi.StringWidth(result.content)-2)
	result.content = edgeLine(result.content, colors(m.theme).subtle().Render(fitLine(safeText(status), budget)), width)
	return result
}

// The same immutable result supplies the draw and the hit geometry. Expansion
// is local to the selected row; no helper, subscription or attachment runs.
func (m *Model) navigatorRowsLayout(width, height int) renderedSurface {
	n := &m.navigator
	p := colors(m.navigatorTheme())
	items := n.list.VisibleItems()
	ordered := n.ordered
	if len(ordered) == 0 {
		for _, raw := range items {
			if row, ok := raw.(navigatorItem); ok {
				if row.shelf == "" {
					row.shelf = nativeShelf(row.thread)
				}
				ordered = append(ordered, row)
			}
		}
	}
	counts, requests := map[string]int{}, map[string]int{}
	visible := make(map[string]int, len(items))
	for index, raw := range items {
		if row, ok := raw.(navigatorItem); ok {
			visible[row.identity()] = index
		}
	}
	for _, item := range ordered {
		counts[item.shelf]++
		if !item.stale && item.thread.Inbox != nil {
			requests[item.shelf] += item.thread.Inbox.PendingRequestCount
			if item.thread.Inbox.PendingChildRequest {
				requests[item.shelf]++
			}
		}
	}
	var lines []string
	var hits []hitZone
	selectedStart, selectedEnd := 0, 0
	seenShelf := map[string]bool{}
	lastShelf := ""
	for _, item := range ordered {
		shelf := item.shelf
		if shelf == "" {
			shelf = nativeShelf(item.thread)
		}
		if shelf != lastShelf {
			marker := "▾ "
			if n.collapsed[shelf] {
				marker = "▸ "
			}
			label := fmt.Sprintf("%s%s (%d)", marker, shelf, counts[shelf])
			if seenShelf[shelf] {
				label += " · continued"
			}
			if requests[shelf] > 0 {
				label += fmt.Sprintf(" · ?%d", requests[shelf])
			}
			hits = append(hits, hitZone{x: 0, y: len(lines), width: width, height: 1, action: "navigator-shelf", args: shelf, serial: n.serial})
			lines = append(lines, p.subtle().Bold(true).Render(fitLine(label, width)))
			seenShelf[shelf], lastShelf = true, shelf
		}
		index, shown := visible[item.identity()]
		if n.collapsed[shelf] || !shown {
			continue
		}
		start := len(lines)
		var line strings.Builder
		attached := m.status.ProfileID + "\x00" + m.status.ThreadID
		navigatorDelegate{theme: m.navigatorTheme(), focused: n.focused, attached: attached}.Render(&line, n.list, index, item)
		lines = append(lines, line.String())
		if index == n.list.Index() && n.focused {
			selectedStart = start
			if height >= 6 {
				// A focused title has a fixed two-line allocation. A metadata refresh
				// can update text without moving any following drawn target.
				for _, part := range expandedTitleLines(item.thread.Title, max(1, width-2)) {
					lines = append(lines, p.selection().Render(fitLine("  "+part, width)))
				}
				context := item.thread.Project + " · " + item.label + " / " + item.profile
				if item.thread.ProviderInstanceID != "" {
					context += " · " + item.thread.ProviderInstanceID
				}
				if item.stale {
					context = "No longer in returned page · Refresh"
				}
				lines = append(lines, p.subtle().Render(fitLine("  "+safeText(context), width)))
			}
			selectedEnd = len(lines)
		}
		hits = append(hits, hitZone{x: 0, y: start, width: width, height: len(lines) - start, action: "navigator-row", args: item.identity(), serial: n.serial})
		if !item.stale && item.thread.ChildCount > 0 {
			hits = append(hits, hitZone{x: 0, y: start, width: 2, height: 1, action: "navigator-toggle", args: item.identity(), serial: n.serial})
		}
	}
	offset := max(0, selectedEnd-height)
	if selectedStart < offset {
		offset = selectedStart
	}
	if len(lines) > height {
		lines = lines[offset:min(len(lines), offset+height)]
	}
	hits = clipHits(moveHits(hits, 0, -offset), 0, 0, width, height)
	return renderedSurface{content: padBlock(strings.Join(lines, "\n"), width, height), hits: hits}
}

// expandedTitleLines preserves both ends of a long title in a stable two-line
// region. Cell-aware truncation never splits a grapheme; Info retains all text.
func expandedTitleLines(title string, width int) [2]string {
	width = max(1, width)
	title = strings.Join(strings.Fields(safeText(title)), " ")
	wrapped := strings.Split(ansi.Wrap(title, width, ""), "\n")
	result := [2]string{fitLine(wrapped[0], width), ""}
	if len(wrapped) == 2 {
		result[1] = fitLine(wrapped[1], width)
	} else if len(wrapped) > 2 {
		result[1] = fitLine(ansi.TruncateLeft(title, max(0, ansi.StringWidth(title)-width+1), "…"), width)
	}
	return result
}

func (m *Model) cancelNavigatorPreview() {
	n := &m.navigator
	if n.previewCancel != nil {
		n.previewCancel()
		n.previewCancel = nil
	}
	n.previewSerial++
	n.previewLoading = false
	n.preview = nil
	n.previewTarget = ""
}

func (m *Model) previewNavigator() tea.Cmd {
	page, row, ok := m.navigatorSource()
	if !ok || m.session == nil {
		return nil
	}
	m.cancelNavigatorPreview()
	n := &m.navigator
	ctx, cancel := context.WithCancel(m.sessionCtx)
	n.previewCancel = cancel
	n.previewLoading = true
	n.previewTarget = page.ProfileID + "\x00" + row.ID
	serial, generation, pageSerial, session, target := n.previewSerial, m.generation, n.serial, m.session, n.previewTarget
	m.notice = "Loading bounded preview · attached conversation unchanged"
	return func() tea.Msg {
		defer cancel()
		preview, err := session.NavigatorPreview(ctx, page, row.ID)
		return navigatorPreviewMsg{generation, serial, pageSerial, target, preview, err}
	}
}

func (m *Model) navigatorPreviewFinished(msg navigatorPreviewMsg) tea.Cmd {
	n := &m.navigator
	selected, ok := n.list.SelectedItem().(navigatorItem)
	if msg.generation != m.generation || msg.serial != n.previewSerial || msg.pageSerial != n.serial || msg.target != n.previewTarget || !ok || selected.identity() != msg.target {
		return nil
	}
	n.previewCancel = nil
	n.previewLoading = false
	if m.dialog != nil || m.detail != "" || !n.focused {
		m.cancelNavigatorPreview()
		return nil
	}
	if msg.err != nil {
		m.notice = "Preview unavailable · " + safeText(msg.err.Error())
		return nil
	}
	page, exists := n.pages[selected.profile]
	if !exists {
		page = n.page
	}
	if msg.preview.ProfileID != selected.profile || msg.preview.EnvironmentID != page.EnvironmentID || msg.preview.ThreadID != selected.thread.ID {
		m.cancelNavigatorPreview()
		m.notice = "Preview identity changed · request it again"
		return nil
	}
	n.preview = &msg.preview
	lines := []string{"Read-only preview · attachment and Stop target unchanged", "Environment: " + msg.preview.EnvironmentID, "Conversation: " + selected.thread.Title, "Observed: " + msg.preview.ObservedAt}
	if !msg.preview.Available || msg.preview.Snapshot == nil {
		lines = append(lines, "Metadata only · "+msg.preview.Unavailable)
	} else {
		for _, item := range msg.preview.Snapshot.Items {
			lines = append(lines, "", item.Kind+" · "+item.SourceThreadID, safeText(item.Text))
			if item.Truncated {
				lines = append(lines, "[Bounded native row]")
			}
		}
	}
	m.openDetail("Preview", strings.Join(lines, "\n"))
	return nil
}
