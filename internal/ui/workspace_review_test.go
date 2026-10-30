package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestWorkspaceSearchOwnsKeysAndRestoresInvokingPane(t *testing.T) {
	for _, pane := range []string{"navigator", "context", "activity"} {
		t.Run(pane, func(t *testing.T) {
			m := navigatorFixture(t, 170, 40)
			m.sidebar = true
			m.navigator.focused = pane == "navigator"
			m.navigator.editing = pane == "navigator"
			m.contextFocused = pane == "context"
			m.activityFocused = pane == "activity"
			m.transcriptFocus = false
			m.reviewContextSelection()
			m.resize()
			m.restoreFocus()
			m.transcript = strings.Repeat("retained reading row\n", 80)
			m.viewport.SetContent(m.transcript)
			m.viewport.SetYOffset(9)
			m.follow = false
			draft, snapshot, thread := m.currentDraft(), m.snapshot, m.status.ThreadID
			query, selection, offset := m.navigator.list.FilterInput.Value(), m.contextSelection, m.viewport.YOffset()

			m.toolbarAction("commands")
			found := false
			for i, raw := range m.dialog.list.VisibleItems() {
				if raw.(choiceItem).command == "search" {
					m.dialog.list.Select(i)
					found = true
					break
				}
			}
			if !found {
				t.Fatal("Find is missing from Commands")
			}
			m.chooseDialog()
			if !m.searching || !m.searchInput.Focused() || m.composer.Focused() || m.navigator.list.FilterInput.Focused() {
				t.Fatal("Find did not take exclusive native input focus")
			}
			m.Update(keyPress('q'))
			m.Update(tea.PasteMsg{Content: "界"})
			if m.searchInput.Value() != "q界" || m.navigator.list.FilterInput.Value() != query || m.contextSelection != selection {
				t.Fatal("search edits reached the invoking pane or were dropped")
			}
			m.Update(keyPress(tea.KeyEscape))
			if m.searching || m.searchInput.Focused() || m.composer.Focused() {
				t.Fatal("Escape left search or composer owning the pane's keyboard")
			}
			if m.navigator.focused != (pane == "navigator") || m.contextFocused != (pane == "context") || m.activityFocused != (pane == "activity") || m.transcriptFocus {
				t.Fatal("Escape lost the invoking pane")
			}
			if m.navigator.list.FilterInput.Focused() != (pane == "navigator") {
				t.Fatal("Escape failed to restore the invoking native editor")
			}
			if m.currentDraft() != draft || m.snapshot != snapshot || m.status.ThreadID != thread || m.viewport.YOffset() != offset || m.follow || m.actionBusy || m.stopBusy {
				t.Fatal("local search changed draft, attachment, reading position or operation state")
			}
		})
	}
}

func TestWorkspaceShortContextUsesVisibleReviewInsteadOfHiddenActivity(t *testing.T) {
	m := navigatorFixture(t, 170, 40)
	m.sidebar, m.contextFocused = true, true
	m.composer.SetValue("first exact line\nsecond exact line")
	m.composer.SetHeight(2)
	m.status.PendingAction = true
	m.problem = "Retained operation requires review"
	m.reviewContextSelection()
	m.resize()
	m.restoreFocus()
	draft, snapshot, thread, key := m.currentDraft(), m.snapshot, m.status.ThreadID, m.contextFocusKey

	m.Update(tea.WindowSizeMsg{Width: 170, Height: 12})
	if m.bodyHeight != 1 {
		t.Fatalf("fixture must exercise one remaining context row, got %d", m.bodyHeight)
	}
	if m.contextVisible() || m.contextFocused || !m.transcriptFocus || !m.sidebar {
		t.Fatal("short context retained invisible focus or discarded the sidebar preference")
	}
	m.View()
	for _, z := range m.hitZones {
		if z.action == "context-focus" {
			t.Fatal("collapsed context retained an invisible mouse target")
		}
	}
	m.Update(keyPress(tea.KeyEnter))
	if m.actionBusy || m.dialog != nil {
		t.Fatal("Enter inspected the formerly focused hidden activity")
	}
	m.openContext()
	if m.dialog == nil || m.dialog.local != "context" {
		t.Fatal("Context did not offer the visible local review at short height")
	}
	found := false
	for _, raw := range m.dialog.list.Items() {
		if raw.(choiceItem).command == "activity" {
			found = true
		}
	}
	if !found {
		t.Fatal("compact context review lost the activity entry point")
	}
	m.cancelDialog()
	m.Update(tea.WindowSizeMsg{Width: 170, Height: 40})
	if !m.contextVisible() || m.contextFocused || !m.transcriptFocus {
		t.Fatal("resizing back lost the sidebar or revived its hidden keyboard focus")
	}
	if m.currentDraft() != draft || m.snapshot != snapshot || m.status.ThreadID != thread || m.contextFocusKey != key || !m.status.PendingAction || m.actionBusy || m.stopBusy {
		t.Fatal("compact context navigation changed human work or operation identity")
	}
}
