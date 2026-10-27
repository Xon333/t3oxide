package ui

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/charmbracelet/x/ansi"
)

// This rail adapts t3term's sidebar/focus composition to the existing bounded
// t3oxide metadata owner. It holds snapshots only: no all-thread watches, idle
// timer or hidden transcript reads. See NOTICE and docs/reuse-t3term.md.
type navigatorState struct {
	page                     client.NavigatorPage
	list                     list.Model
	loaded, loading, focused bool
	profile, error           string
	serial                   uint64
	cancel                   context.CancelFunc
	loadedAt                 time.Time
	selected                 string
	review                   *navigatorReview
}

// A local menu reviews a particular metadata page and target. A later list
// refresh must not turn an explicit action into an operation on another row.
type navigatorReview struct {
	serial, pageSerial, generation uint64
	page                           client.NavigatorPage
	thread                         client.Thread
	selected                       bool
}

type navigatorMsg struct {
	generation, serial uint64
	page               client.NavigatorPage
	err                error
}

type navigatorItem struct{ thread client.Thread }

func (i navigatorItem) Title() string       { return i.thread.Title }
func (i navigatorItem) Description() string { return i.thread.Status }
func (i navigatorItem) FilterValue() string {
	return i.thread.Title + " " + i.thread.Project + " " + i.thread.ID
}

type navigatorDelegate struct {
	theme    string
	focused  bool
	attached string
}

func (d navigatorDelegate) Height() int                         { return 1 }
func (d navigatorDelegate) Spacing() int                        { return 0 }
func (d navigatorDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }
func (d navigatorDelegate) Render(w io.Writer, model list.Model, index int, raw list.Item) {
	i, ok := raw.(navigatorItem)
	if !ok {
		return
	}
	p := colors(d.theme)
	row := i.thread
	prefix := "  "
	if row.Lineage.Type() == "unknown" {
		prefix = "? "
	}
	if row.ChildCount > 0 {
		prefix = "▸ "
		if row.Expanded {
			prefix = "▾ "
		}
	}
	if row.Lineage.Type() == "subagent" && row.Depth == 0 {
		prefix = "↳ "
	}
	if row.Depth > 0 {
		prefix = strings.Repeat(" ", min(row.Depth, 6)) + "↳ "
	}
	if row.ID == d.attached {
		prefix = "• " + strings.TrimLeft(prefix, " ")
	}
	status := navigatorStatus(row.Status)
	if row.Lineage.Type() == "fork" {
		status = "fork · " + status
	}
	left := fitLine(prefix+safeText(row.Title), max(1, model.Width()-ansi.StringWidth(status)-2))
	text := edgeLine(left, status, model.Width())
	style := p.plain()
	if index == model.Index() && d.focused {
		style = p.selection()
	}
	if row.ID == d.attached {
		style = style.Bold(true)
	}
	fmt.Fprint(w, style.Render(fitLine(text, model.Width())))
}

func navigatorStatus(value string) string {
	switch value {
	case "running", "active", "responding":
		return "running"
	case "waiting", "waiting_for_input", "awaiting_input", "waiting_for_approval":
		return "waiting"
	case "completed", "idle":
		return value
	case "failed", "error":
		return "failed"
	case "interrupted", "stopped":
		return "stopped"
	case "":
		return "unknown"
	default:
		return fitLine(safeText(value), 10)
	}
}

func (m *Model) navigationVisible() bool {
	return !m.navigationHidden && m.width >= 104 && !m.browserPage && !m.launchPage
}

func (m *Model) navigatorWidth() int {
	if !m.navigationVisible() {
		return 0
	}
	// Ported from t3term tui/mod.rs::sidebar_width with a larger minimum
	// conversation width appropriate to t3oxide's source-bound controls.
	return min(max(m.width/4, 26), 34)
}

