package ui

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Xon333/t3oxide/internal/client"
)

const allProfiles = "*"

func (i navigatorItem) identity() string { return i.profile + "\x00" + i.thread.ID }

// Project accents are a pure function of exact authority and project identity.
// Refreshes, priorities, duplicate names and inserted rows cannot recolor them.
func projectAccent(environment, project, theme string) string {
	p := colors(theme)
	h := fnv.New32a()
	_, _ = h.Write([]byte(environment + "\x00" + project))
	return []string{p.accent, p.secondary, p.operation}[h.Sum32()%3]
}

func environmentMark(label, profile string) string {
	if label == "" {
		label = profile
	}
	for _, r := range label {
		if r >= 'a' && r <= 'z' {
			return strings.ToUpper(string(r))
		}
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return string(r)
		}
	}
	return "@"
}

func (m *Model) unifiedNavigatorLoad(profile, query string, force bool) tea.Cmd {
	if m.session == nil || !m.navigationVisible() {
		return nil
	}
	n := &m.navigator
	if profile == "" {
		profile = n.profile
	}
	if profile == "" {
		profile = allProfiles
	}
	if !force && ((n.loaded && n.profile == profile) || n.loading) {
		return nil
	}
	if n.cancel != nil {
		n.cancel()
	}
	if i, ok := n.list.SelectedItem().(navigatorItem); ok {
		n.selected = i.identity()
	}
	n.serial++
	serial, generation, session := n.serial, m.generation, m.session
	ctx, cancel := context.WithCancel(m.sessionCtx)
	n.cancel = cancel
	n.profile, n.query, n.error = profile, query, ""
	n.pages = make(map[string]client.NavigatorPage)
	n.pending = make(map[string]bool)
	n.failures = make(map[string]string)
	n.loaded, n.loading = false, false
	scope := session.NavigatorScope()
	if profile != allProfiles {
		scope = []string{profile}
	}
	// Bound simultaneous helpers independently of profile count. Each result
	// is delivered immediately: an offline host never holds healthy rows back.
	slots := make(chan struct{}, 4)
	commands := make([]tea.Cmd, 0, len(scope))
	for _, id := range scope {
		n.pending[id] = true
		commands = append(commands, func() tea.Msg {
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return navigatorMsg{generation: generation, serial: serial, profile: id, err: ctx.Err()}
			}
			defer func() { <-slots }()
			page, err := session.NavigatorRead(ctx, id, query)
			return navigatorMsg{generation: generation, serial: serial, profile: id, page: page, err: err}
		})
	}
	n.loading = len(n.pending) > 0
	if !n.loading {
		n.error = "No environments configured"
	}
	m.layoutDirty = true
	return tea.Batch(commands...)
}

func (m *Model) unifiedNavigatorLoaded(msg navigatorMsg) tea.Cmd {
	n := &m.navigator
	if msg.generation != m.generation || msg.serial != n.serial {
		return nil
	}
	profile := msg.profile
	if profile == "" {
		profile = msg.page.ProfileID
	}
	if n.profile == "" {
		n.profile = profile
	}
	if n.pages == nil {
		n.pages = make(map[string]client.NavigatorPage)
	}
	if n.failures == nil {
		n.failures = make(map[string]string)
	}
	delete(n.pending, profile)
	n.loading = len(n.pending) > 0
	m.layoutDirty = true
	if msg.err != nil {
		n.failures[profile] = safeText(msg.err.Error())
		if len(n.pages) == 0 && !n.loading {
			n.error = safeText(msg.err.Error())
		}
	} else {
		delete(n.failures, profile)
		n.pages[profile] = msg.page
		n.error = ""
		n.loaded = true
		n.loadedAt = time.Now()
	}
	if len(n.pages) > 0 {
		m.rebuildUnifiedNavigator()
	}
	if !n.loading && n.cancel != nil {
		n.cancel()
		n.cancel = nil
	}
	m.resizeNavigator()
	return nil
}

