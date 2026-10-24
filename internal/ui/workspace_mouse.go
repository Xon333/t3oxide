package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/charmbracelet/x/ansi"
)

// Hit targets come from the rendered layout, following t3term's drawn geometry
// discipline. A resize invalidates them; row actions carry the displayed
// identity rather than a numeric index that can change after a read.
type hitZone struct {
	x, y, width, height int
	action, args        string
	serial              uint64
}

func (z hitZone) contains(x, y int) bool {
	return x >= z.x && x < z.x+z.width && y >= z.y && y < z.y+z.height
}

func (m *Model) addHit(x, y, w, h int, action, args string, serial uint64) {
	if w > 0 && h > 0 {
		m.hitZones = append(m.hitZones, hitZone{x: x, y: y, width: w, height: h, action: action, args: args, serial: serial})
	}
}

func (m *Model) recordWorkspaceHits() {
	m.hitZones = nil
	m.hitGeneration = m.generation
	m.layoutDirty = false
	if m.dialog != nil || m.detail != "" {
		m.recordModalHits()
		return
	}
	x := m.mainOriginX()
	inner := m.workspaceWidth()
	m.addHit(x, m.transcriptTop(), m.contentWidth, m.bodyHeight, "transcript", "", 0)
	stripY := m.transcriptTop() + m.bodyHeight
	if item := m.recentActivity(); item != nil {
		m.addHit(x, stripY, inner, 1, "activity", item.Key, 0)
	}
	composerY := m.transcriptTop() + m.bodyHeight + m.activityStripRows()
	if m.feedbackVisible() {
		composerY++
	}
	m.addHit(x, composerY, m.contentWidth, m.composer.Height()+2, "composer", "", 0)
	buttonX := x
	for _, a := range m.toolbarActions() {
		w := ansi.StringWidth(a.label)
		if buttonX-x+w > inner {
			break
		}
		m.addHit(buttonX, 2, w, 1, "toolbar", a.action, 0)
		buttonX += w + 2
	}
	// Model text itself opens the same staged settings review as Settings.
	m.addHit(x, 1, max(1, inner/2), 1, "toolbar", "settings", 0)
	if m.navigationVisible() {
		w := m.navigatorWidth() - 2
		serial := m.navigator.serial
		m.addHit(0, 1, w, 1, "navigator", "environment", serial)
		m.addHit(0, 2, 3, 1, "navigator", "new", serial)
		m.addHit(5, 2, 7, 1, "navigator", "actions", serial)
		m.addHit(0, 3, w, 1, "navigator-focus", "", serial)
		start, end := m.navigator.list.Paginator.GetSliceBounds(len(m.navigator.list.VisibleItems()))
		for index := start; index < end; index++ {
			i, ok := m.navigator.list.VisibleItems()[index].(navigatorItem)
			if !ok {
				continue
			}
			y := 4 + index - start
			if y >= m.height-5 {
				break
			}
			m.addHit(0, y, w, 1, "navigator-row", i.thread.ID, serial)
			if i.thread.ChildCount > 0 {
				m.addHit(0, y, 2, 1, "navigator-toggle", i.thread.ID, serial)
			}
		}
		row := m.height - 3
		left := 0
		if m.navigator.page.Page.Cursor > 0 {
			m.addHit(left, row, 4, 1, "navigator", "previous", serial)
			left += 6
		}
		m.addHit(left, row, 7, 1, "navigator", "refresh", serial)
		left += 9
		if m.navigator.page.Page.NextCursor != nil {
			m.addHit(left, row, 4, 1, "navigator", "next", serial)
		}
	}
	if m.contextVisible() {
		cx := x + m.contentWidth + 2
		m.addHit(cx, m.transcriptTop(), 28, m.bodyHeight, "context-focus", "", 0)
		if m.snapshot != nil && m.snapshot.Metadata.Activity != nil {
			y := m.transcriptTop() + 1
			for _, value := range m.contextFacts() {
				if value != "" {
					y++
				}
			}
			y += 2
			for _, item := range m.snapshot.Metadata.Activity.Items {
				if y >= m.transcriptTop()+m.bodyHeight-1 {
					break
				}
				m.addHit(cx, y, 28, 1, "activity", item.Key, 0)
				y++
			}
		}
	}
}