func (m *Model) initNavigator() {
	d := navigatorDelegate{theme: m.theme}
	m.navigator.list = list.New(nil, d, 28, 16)
	m.navigator.list.SetShowTitle(false)
	m.navigator.list.SetShowFilter(false)
	m.navigator.list.SetShowStatusBar(false)
	m.navigator.list.SetShowPagination(false)
	m.navigator.list.SetShowHelp(false)
	m.navigator.list.DisableQuitKeybindings()
	m.navigator.list.FilterInput.Prompt = ""
	m.navigator.list.FilterInput.Placeholder = "Filter loaded"
	m.navigator.list.FilterInput.CharLimit = 256
	m.navigator.list.FilterInput.SetVirtualCursor(true)
	m.navigator.list.Filter = list.UnsortedFilter
}

func (m *Model) loadNavigator(profile, query string, force bool) tea.Cmd {
	if m.session == nil || !m.navigationVisible() || m.status.ThreadID == "" {
		return nil
	}
	if !force && !m.status.Connected {
		return nil
	}
	profiles := m.session.NavigatorProfiles()
	if profile == "" {
		profile = m.navigator.profile
	}
	if profile == "" {
		profile = m.status.ProfileID
	}
	if profile == "" && len(profiles) > 0 {
		profile = profiles[0].ID
	}
	if profile == "" {
		return nil
	}
	if !force && m.navigator.loaded && m.navigator.profile == profile {
		return nil
	}
	if m.navigator.loading && !force {
		return nil
	}
	if m.navigator.cancel != nil {
		m.navigator.cancel()
	}
	m.navigator.serial++
	serial, generation, session := m.navigator.serial, m.generation, m.session
	ctx, cancel := context.WithCancel(m.sessionCtx)
	m.navigator.cancel = cancel
	m.navigator.loading, m.navigator.profile, m.navigator.error = true, profile, ""
	return func() tea.Msg {
		defer cancel()
		page, err := session.NavigatorRead(ctx, profile, query)
		return navigatorMsg{generation: generation, serial: serial, page: page, err: err}
	}
}

func (m *Model) navigatorLoaded(msg navigatorMsg) tea.Cmd {
	if msg.generation != m.generation || msg.serial != m.navigator.serial {
		return nil
	}
	m.navigator.loading = false
	m.layoutDirty = true
	if msg.err != nil {
		m.navigator.error = safeText(msg.err.Error())
		return nil
	}
	identity := ""
	if m.navigator.loaded && m.navigator.page.ProfileID == msg.page.ProfileID {
		if item, ok := m.navigator.list.SelectedItem().(navigatorItem); ok {
			identity = item.thread.ID
		}
	}
	m.navigator.page, m.navigator.profile = msg.page, msg.page.ProfileID
	m.navigator.loaded, m.navigator.loadedAt = true, time.Now()
	items := make([]list.Item, 0, len(msg.page.Page.Threads))
	for _, row := range msg.page.Page.Threads {
		items = append(items, navigatorItem{thread: row})
	}
	m.navigator.list.SetItems(items)
	m.navigator.list.SetFilterText("")
	m.navigator.list.ResetFilter()
	m.navigator.list.FilterInput.SetValue(msg.page.Query)
	for i, row := range msg.page.Page.Threads {
		if row.ID == identity {
			m.navigator.list.Select(i)
			break
		}
	}
	m.resizeNavigator()
	return nil
}

func (m *Model) resizeNavigator() {
	w := max(1, m.navigatorWidth()-2)
	m.navigator.list.SetSize(w, max(1, m.height-9))
	m.navigator.list.FilterInput.SetWidth(w)
	p := colors(m.navigatorTheme())
	styles := textinput.DefaultStyles(true)
	styles.Focused.Text = p.plain()
	styles.Focused.Prompt = p.active()
	styles.Focused.Placeholder = p.subtle()
	styles.Blurred = styles.Focused
	styles.Cursor.Color = lipgloss.Color(p.accent)
	styles.Cursor.Blink = false
	m.navigator.list.FilterInput.SetStyles(styles)
	attached := ""
	if m.navigator.profile == m.status.ProfileID {
		attached = m.status.ThreadID
	}
	m.navigator.list.SetDelegate(navigatorDelegate{theme: m.navigatorTheme(), focused: m.navigator.focused, attached: attached})
}