func (m *Model) rebuildUnifiedNavigator() {
	n := &m.navigator
	identity := n.selected
	if item, ok := n.list.SelectedItem().(navigatorItem); ok {
		identity = item.identity()
	}
	input := n.list.FilterInput
	filter := n.list.FilterValue()
	var rows []navigatorItem
	page := client.NavigatorPage{ProfileID: n.profile, Label: "All environments", Query: n.query}
	ids := make([]string, 0, len(n.pages))
	for id := range n.pages {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		source := n.pages[id]
		if n.profile != allProfiles {
			page = source
		}
		for _, row := range source.Page.Threads {
			rows = append(rows, navigatorItem{thread: row, profile: id, environment: source.EnvironmentID, label: source.Label})
		}
		if source.Page.More {
			page.Page.More = true
		}
		if source.Page.NextCursor != nil {
			next := 1
			page.Page.NextCursor = &next
		}
		page.Page.Cursor = max(page.Page.Cursor, source.Page.Cursor)
		page.Tree = page.Tree || source.Tree
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].thread.UpdatedAt != rows[j].thread.UpdatedAt {
			return rows[i].thread.UpdatedAt > rows[j].thread.UpdatedAt
		}
		return rows[i].identity() < rows[j].identity()
	})
	items := make([]list.Item, len(rows))
	for i := range rows {
		items[i] = rows[i]
	}
	n.page = page
	n.list.SetItems(items)
	if filter != "" {
		n.list.SetFilterText(filter)
		n.list.SetFilterState(list.FilterApplied)
	} else {
		n.list.ResetFilter()
	}
	n.list.FilterInput = input
	for i, raw := range n.list.VisibleItems() {
		if raw.(navigatorItem).identity() == identity {
			n.list.Select(i)
			break
		}
	}
	if selected, ok := n.list.SelectedItem().(navigatorItem); ok {
		n.selected = selected.identity()
	}
}

func (m *Model) navigatorSource() (client.NavigatorPage, client.Thread, bool) {
	i, ok := m.navigator.list.SelectedItem().(navigatorItem)
	if !ok {
		return client.NavigatorPage{}, client.Thread{}, false
	}
	if i.profile != "" {
		page, exists := m.navigator.pages[i.profile]
		return page, i.thread, exists && !m.navigator.pending[i.profile] && m.navigator.failures[i.profile] == ""
	}
	return m.navigator.page, i.thread, m.navigator.loaded && !m.navigator.loading
}

func (m *Model) navigatorPageLive(page client.NavigatorPage) bool {
	if page.ProfileID == allProfiles && m.navigator.profile == allProfiles {
		return true
	}
	if m.navigator.pages == nil {
		return m.navigator.loaded && !m.navigator.loading && page.ProfileID == m.navigator.profile
	}
	_, ok := m.navigator.pages[page.ProfileID]
	return ok && !m.navigator.pending[page.ProfileID] && m.navigator.failures[page.ProfileID] == ""
}

func (m *Model) changeNavigatorPages(action, target string) error {
	if len(m.navigator.pages) == 0 {
		return m.session.NavigatorChange(m.navigator.page, action, target)
	}
	for _, page := range m.navigator.pages {
		if action == "next" && page.Page.NextCursor == nil || action == "previous" && page.Page.Cursor == 0 {
			continue
		}
		if strings.HasPrefix(action, "type:") && !page.Tree {
			continue
		}
		if err := m.session.NavigatorChange(page, action, target); err != nil {
			return err
		}
	}
	return nil
}

func (m *Model) navigatorHealth() string {
	n := &m.navigator
	var parts []string
	if len(n.pending) > 0 {
		parts = append(parts, fmt.Sprintf("%d loading", len(n.pending)))
	}
	if len(n.failures) > 0 {
		ids := make([]string, 0, len(n.failures))
		for id := range n.failures {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		parts = append(parts, "! "+strings.Join(ids, ", "))
	}
	if n.page.Page.More {
		parts = append(parts, "More available")
	}
	return strings.Join(parts, " · ")
}

func (m *Model) emptyWorkspaceView() string {
	p := colors(m.theme)
	width := m.mainWidth()
	return padBlock(lipgloss.JoinVertical(lipgloss.Left, p.active().Render("t3oxide"), ""), width, m.height)
}