func (m *Model) recordModalHits() {
	if m.detail != "" || m.dialog == nil {
		return
	}
	d := m.dialog
	// Existing form controllers own their precise keyboard navigation. Mouse
	// never falls through a form to activate the hidden attachment.
	if d.kind == client.EnterText || d.details || d.settingsForm != nil || d.requestForm != nil || d.launch != nil {
		return
	}
	panel := m.modalView()
	x, y := max(0, (m.width-lipgloss.Width(panel))/2), max(0, (m.height-lipgloss.Height(panel))/2)
	if m.browserPage || m.launchPage {
		x, y = 0, 2
	}
	listY := y + 2 + m.questionViewport.Height()
	if d.surface == client.SurfaceBrowser {
		listY += m.browserControlsHeight(m.modalWidth)
	}
	start, end := d.list.Paginator.GetSliceBounds(len(d.list.VisibleItems()))
	for index := start; index < end; index++ {
		item, ok := d.list.VisibleItems()[index].(choiceItem)
		if !ok {
			continue
		}
		identity := item.id
		if identity == "" {
			identity = item.command + "\x00" + item.args
		}
		m.addHit(x+2, listY+index-start, m.modalWidth, 1, "dialog-row", identity, d.serial)
	}
}

func (m *Model) mouseClick(msg tea.MouseClickMsg) tea.Cmd {
	if !m.mouse || msg.Button != tea.MouseLeft || m.layoutDirty || m.hitGeneration != m.generation {
		return nil
	}
	for i := len(m.hitZones) - 1; i >= 0; i-- {
		z := m.hitZones[i]
		if !z.contains(msg.X, msg.Y) {
			continue
		}
		if m.dialog != nil || m.detail != "" {
			if z.action != "dialog-row" || m.dialog == nil || m.dialog.serial != z.serial {
				return nil
			}
			for j, raw := range m.dialog.list.VisibleItems() {
				item := raw.(choiceItem)
				id := item.id
				if id == "" {
					id = item.command + "\x00" + item.args
				}
				if id == z.args {
					m.dialog.list.Select(j)
					return m.chooseDialog()
				}
			}
			return nil
		}
		switch z.action {
		case "toolbar":
			return m.toolbarAction(z.args)
		case "transcript":
			m.activityFocused = false
			m.navigator.focused = false
			m.contextFocused = false
			m.transcriptFocus = true
			m.resizeNavigator()
			return m.restoreFocus()
		case "composer":
			m.activityFocused = false
			m.navigator.focused = false
			m.contextFocused = false
			m.transcriptFocus = false
			m.resizeNavigator()
			return m.restoreFocus()
		case "context-focus":
			m.activityFocused = false
			m.navigator.focused = false
			m.transcriptFocus = false
			m.contextFocused = true
			m.resizeNavigator()
			return m.restoreFocus()
		case "activity":
			return m.execute("activity", z.args)
		case "navigator-focus":
			if z.serial == m.navigator.serial {
				return m.focusNavigator()
			}
		case "navigator":
			if z.serial == m.navigator.serial {
				return m.navigatorAction(z.args)
			}
		case "navigator-row", "navigator-toggle":
			if z.serial != m.navigator.serial || m.navigator.loading {
				return nil
			}
			for j, raw := range m.navigator.list.VisibleItems() {
				if raw.(navigatorItem).thread.ID == z.args {
					m.navigator.list.Select(j)
					m.navigator.selected = z.args
					m.navigator.focused = true
					m.resizeNavigator()
					if z.action == "navigator-toggle" {
						return m.navigatorAction("toggle")
					}
					return m.navigatorAction("open")
				}
			}
		}
		return nil
	}
	return nil
}

func (m *Model) workspaceWheel(msg tea.MouseWheelMsg) (bool, tea.Cmd) {
	if !m.mouse {
		return true, nil
	}
	if m.dialog != nil || m.detail != "" {
		return false, nil
	}
	if m.layoutDirty {
		return true, nil
	}
	if m.navigationVisible() && msg.X < m.navigatorWidth() {
		if msg.Button == tea.MouseWheelUp {
			m.navigator.list.CursorUp()
		} else if msg.Button == tea.MouseWheelDown {
			m.navigator.list.CursorDown()
		}
		return true, nil
	}
	if m.contextVisible() && msg.X >= m.mainOriginX()+m.contentWidth+2 {
		if msg.Button == tea.MouseWheelUp {
			m.contextSelection = max(0, m.contextSelection-1)
		} else if msg.Button == tea.MouseWheelDown && m.snapshot != nil && m.snapshot.Metadata.Activity != nil {
			m.contextSelection = min(max(0, len(m.snapshot.Metadata.Activity.Items)-1), m.contextSelection+1)
		}
		return true, nil
	}
	return false, nil
}

// Keep path-bearing facts readable without using their bytes as controls.
func compactContextPath(path string, width int) string {
	if ansi.StringWidth(path) <= width {
		return path
	}
	parts := strings.Split(path, "/")
	return "…/" + fitLine(parts[len(parts)-1], max(1, width-2))
}