func (m *Model) navigatorSelected() (client.Thread, bool) {
	i, ok := m.navigator.list.SelectedItem().(navigatorItem)
	return i.thread, ok
}

func (m *Model) navigatorAction(action string) tea.Cmd {
	if strings.HasPrefix(action, "review:") {
		return m.reviewedNavigatorAction(action)
	}
	if action == "details" {
		return m.openNavigatorDetail()
	}
	if m.session == nil {
		return nil
	}
	n := &m.navigator
	row, selected := m.navigatorSelected()
	switch action {
	case "open":
		if !selected || n.loading || n.error != "" || n.page.ProfileID != n.profile {
			return nil
		}
		return m.navigatorTarget(n.page, row.ID, action)
	case "new", "settings":
		if !selected || n.loading || n.error != "" || n.page.ProfileID != n.profile {
			return nil
		}
		return m.navigatorTarget(n.page, row.ID, action)
	case "refresh":
		return m.loadNavigator(n.profile, n.page.Query, true)
	case "environment":
		profiles := m.session.NavigatorProfiles()
		items := make([]list.Item, 0, len(profiles))
		for _, p := range profiles {
			items = append(items, choiceItem{id: p.ID, title: p.Label, command: "navigator-profile", args: p.ID})
		}
		m.openLocal("Environment", "navigator-environments", items)
		return nil
	case "actions":
		return m.openNavigatorActions()
	case "search":
		query := n.list.FilterInput.Value()
		if err := m.session.NavigatorChange(n.page, "search", query); err != nil {
			n.error = safeText(err.Error())
			return nil
		}
		return m.loadNavigator(n.profile, query, true)
	default:
		if !n.loaded || n.loading {
			return nil
		}
		if err := m.session.NavigatorChange(n.page, action, row.ID); err != nil {
			n.error = safeText(err.Error())
			return nil
		}
		return m.loadNavigator(n.profile, n.page.Query, true)
	}
}

func (m *Model) navigatorTarget(page client.NavigatorPage, thread, action string) tea.Cmd {
	if m.session == nil {
		return nil
	}
	session := m.session
	return m.navigatorRun(action, func(ctx context.Context) (client.Result, error) {
		if action == "open" {
			next, err := session.NavigatorOpen(page, thread)
			return client.Result{Next: next}, err
		}
		return session.NavigatorSource(ctx, page, thread, action)
	})
}

func (m *Model) reviewedNavigatorAction(token string) tea.Cmd {
	id, action, found := strings.Cut(strings.TrimPrefix(token, "review:"), ":")
	serial, err := strconv.ParseUint(id, 10, 64)
	r := m.navigator.review
	if !found || err != nil || r == nil || r.serial != serial || r.generation != m.generation || r.pageSerial != m.navigator.serial || r.page.ProfileID != m.navigator.profile || !m.navigator.loaded || m.navigator.loading || m.navigator.error != "" {
		m.notice = "Conversation list changed · open More again"
		return nil
	}
	m.navigator.review = nil
	switch action {
	case "open", "new", "settings":
		if !r.selected {
			m.notice = "Select a conversation first"
			return nil
		}
		return m.navigatorTarget(r.page, r.thread.ID, action)
	case "details":
		if r.selected {
			m.showNavigatorDetail(r.page, r.thread)
		}
		return nil
	case "toggle", "expand", "collapse":
		if m.session == nil || !r.selected {
			return nil
		}
		if err := m.session.NavigatorChange(r.page, action, r.thread.ID); err != nil {
			m.navigator.error = safeText(err.Error())
			return nil
		}
		return m.loadNavigator(r.page.ProfileID, r.page.Query, true)
	}
	// Navigation itself remains under its existing exact page/cursor owner.
	return m.navigatorAction(action)
}

