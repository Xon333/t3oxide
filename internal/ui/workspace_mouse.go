package ui

import (
	"slices"
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
	owner               string
	target              client.InboxTarget
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
	mainVisible := !(m.session != nil && m.status.ThreadID == "") && m.navigatorWidth() < m.width
	if mainVisible {
		m.addHit(x, m.transcriptTop(), m.contentWidth, m.bodyHeight, "transcript", "", 0)
		stripY := m.transcriptTop() + m.bodyHeight + 1
		if item := m.recentActivity(); item != nil {
			m.addHit(x, stripY, inner, 1, "activity", item.Key, 0)
		}
		composerY := m.transcriptTop() + m.bodyHeight + m.activityStripRows()
		if m.feedbackVisible() {
			composerY++
		}
		m.addHit(x, composerY, m.contentWidth, m.composer.Height()+2, "composer", "", 0)
		buttonX := x
		for _, a := range m.visibleToolbarActions(inner) {
			w := ansi.StringWidth(a.label)
			if buttonX-x+w > inner {
				break
			}
			m.addHit(buttonX, 2, w, 1, "toolbar", a.action, 0)
			buttonX += w + 2
		}
		// Model text itself opens the same staged settings review as Settings.
		m.addHit(x, 1, max(1, inner/2), 1, "toolbar", "settings", 0)
	}
	if m.navigationVisible() {
		w := m.navigatorWidth() - 2
		serial := m.navigator.serial
		m.addHit(0, 1, w, 1, "navigator", "environment", serial)
		m.addHit(0, 2, 3, 1, "navigator", "new", serial)
		m.addHit(5, 2, 4, 1, "navigator", "actions", serial)
		m.addHit(11, 2, max(1, w-11), 1, "navigator", "destination", serial)
		m.addHit(0, 3, w, 1, "navigator-focus", "", serial)
		m.hitZones = append(m.hitZones, moveHits(m.compactNavigatorOperations(w).hits, 0, 4)...)
		m.hitZones = append(m.hitZones, moveHits(m.navigator.surface.hits, 0, m.navigatorRowsTop())...)
		if len(m.navigator.list.VisibleItems()) == 0 && m.navigator.list.FilterInput.Value() != "" {
			m.addHit(0, m.navigatorRowsTop()+1, w, 1, "navigator", "search", serial)
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
		if m.hasSavedActions() {
			m.addHit(0, m.height-1, min(w, len("Review saved action")), 1, "navigator-recover", "", 0)
		}
	}
	if mainVisible && m.contextVisible() {
		cx := x + m.contentWidth + 2
		m.addHit(cx, m.transcriptTop(), 28, m.bodyHeight, "context-focus", "", 0)
		if m.snapshot != nil && m.snapshot.Metadata.Activity != nil {
			_, first, offset := m.contextPanel(28, m.bodyHeight)
			y := m.transcriptTop() + first - offset
			for _, item := range m.snapshot.Metadata.Activity.Items {
				if y >= m.transcriptTop()+m.bodyHeight {
					break
				}
				if y > m.transcriptTop() {
					m.addHit(cx, y, 28, 1, "activity", item.Key, 0)
				}
				y++
			}
		}
	}
}

func (m *Model) recordModalHits() {
	// View has just captured this frame. Revalidate authority only when an
	// input event tries to use it, without rendering or hashing it twice.
	m.hitZones = m.drawnModal.surface.hits
}

func (m *Model) modalHitZones() []hitZone {
	f := m.drawnModal
	if m.dialog == nil || m.detail != "" || m.dialog.details || f.authority == "" || f.generation != m.generation || f.width != m.width || f.height != m.height || f.authority != m.modalAuthority() {
		return nil
	}
	return f.surface.hits
}

func modalPromptLive(d *dialog) bool {
	if d == nil {
		return false
	}
	if d.prompt != nil {
		select {
		case <-d.prompt.Done:
			return false
		default:
		}
	}
	return d.formSource == nil || modalPromptLive(d.formSource)
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
			if m.dialog == nil || m.dialog.serial != z.serial || !modalPromptLive(m.dialog) || !slices.Contains(m.modalHitZones(), z) {
				return nil
			}
			m.layoutDirty = true
			return m.activateModalHit(z)
		}
		switch z.action {
		case "navigator-recover":
			return m.execute("recover", "")
		case "toolbar":
			return m.toolbarAction(z.args)
		case "transcript":
			m.activityFocused = false
			m.navigator.focused = false
			m.contextFocused = false
			m.transcriptFocus = true
			return m.resizeWorkspaceFocus()
		case "composer":
			m.activityFocused = false
			m.navigator.focused = false
			m.contextFocused = false
			m.transcriptFocus = false
			return m.resizeWorkspaceFocus()
		case "context-focus":
			m.activityFocused = false
			m.navigator.focused = false
			m.transcriptFocus = false
			m.contextFocused = true
			m.reviewContextSelection()
			return m.resizeWorkspaceFocus()
		case "activity":
			return m.execute("activity", z.args)
		case "navigator-focus":
			if z.serial == m.navigator.serial {
				m.navigator.editing = true
				return m.focusNavigator()
			}
		case "navigator-shelf":
			if z.serial == m.navigator.serial {
				return m.toggleNavigatorShelf(z.args)
			}
		case "navigator":
			if z.serial == m.navigator.serial {
				return m.navigatorAction(z.args)
			}
		case "navigator-row", "navigator-toggle":
			if z.serial != m.navigator.serial {
				return nil
			}
			for j, raw := range m.navigator.list.VisibleItems() {
				if raw.(navigatorItem).identity() == z.args {
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

// Route controls directly to their existing controller after validating the
// displayed identity/geometry. In particular, pointer input never synthesizes
// a global Send key or changes editor focus before saving a request answer.
func (m *Model) activateModalHit(z hitZone) tea.Cmd {
	d := m.dialog
	defer m.resizeDialog()
	switch z.action {
	case "defaults-body":
		d.actionFocus = 0
		return m.restoreFocus()
	case "defaults-action":
		return m.nativeDefaultsAction(z.args)
	case "dialog-row":
		for i, raw := range d.list.VisibleItems() {
			item, ok := raw.(choiceItem)
			if !ok || item.disabled {
				continue
			}
			id := item.id
			if id == "" {
				id = item.command + "\x00" + item.args
			}
			if id == z.args {
				d.list.Select(i)
				return m.chooseDialog()
			}
		}
	case "dialog-finish":
		if d.kind == client.ChooseMany {
			return m.finishMultiple()
		}
	case "launch-field":
		if d.launch != nil {
			d.launch.focus = 0
			if z.args == "title" {
				d.launch.focus = 1
			}
			return m.restoreFocus()
		}
	case "launch-action":
		if d.launch != nil {
			if index := slices.Index(m.launchActions(), z.args); index >= 0 {
				d.launch.focus = index + 2
				if z.args == "editor" {
					return m.editLaunch()
				}
				return m.launchAction(z.args)
			}
		}
	case "settings-field":
		if f := d.settingsForm; f != nil {
			if z.args == "model" {
				f.focus = 0
				return m.openModelSettingChooser()
			}
			if model := f.model(); model != nil {
				for i, option := range model.Options {
					if option.ID == z.args {
						f.focus = i + 1
						return m.openModelSettingChooser()
					}
				}
			}
		}
	case "settings-action":
		if d.settingsForm != nil {
			if z.args == "apply" {
				return m.applyModelSettings()
			}
			return m.cancelForm()
		}
	case "request-option", "request-field", "request-action":
		f := d.requestForm
		if f == nil || f.questions[f.index].ID != z.owner {
			return nil
		}
		m.attention.armed = true
		if z.action == "request-action" {
			switch z.args {
			case "back":
				return m.advanceRequest(true)
			case "next":
				return m.advanceRequest(false)
			case "cancel":
				return m.cancelForm()
			}
		}
		if !m.saveRequestAnswer() {
			return nil
		}
		if z.action == "request-field" {
			f.focus = 1
			return m.restoreFocus()
		}
		for i, option := range f.questions[f.index].Options {
			if option.Value == z.args {
				f.focus, f.option = 0, i
				m.toggleRequestOption()
				m.refreshDialogDetails()
				return m.restoreFocus()
			}
		}
	case "text-field":
		d.actionFocus = 0
		return m.restoreFocus()
	case "text-action":
		if z.args == "Submit" {
			return m.chooseDialog()
		}
		return m.cancelDialog()
	}
	return nil
}

func (m *Model) workspaceWheel(msg tea.MouseWheelMsg) (bool, tea.Cmd) {
	if !m.mouse {
		return true, nil
	}
	if m.dialog != nil && m.detail == "" && !m.dialog.details && m.dialog.surface == client.SurfaceNativeDefaults {
		return true, m.nativeDefaultsWheel(msg)
	}
	if m.dialog != nil && m.detail == "" && !m.dialog.details {
		if handled, cmd := m.formWheel(msg); handled {
			return true, cmd
		}
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
		m.layoutDirty = true
		return true, nil
	}
	if m.contextVisible() && msg.X >= m.mainOriginX()+m.contentWidth+2 {
		m.layoutDirty = true
		if msg.Button == tea.MouseWheelUp {
			m.contextSelection = max(0, m.contextSelection-1)
		} else if msg.Button == tea.MouseWheelDown && m.snapshot != nil && m.snapshot.Metadata.Activity != nil {
			m.contextSelection = min(max(0, len(m.snapshot.Metadata.Activity.Items)-1), m.contextSelection+1)
		}
		m.reviewContextSelection()
		return true, nil
	}
	return false, nil
}

// Scroll long forms through their existing focused rows. This exposes every
// field to the pointer without a second independent viewport position. A wheel
// never toggles an offered value or activates a submission control.
func (m *Model) formWheel(msg tea.MouseWheelMsg) (bool, tea.Cmd) {
	d := m.dialog
	if d == nil || d.launch == nil && d.settingsForm == nil && d.requestForm == nil {
		return false, nil
	}
	if m.layoutDirty || m.hitGeneration != m.generation || !modalPromptLive(d) {
		return true, nil
	}
	delta := 0
	if msg.Button == tea.MouseWheelUp {
		delta = -1
	} else if msg.Button == tea.MouseWheelDown {
		delta = 1
	}
	if delta == 0 {
		return true, nil
	}
	if m.drawnModal.authority == "" || m.drawnModal.authority != m.modalAuthority() {
		return true, nil
	}
	panel := m.drawnModal.surface
	x, y := max(0, (m.width-lipgloss.Width(panel.content))/2), max(0, (m.height-lipgloss.Height(panel.content))/2)
	bounds := hitZone{x: x, y: y, width: lipgloss.Width(panel.content), height: lipgloss.Height(panel.content)}
	if m.browserPage || m.launchPage {
		bounds = hitZone{x: 0, y: 2, width: m.workspaceWidth(), height: max(1, m.height-2-m.surfaceHelpHeight)}
	}
	if !bounds.contains(msg.X, msg.Y) {
		return true, nil
	}
	for _, hit := range m.modalHitZones() {
		if hit.contains(msg.X, msg.Y) && m.modalInput.Focused() && (hit.action == "launch-field" && hit.args == "message" || hit.action == "request-field") {
			return true, m.updateComponent("modal", d.serial, msg)
		}
	}
	if f := d.settingsForm; f != nil {
		f.focus = max(0, min(f.optionCount()+2, f.focus+delta))
	} else if f := d.launch; f != nil {
		f.focus = max(0, min(len(m.launchActions())+1, f.focus+delta))
	} else if f := d.requestForm; f != nil {
		q := f.questions[f.index]
		if len(q.Options) == 0 || !m.saveRequestAnswer() {
			return true, nil
		}
		if delta > 0 && f.option == len(q.Options)-1 && q.AllowCustomAnswer {
			f.focus = 1
		} else {
			if f.focus == 0 {
				f.option = max(0, min(len(q.Options)-1, f.option+delta))
			}
			f.focus = 0
		}
	}
	m.layoutDirty = true
	m.resizeDialog()
	return true, m.restoreFocus()
}

// Keep path-bearing facts readable without using their bytes as controls.
func compactContextPath(path string, width int) string {
	if ansi.StringWidth(path) <= width {
		return path
	}
	parts := strings.Split(path, "/")
	return "…/" + fitLine(parts[len(parts)-1], max(1, width-2))
}
