package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestSidebarCollapsedKeyboardAndMouse(t *testing.T) {
	for _, width := range []int{60, 120, 170} {
		m := navigatorFixture(t, width, 24)
		m.navigator.focused = true
		m.toggleNavigatorShelf("Working")
		m.toggleNavigatorShelf("Conversations")
		m.Update(keyPress(tea.KeyHome))
		if h, ok := m.navigator.list.SelectedItem().(navigatorHeading); !ok || h.shelf != "Working" {
			t.Fatal("home must select collapsed heading")
		}
		if _, _, ok := m.navigatorSource(); ok {
			t.Fatal("heading acquired thread target")
		}
		m.Update(keyPress(tea.KeyDown))
		if h := m.navigator.list.SelectedItem().(navigatorHeading); h.shelf != "Conversations" {
			t.Fatal("arrow skipped collapsed group")
		}
		m.Update(keyPress(' '))
		m.Update(keyPress(tea.KeyDown))
		if row, ok := m.navigator.list.SelectedItem().(navigatorItem); !ok || row.thread.ID != "docs" {
			t.Fatal("arrow did not cross heading into thread")
		}
		m.Update(keyPress(tea.KeyUp))
		m.Update(keyPress(tea.KeyHome))
		m.Update(keyPress(' '))
		m.Update(keyPress(tea.KeyDown))
		if row, ok := m.navigator.list.SelectedItem().(navigatorItem); !ok || row.thread.ID != "fleet" {
			t.Fatal("expanded heading lost rows")
		}
		_ = m.View()
		found := false
		for _, hit := range m.hitZones {
			if hit.action == "navigator-shelf" && hit.args == "Working" {
				found = true
				m.Update(tea.MouseClickMsg{X: hit.x, Y: hit.y, Button: tea.MouseLeft})
				if h, ok := m.navigator.list.SelectedItem().(navigatorHeading); !ok || h.shelf != "Working" || !m.navigator.collapsed[h.shelf] {
					t.Fatal("mouse and keyboard selection diverged")
				}
				break
			}
		}
		if !found {
			t.Fatal("drawn heading lacked mouse hit")
		}
		m.Update(keyPress(tea.KeyEnd))
		if m.navigator.list.Index() != len(m.navigator.list.VisibleItems())-1 {
			t.Fatal("end")
		}
		m.Update(keyPress(tea.KeyPgUp))
		m.Update(keyPress(tea.KeyPgDown))
		_ = m.View()
	}
}

func TestSidebarExplicitFilterFocus(t *testing.T) {
	m := navigatorFixture(t, 120, 35)
	m.navigator.focused = true
	m.restoreFocus()
	before := navigatorSelection(m.navigator.list.SelectedItem())
	m.Update(keyPress('x'))
	m.Update(tea.PasteMsg{Content: "unwanted"})
	if m.navigator.editing || m.navigator.list.FilterInput.Focused() || m.navigator.list.FilterInput.Value() != "" {
		t.Fatal("implicit filter editing")
	}
	m.Update(keyPress('/'))
	m.Update(tea.PasteMsg{Content: "documentation"})
	if !m.navigator.editing || len(m.navigator.list.VisibleItems()) != 1 {
		t.Fatal("explicit paste filter failed")
	}
	selected := navigatorSelection(m.navigator.list.SelectedItem())
	m.Update(keyPress(tea.KeyEscape))
	m.Update(keyPress('x'))
	m.Update(tea.PasteMsg{Content: "discard"})
	m.restoreFocus()
	m.resize()
	page := m.navigator.pages["linux-host"]
	m.Update(navigatorMsg{generation: m.generation, serial: m.navigator.serial, profile: "linux-host", page: page})
	if m.navigator.editing || m.navigator.list.FilterInput.Focused() || m.navigator.list.FilterInput.Value() != "documentation" || navigatorSelection(m.navigator.list.SelectedItem()) != selected || before == selected {
		t.Fatal("escape/focus/metadata changed query or selection")
	}
	_ = m.View()
}

func TestSidebarFrozenInterleavedShelvesDrawOnce(t *testing.T) {
	m := navigatorFixture(t, 120, 35)
	m.navigator.focused = true
	// Reproduce a frozen stream with later arrivals from earlier shelves.
	rows := m.navigator.ordered
	m.navigator.ordered = append(append(append([]navigatorItem{}, rows[0], rows[3]), rows[1]), rows[2])
	m.rebuildUnifiedNavigator()
	m.Update(keyPress(tea.KeyHome))
	plain := ansi.Strip(m.View().Content)
	if strings.Count(plain, "Working (3)") != 1 || strings.Count(plain, "Conversations (1)") != 1 || strings.Contains(plain, "continued") {
		t.Fatalf("duplicate headings: %s", plain)
	}
	var ids []string
	for _, raw := range m.navigator.list.Items() {
		if row, ok := raw.(navigatorItem); ok && row.shelf == "Working" {
			ids = append(ids, row.thread.ID)
		}
	}
	if strings.Join(ids, ",") != "fleet,design,child" {
		t.Fatal("intra-group order changed", ids)
	}
}

func TestSidebarFilterIncludesCollapsedLoadedThreads(t *testing.T) {
	m := navigatorFixture(t, 120, 35)
	m.navigator.focused = true
	m.toggleNavigatorShelf("Conversations")
	m.Update(keyPress('/'))
	m.Update(tea.PasteMsg{Content: "documentation"})
	if len(m.navigator.list.VisibleItems()) != 1 {
		t.Fatal("collapsed loaded thread omitted from explicit filter")
	}
	if !m.navigator.collapsed["Conversations"] {
		t.Fatal("filter changed saved collapse state")
	}
	if _, ok := m.navigator.list.SelectedItem().(navigatorItem); !ok {
		t.Fatal("filter did not offer exact conversation")
	}
}