func (m *Model) openNavigatorDetail() tea.Cmd {
	n := &m.navigator
	row, ok := m.navigatorSelected()
	if !ok || !n.loaded || n.loading || n.page.ProfileID != n.profile {
		m.notice = "Select a loaded conversation to inspect"
		return nil
	}
	m.showNavigatorDetail(n.page, row)
	return nil
}

func (m *Model) showNavigatorDetail(page client.NavigatorPage, row client.Thread) {
	lines := []string{"Browse: " + page.Label, "Conversation: " + row.Title, "Status: " + navigatorStatus(row.Status)}
	if page.ProfileID == m.status.ProfileID && row.ID == m.status.ThreadID {
		lines = append(lines, "Attached conversation")
	} else {
		lines = append(lines, "Metadata only · Enter in the list opens this conversation")
	}
	for _, field := range []struct{ name, value string }{{"Project", row.Project}, {"Model", row.Model}, {"Parent", row.ParentTitle}, {"Observed", page.Page.ObservedAt}, {"Profile ID", page.ProfileID}, {"Conversation ID", row.ID}, {"Project ID", row.ProjectID}} {
		if field.value != "" {
			lines = append(lines, field.name+": "+field.value)
		}
	}
	lines = append(lines, "Type: "+row.Lineage.Type())
	if row.ChildCount > 0 {
		lines = append(lines, fmt.Sprintf("Children: %d", row.ChildCount))
	}
	m.openDetail("Conversation details", strings.Join(lines, "\n"))
}

func (m *Model) navigatorRun(name string, run func(context.Context) (client.Result, error)) tea.Cmd {
	if name == "open" {
		if m.browserBusy {
			m.notice = "Opening conversation"
			return nil
		}
		m.browserID++
		id, generation := m.browserID, m.generation
		ctx, cancel := context.WithCancel(m.sessionCtx)
		m.cancelBrowser = cancel
		done := make(chan struct{})
		m.browserDone = done
		m.browserBusy = true
		return func() tea.Msg {
			defer close(done)
			defer cancel()
			result, err := run(ctx)
			return actionMsg{generation: generation, id: id, name: "switch", browser: true, result: result, err: err}
		}
	}
	if m.actionBusy {
		m.notice = "An action is pending; Stop remains available"
		return nil
	}
	m.actionID++
	id, generation := m.actionID, m.generation
	ctx, cancel := context.WithCancel(m.sessionCtx)
	m.cancelAction = cancel
	done := make(chan struct{})
	m.actionDone = done
	m.actionBusy = true
	m.actionName = name
	return func() tea.Msg {
		defer close(done)
		defer cancel()
		result, err := run(ctx)
		return actionMsg{generation: generation, id: id, name: name, result: result, err: err}
	}
}

