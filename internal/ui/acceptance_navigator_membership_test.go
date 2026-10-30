package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
)

func membershipNavigator(t *testing.T) (*Model, map[string]client.NavigatorPage) {
	t.Helper()
	state, err := config.OpenState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	manager := client.NewManager([]config.Environment{
		{ID: "alpha", EnvironmentID: "env-alpha", Label: "Alpha", Enabled: true},
		{ID: "beta", EnvironmentID: "env-beta", Label: "Beta", Enabled: true},
	}, state)
	session, err := manager.NewSession("alpha", "attached")
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel(context.Background(), session, nil)
	t.Cleanup(func() { _ = m.Close(); _ = manager.Close(); _ = state.Close() })
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 35})
	m.navigator.profile = allProfiles
	m.navigator.pending = map[string]bool{"alpha": true, "beta": true}
	pages := map[string]client.NavigatorPage{}
	for _, profile := range []string{"alpha", "beta"} {
		parent, fork := "main", "fork"
		page := client.NavigatorPage{
			ProfileID: profile, EnvironmentID: "env-" + profile, Label: profile, Tree: true,
			Navigation: client.NavigationState{Filter: "all", Lifecycle: "active", Expanded: map[string]bool{}},
			Page: client.ThreadPage{ObservedAt: "2026-10-30T12:00:00Z", Limit: 24, Threads: []client.Thread{
				{ID: "main", ProjectID: "project", Project: "Project", Title: "Task main " + profile, UpdatedAt: "2026-10-30T12:00:00Z", Lineage: &wire.Lineage{}},
				{ID: "fork", ProjectID: "project", Project: "Project", Title: "Task fork " + profile, UpdatedAt: "2026-10-30T11:00:00Z", Lineage: &wire.Lineage{RelationshipToParent: &fork, ParentThreadID: &parent}},
			}},
		}
		pages[profile] = page
		m.navigatorLoaded(navigatorMsg{generation: m.generation, serial: m.navigator.serial, profile: profile, page: page})
	}
	m.navigator.focused = true
	m.navigator.list.SetFilterText("Task")
	m.navigator.list.FilterInput.SetCursor(2)
	m.composer.SetValue("  exact attached draft\nsecond line  ")
	return m, pages
}

func selectNavigatorIdentity(t *testing.T, m *Model, identity string) {
	t.Helper()
	for i, raw := range m.navigator.list.VisibleItems() {
		if raw.(navigatorItem).identity() == identity {
			m.navigator.list.Select(i)
			return
		}
	}
	t.Fatal("missing offered identity", identity)
}

// These tests run the real local navigation admission and result reducers. The
// read commands are deliberately not executed; actual adapter/program tests
// separately exercise the RPC reads and rendered mouse selection.
func TestAcceptanceNavigatorExplicitFilterRetiresAllOldSources(t *testing.T) {
	m, pages := membershipNavigator(t)
	selectNavigatorIdentity(t, m, "alpha\x00fork")
	draft, caret := m.currentDraft(), m.navigator.list.FilterInput.Position()
	oldSerial := m.navigator.serial
	if command := m.navigatorAction("type:forks"); command == nil {
		t.Fatal("explicit filter did not admit a fresh metadata read")
	}
	if m.navigator.serial == oldSerial || m.navigator.loaded || len(m.navigator.pages) != 0 || len(m.navigator.ordered) != 0 || len(m.navigator.list.Items()) != 0 || len(m.navigator.pending) != 2 {
		t.Fatal("explicit filter retained old-source membership or missed a source", m.navigator)
	}
	if _, _, ok := m.navigatorSource(); ok {
		t.Fatal("retired filtered row remained actionable while reading")
	}
	if m.navigator.list.FilterInput.Value() != "Task" || m.navigator.list.FilterInput.Position() != caret || m.currentDraft() != draft {
		t.Fatal("explicit membership reset changed the filter editor or attached draft")
	}
	m.navigatorLoaded(navigatorMsg{generation: m.generation, serial: oldSerial, profile: "beta", page: pages["beta"]})
	if len(m.navigator.list.Items()) != 0 {
		t.Fatal("late old serial repopulated retired membership")
	}
	for _, profile := range []string{"alpha", "beta"} {
		fresh := pages[profile]
		fresh.Navigation.Filter = "forks"
		fresh.Page.Threads = fresh.Page.Threads[1:]
		m.navigatorLoaded(navigatorMsg{generation: m.generation, serial: m.navigator.serial, profile: profile, page: fresh})
		for _, raw := range m.navigator.list.Items() {
			row := raw.(navigatorItem)
			if row.thread.ID != "fork" || row.stale {
				t.Fatal("explicit filter kept an old nonmatching slot", row)
			}
		}
		page, row, ok := m.navigatorSource()
		if !ok || page.ProfileID != "alpha" || row.ID != "fork" {
			t.Fatal("fresh exact source was unavailable or another source stole selection", page, row, ok)
		}
		if profile == "alpha" && (!m.navigator.loading || len(m.navigator.list.Items()) != 1) {
			t.Fatal("healthy source waited for delayed source or inherited its old rows")
		}
	}
	if m.navigator.loading || len(m.navigator.list.Items()) != 2 || m.navigator.list.FilterInput.Position() != caret || m.currentDraft() != draft {
		t.Fatal("completed filter lost exact editor/source state")
	}
}

