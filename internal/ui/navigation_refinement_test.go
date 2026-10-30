package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/wire"
	"github.com/charmbracelet/x/ansi"
)

func refinementNavigation(t *testing.T, count int) *Model {
	t.Helper()
	m := testModel(t)
	m.width, m.height = 120, 35
	m.status = client.Status{ProfileID: "attached", EnvironmentID: "attached-env", ThreadID: "attached-thread"}
	page := client.NavigatorPage{ProfileID: "source", EnvironmentID: "source-env", Label: "Source", Page: client.ThreadPage{Limit: 200}}
	for i := range count {
		page.Page.Threads = append(page.Page.Threads, client.Thread{ID: fmt.Sprintf("row-%02d", i), Title: fmt.Sprintf("Synthetic title %02d", i), UpdatedAt: fmt.Sprintf("%03d", count-i), Status: "running", Project: "Synthetic", Lineage: &wire.Lineage{}, Inbox: &wire.InboxMetadata{Working: new(true)}})
	}
	m.navigatorLoaded(navigatorMsg{generation: m.generation, serial: m.navigator.serial, page: page})
	m.navigator.focused = true
	m.resize()
	return m
}

func navigatorRowHits(m *Model) map[string]hitZone {
	out := make(map[string]hitZone)
	for _, hit := range m.navigator.surface.hits {
		if hit.action == "navigator-row" {
			out[hit.args] = hit
		}
	}
	return out
}

func TestRefinementNavigatorFocusedSlotsSurviveNewShelfTitleAndRemoval(t *testing.T) {
	m := refinementNavigation(t, 8)
	_ = m.View()
	before := navigatorRowHits(m)
	page := m.navigator.pages["source"]
	page.Page.Threads = append([]client.Thread(nil), page.Page.Threads...)
	page.Page.Threads[0].Title = strings.Repeat("Identical prefix 界 ", 12) + "unique end α"
	page.Page.Threads[0].Status = "failed"
	page.Page.Threads[0].Inbox = &wire.InboxMetadata{PendingRequestCount: 1}
	page.Page.Threads = append(page.Page.Threads, client.Thread{ID: "new-pin", Title: "Pinned later", Lineage: &wire.Lineage{}, Inbox: &wire.InboxMetadata{PinnedAt: "synthetic"}})
	m.Update(navigatorMsg{generation: m.generation, serial: m.navigator.serial, profile: "source", page: page})
	plain := ansi.Strip(m.View().Content)
	after := navigatorRowHits(m)
	for key, hit := range before {
		if next, ok := after[key]; !ok || hit.y != next.y || hit.height != next.height {
			t.Fatalf("metadata moved drawn target %q: before=%+v after=%+v", key, hit, next)
		}
	}
	if !m.navigator.orderDeferred || !strings.Contains(plain, "! Failed") || !strings.Contains(plain, "?1") || !strings.Contains(plain, "unique end α") {
		t.Fatalf("frozen order/title tail/request cue missing: %s", plain)
	}
	page.Page.Threads = page.Page.Threads[1:]
	m.Update(navigatorMsg{generation: m.generation, serial: m.navigator.serial, profile: "source", page: page})
	if _, _, ok := m.navigatorSource(); ok {
		t.Fatal("removed but visually held source became actionable")
	}
	if selected := m.navigator.list.SelectedItem().(navigatorItem); !selected.stale || selected.thread.ID != "row-00" {
		t.Fatal("removed focused target jumped to another row")
	}
	m.Update(keyPress('o', tea.ModCtrl))
	if m.navigator.focused || m.navigator.orderDeferred || m.navigator.list.Items()[0].(navigatorItem).thread.ID != "new-pin" {
		t.Fatal("leaving did not apply queued native shelf ordering")
	}
}