func (m *Model) openNavigatorActions() tea.Cmd {
	row, selected := m.navigatorSelected()
	unavailable := ""
	switch {
	case !selected:
		unavailable = "Select a loaded conversation first"
	case m.navigator.loading:
		unavailable = "Wait for conversations to finish loading"
	case m.navigator.error != "":
		unavailable = "Refresh conversations before using this source"
	case !m.navigator.loaded || m.navigator.page.ProfileID != m.navigator.profile:
		unavailable = "Load this environment's conversation list first"
	}
	items := []list.Item{
		choiceItem{title: "Open", command: "navigator-action", args: "open"},
		choiceItem{title: "New in selected workspace", command: "navigator-action", args: "new"},
		choiceItem{title: "Selected conversation settings", command: "navigator-action", args: "settings"},
		choiceItem{title: "Conversation details", command: "navigator-action", args: "details"},
		choiceItem{title: "Search conversations", description: "Search titles and projects on this environment", command: "navigator-action", args: "search"},
		choiceItem{title: "Refresh", command: "navigator-action", args: "refresh"},
	}
	if selected && row.ChildCount > 0 {
		action, label := "expand", "Show working children"
		if row.Expanded {
			action, label = "collapse", "Hide children"
		}
		items = append(items, choiceItem{title: label, command: "navigator-action", args: action})
	}
	for _, scope := range []string{"active", "all", "completed", "failed"} {
		items = append(items, choiceItem{title: "Scope · " + scope, command: "navigator-action", args: scope})
	}
	if m.navigator.page.Tree {
		for _, kind := range []string{"all", "main", "subagents", "forks"} {
			items = append(items, choiceItem{title: "Type · " + kind, command: "navigator-action", args: "type:" + kind})
		}
	}
	if m.navigator.page.Page.NextCursor != nil {
		items = append(items, choiceItem{title: "Next page", command: "navigator-action", args: "next"})
	}
	if m.navigator.page.Page.Cursor > 0 {
		items = append(items, choiceItem{title: "Previous page", command: "navigator-action", args: "previous"})
	}
	m.openLocal("More · Conversations", "navigator-actions", items)
	m.navigator.review = &navigatorReview{serial: m.dialog.serial, pageSerial: m.navigator.serial, generation: m.generation, page: m.navigator.page, thread: row, selected: selected}
	for i, raw := range m.dialog.list.Items() {
		item := raw.(choiceItem)
		if unavailable != "" && (item.args == "open" || item.args == "new" || item.args == "settings" || item.args == "details") {
			item.disabled = true
			item.description = unavailable
		}
		item.args = fmt.Sprintf("review:%d:%s", m.dialog.serial, item.args)
		m.dialog.list.SetItem(i, item)
	}
	// SetItem leaves the native filtered projection unchanged until refiltered.
	// Publish the bound tokens and disabled reasons before the first activation.
	m.dialog.refilter(false)
	m.dialog.description = "Browse: " + m.navigator.page.Label
	if selected {
		m.dialog.description += "\nSelected: " + row.Title
	}
	m.refreshDialogDetails()
	m.resize()
	return nil
}

func (m *Model) focusNavigator() tea.Cmd {
	m.navigator.focused = true
	m.activityFocused = false
	m.contextFocused = false
	m.transcriptFocus = false
	m.searching = false
	m.resizeNavigator()
	cmd := m.loadNavigator("", "", false)
	return tea.Batch(m.restoreFocus(), cmd)
}

func (m *Model) navigatorKey(msg tea.KeyPressMsg) tea.Cmd {
	n := &m.navigator
	switch msg.String() {
	case "esc", "ctrl+o":
		n.focused = false
		m.resizeNavigator()
		return m.restoreFocus()
	case "tab", "shift+tab":
		return m.cycleWorkspace(msg.String() == "tab")
	case "enter":
		return m.navigatorAction("open")
	case "ctrl+f":
		return m.navigatorAction("search")
	case "ctrl+r":
		return m.execute("requests", "")
	case "f5":
		return m.navigatorAction("refresh")
	case "ctrl+e":
		return m.navigatorAction("environment")
	case "f6":
		return m.navigatorAction("actions")
	case "left":
		if n.list.FilterInput.Value() == "" {
			return m.navigatorAction("collapse")
		}
	case "right":
		if n.list.FilterInput.Value() == "" {
			return m.navigatorAction("expand")
		}
	}
	// Keep filtering synchronous, as the full Browser does, so a late match
	// computation cannot change a newly selected environment or target.
	if msg.String() == "up" {
		n.list.CursorUp()
		return nil
	}
	if msg.String() == "down" {
		n.list.CursorDown()
		return nil
	}
	if msg.String() == "pgup" {
		n.list.PrevPage()
		return nil
	}
	if msg.String() == "pgdown" {
		n.list.NextPage()
		return nil
	}
	if msg.String() == "home" && n.list.FilterInput.Value() == "" {
		n.list.Select(0)
		return nil
	}
	if msg.String() == "end" && n.list.FilterInput.Value() == "" {
		n.list.Select(len(n.list.VisibleItems()) - 1)
		return nil
	}
	var cmd tea.Cmd
	n.list.FilterInput, cmd = n.list.FilterInput.Update(msg)
	input := n.list.FilterInput
	query := n.list.FilterInput.Value()
	if query == "" {
		n.list.ResetFilter()
	} else {
		selected := n.list.Index()
		n.list.SetFilterText(query)
		n.list.SetFilterState(list.FilterApplied)
		n.list.Select(min(selected, max(0, len(n.list.VisibleItems())-1)))
	}
	n.list.FilterInput = input
	return m.componentCmd("navigator", n.serial, cmd)
}