func TestAcceptanceNavigatorBackgroundKeepsDisabledSlotUntilExplicitRefresh(t *testing.T) {
	m, pages := membershipNavigator(t)
	selectNavigatorIdentity(t, m, "alpha\x00main")
	draft, caret := m.currentDraft(), m.navigator.list.FilterInput.Position()
	if command := m.loadNavigator(allProfiles, "", true); command == nil {
		t.Fatal("background read was not admitted")
	}
	if len(m.navigator.list.Items()) != 4 {
		t.Fatal("background refresh moved the focused rows at admission")
	}
	if _, _, ok := m.navigatorSource(); ok {
		t.Fatal("refreshing source remained actionable before a fresh offer")
	}
	empty := pages["alpha"]
	empty.Page.Threads = nil
	m.navigatorLoaded(navigatorMsg{generation: m.generation, serial: m.navigator.serial, profile: "alpha", page: empty})
	m.navigatorLoaded(navigatorMsg{generation: m.generation, serial: m.navigator.serial, profile: "beta", page: pages["beta"]})
	selected := m.navigator.list.SelectedItem().(navigatorItem)
	if len(m.navigator.list.Items()) != 4 || selected.identity() != "alpha\x00main" || !selected.stale || !m.navigator.orderDeferred {
		t.Fatal("background removal moved the focused slot instead of disabling it", selected)
	}
	if _, _, ok := m.navigatorSource(); ok {
		t.Fatal("removed frozen slot was actionable")
	}
	oldSerial := m.navigator.serial
	if command := m.navigatorAction("refresh"); command == nil || len(m.navigator.list.Items()) != 0 || len(m.navigator.ordered) != 0 {
		t.Fatal("explicit Refresh did not retire held membership")
	}
	m.navigatorLoaded(navigatorMsg{generation: m.generation, serial: oldSerial, profile: "alpha", page: pages["alpha"]})
	m.navigatorLoaded(navigatorMsg{generation: m.generation, serial: m.navigator.serial, profile: "alpha", page: empty})
	m.navigatorLoaded(navigatorMsg{generation: m.generation, serial: m.navigator.serial, profile: "beta", page: pages["beta"]})
	if len(m.navigator.list.Items()) != 2 {
		t.Fatal("explicit Refresh retained stale rows after fresh membership")
	}
	for _, raw := range m.navigator.list.Items() {
		row := raw.(navigatorItem)
		if row.profile != "beta" || row.stale {
			t.Fatal("retired source reappeared after explicit Refresh", row)
		}
	}
	if m.navigator.list.FilterInput.Value() != "Task" || m.navigator.list.FilterInput.Position() != caret || m.currentDraft() != draft {
		t.Fatal("refresh changed the filter editor or attached draft")
	}
}