func TestRefinementNavigatorCollapsedShelfRetainsCueAndRealToggle(t *testing.T) {
	m := refinementNavigation(t, 2)
	page := m.navigator.pages["source"]
	page.Page.Threads[0].Inbox = &wire.InboxMetadata{PendingRequestCount: 1}
	page.Page.Threads[1].Inbox = &wire.InboxMetadata{PendingChildRequest: true}
	m.navigator.focused = false
	m.Update(navigatorMsg{generation: m.generation, serial: m.navigator.serial, profile: "source", page: page})
	m.toggleNavigatorShelf("Needs you")
	plain := ansi.Strip(m.View().Content)
	if !strings.Contains(plain, "Needs you (2) · ?2") || strings.Contains(plain, "No conversations") {
		t.Fatalf("collapsed last shelf hid its pending cue/toggle: %s", plain)
	}
	found := false
	for _, hit := range m.hitZones {
		if hit.action == "navigator-shelf" {
			m.Update(tea.MouseClickMsg{X: hit.x, Y: hit.y, Button: tea.MouseLeft})
			found = true
			break
		}
	}
	if !found || len(m.navigator.list.VisibleItems()) != 2 {
		t.Fatal("actual drawn collapsed shelf could not reopen")
	}
}

func TestRefinementNavigatorNumbersUseActuallyDrawnRows(t *testing.T) {
	m := refinementNavigation(t, 25)
	m.width, m.height = 60, 16
	m.resize()
	m.navigator.list.Select(18)
	_ = m.View()
	var first string
	for _, hit := range m.navigator.surface.hits {
		if hit.action == "navigator-row" {
			first = hit.args
			break
		}
	}
	if first == "" || first == "source\x00row-00" {
		t.Fatal("fixture did not expose a scrolled custom row region")
	}
	m.navigator.list.Select(0)
	m.openVisibleNavigatorNumber(1)
	if got := m.navigator.list.SelectedItem().(navigatorItem).identity(); got != first {
		t.Fatalf("numbered shortcut chose hidden paginator row %q, drawn %q", got, first)
	}
	m.navigator.serial++
	m.navigator.list.Select(0)
	m.openVisibleNavigatorNumber(1)
	if m.navigator.list.Index() != 0 {
		t.Fatal("retired surface numbering remained active")
	}
}

func TestRefinementNavigatorPreviewYieldsToRequestsAndExactSource(t *testing.T) {
	for _, change := range []string{"request", "source", "selection"} {
		t.Run(change, func(t *testing.T) {
			m := refinementNavigation(t, 2)
			target := m.navigator.list.SelectedItem().(navigatorItem).identity()
			m.navigator.previewTarget, m.navigator.previewLoading = target, true
			msg := navigatorPreviewMsg{generation: m.generation, serial: m.navigator.previewSerial, pageSerial: m.navigator.serial, target: target, preview: client.ThreadPreview{ProfileID: "source", EnvironmentID: "source-env", ThreadID: "row-00", Available: false, Unavailable: "fixture metadata"}}
			switch change {
			case "request":
				m.openLocal("Native request", "request-fixture", nil)
			case "source":
				msg.preview.EnvironmentID = "different-env"
			case "selection":
				m.navigator.list.Select(1)
			}
			m.Update(msg)
			if m.detail != "" || m.navigator.preview != nil {
				t.Fatal("optional preview displaced priority surface or changed source")
			}
		})
	}
}

func TestRefinementNavigatorPinAndPreviewKeysDoNotStealFilterText(t *testing.T) {
	m := refinementNavigation(t, 2)
	m.Update(keyPress('/'))
	m.Update(keyPress('p'))
	m.Update(keyPress(' '))
	if !m.navigator.editing || m.navigator.list.FilterInput.Value() != "p " {
		t.Fatal("filter editing activated pin/preview instead of preserving input")
	}
	m.Update(keyPress(tea.KeyEscape))
	if !m.navigator.focused || m.navigator.editing {
		t.Fatal("first Escape did not leave filter editing only")
	}
}

func TestRefinementExpandedTitleKeepsDistinctUnicodeEndsWithinCells(t *testing.T) {
	for _, width := range []int{1, 2, 22, 34} {
		a := expandedTitleLines(strings.Repeat("Same prefix 界 ", 20)+"尾α", width)
		b := expandedTitleLines(strings.Repeat("Same prefix 界 ", 20)+"尾β", width)
		for _, line := range append(a[:], b[:]...) {
			if ansi.StringWidth(line) > width {
				t.Fatalf("expanded title exceeded cell width: %q", line)
			}
		}
		if width >= 2 && a[1] == b[1] {
			t.Fatal("same-prefix selected titles hid their distinct ending")
		}
	}
}