func (m *Model) navigatorTheme() string {
	if m.settings == nil {
		return m.theme
	}
	return m.settings.ThemeFor(m.navigator.profile, m.status.ProfileIDs)
}

func (m *Model) navigatorView(width, height int) string {
	p := colors(m.navigatorTheme())
	n := &m.navigator
	w := max(1, width-2)
	label := n.page.Label
	if label == "" || n.page.ProfileID != n.profile {
		label = n.profile
	}
	if label == "" {
		label = "Environments"
	}
	heading := "Conversations"
	if n.focused {
		heading = "› " + heading
	}
	parts := []string{p.active().Render(heading), p.plain().Bold(true).Render(fitLine("Browse: "+label+" ▾", w)), p.plain().Render("New  More"), ansi.Truncate(n.list.FilterInput.View(), w, "")}
	rows := n.list.View()
	if n.loading {
		rows = p.subtle().Render("Loading…")
	} else if n.error != "" {
		rows = p.plain().Foreground(lipgloss.Color(p.error)).Render(ansi.Hardwrap(n.error, w, true))
	} else if len(n.list.VisibleItems()) == 0 {
		text := "No conversations"
		if n.list.FilterInput.Value() != "" {
			text = "No loaded matches\nCtrl+F Search conversations"
			if n.page.Query == n.list.FilterInput.Value() {
				text = "No host matches"
			}
		}
		rows = p.subtle().Render(text)
	}
	parts = append(parts, padBlock(rows, w, max(1, height-9)))
	scope := n.page.Navigation.Lifecycle
	if scope == "" {
		scope = "active"
	}
	if kind := n.page.Navigation.Filter; kind != "" && kind != "all" {
		scope += " · " + kind
	}
	count := fmt.Sprintf("%d shown", len(n.list.VisibleItems()))
	if len(n.list.VisibleItems()) != len(n.list.Items()) {
		count = fmt.Sprintf("%d/%d shown", len(n.list.VisibleItems()), len(n.list.Items()))
	}
	scope = count + " · " + scope
	page := "Snapshot"
	if n.page.Page.ObservedAt != "" {
		value := n.page.Page.ObservedAt
		if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
			value = parsed.Format("15:04:05")
		}
		page += " " + value
	}
	if n.page.Page.More {
		page += " · more"
	}
	parts = append(parts, p.subtle().Render(fitLine(scope, w)), p.subtle().Render(fitLine(page, w)))
	controls := "Refresh"
	if n.page.Page.Cursor > 0 {
		controls = "Prev  " + controls
	}
	if n.page.Page.NextCursor != nil {
		controls += "  Next"
	}
	hint := "Ctrl+O focus · F6 More"
	if n.focused {
		hint = "Enter open · F2 details"
		if n.list.FilterInput.Value() != "" {
			hint = "Ctrl+F Search conversations"
		}
	}
	parts = append(parts, p.plain().Render(fitLine(controls, w)), p.subtle().Render(fitLine(hint, w)))
	border := p.muted
	if n.focused {
		border = p.accent
	}
	return lipgloss.NewStyle().BorderRight(true).BorderStyle(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color(border)).PaddingRight(1).Render(padBlock(strings.Join(parts, "\n"), w, height))
}