func TestAcceptanceNavigatorServerContentResultsAndLocalQueryRetirement(t *testing.T) {
	for _, size := range flowSizes {
		for _, edit := range []string{"keyboard", "paste"} {
			t.Run(fmt.Sprintf("%dx%d/%s", size[0], size[1], edit), func(t *testing.T) {
				m, pages := membershipNavigator(t)
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				m.navigator.list.SetFilterText("")
				m.restoreFocus()
				draft := m.currentDraft()
				m.Update(tea.PasteMsg{Content: "Markdown"})
				if len(m.navigator.list.VisibleItems()) != 0 || m.navigator.serverSearch {
					t.Fatal("local typing searched remotely or the fixture title unexpectedly matched")
				}
				m.View()
				if _, cmd := m.Update(keyPress(tea.KeyEnter)); cmd == nil || !m.navigator.serverSearch || m.navigator.query != "Markdown" {
					t.Fatal("explicit zero-match search did not capture the exact submitted query")
				}
				serial := m.navigator.serial
				fresh := pages["alpha"]
				fresh.Query = "Markdown"
				fresh.Page.SearchIncomplete = true
				fresh.Page.SearchScope = "native recent message search"
				m.Update(navigatorMsg{generation: m.generation, serial: serial, profile: "alpha", page: fresh})
				m.View()
				if len(m.navigator.list.VisibleItems()) != 2 || m.navigator.list.FilterInput.Value() != "Markdown" || !strings.Contains(m.navigatorHealth(), "1 searched") || !strings.Contains(m.navigatorHealth(), "1 incomplete") {
					t.Fatal("native message matches were removed by the old local title query", m.navigatorHealth())
				}
				page, row, ok := m.navigatorSource()
				if !ok || page.ProfileID != "alpha" || page.EnvironmentID != "env-alpha" || page.Query != "Markdown" || row.ID != "main" || !m.navigator.loading {
					t.Fatal("fast result lost its exact native query/source or waited for the other source", page, row, ok)
				}
				m.navigator.list.Select(1)
				m.Update(keyPress(tea.KeyLeft))
				if !m.navigator.serverSearch || m.navigator.serial != serial || len(m.navigator.list.VisibleItems()) != 2 || m.navigator.list.SelectedItem().(navigatorItem).thread.ID != "fork" {
					t.Fatal("cursor-only movement changed server-result membership or exact selection")
				}
				m.Update(keyPress(tea.KeyEnd))
				m.Update(keyPress('u', tea.ModCtrl))
				if m.navigator.serverSearch || m.navigator.serial == serial || m.navigator.loading || len(m.navigator.pending) != 0 {
					t.Fatal("changed input did not retire the old server search generation")
				}
				// Old native replies and delayed editor events belong to the
				// retired query and cannot alter the new local filter.
				late := pages["beta"]
				late.Query = "Markdown"
				m.Update(navigatorMsg{generation: m.generation, serial: serial, profile: "beta", page: late})
				m.Update(componentMsg{generation: m.generation, id: serial, kind: "navigator", message: tea.PasteMsg{Content: "retired"}})
				query := "Task fork alpha"
				if edit == "paste" {
					m.Update(tea.PasteMsg{Content: query})
				} else {
					for _, r := range query {
						m.Update(keyPress(r))
					}
				}
				m.View()
				page, row, ok = m.navigatorSource()
				if !ok || page.ProfileID != "alpha" || row.ID != "fork" || len(m.navigator.list.VisibleItems()) != 1 || len(m.navigator.pages) != 1 || m.navigator.list.FilterInput.Value() != query || m.navigator.query != "" {
					t.Fatal("new local query was changed by retired server results or ignored its current loaded filter", page, row, ok)
				}
				if m.currentDraft() != draft || !strings.Contains(m.navigatorHealth(), "Loaded search results") || !strings.Contains(m.navigatorHealth(), "incomplete") {
					t.Fatal("query editing changed the attached draft or concealed partial loaded-search scope")
				}
			})
		}
	}
}
