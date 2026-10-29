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

func TestWorkspaceToolbarCommandsReturnToInvokingPane(t *testing.T) {
	m := navigatorFixture(t, 120, 35)
	m.navigator.focused = true
	m.restoreFocus()
	draft, target, snapshot := m.currentDraft(), m.status.ThreadID, m.snapshot
	m.View()
	for _, z := range m.hitZones {
		if z.action == "toolbar" && (z.args == "commands" || z.args == "context") {
			t.Fatal("removed decorative toolbar control retained an invisible click target")
		}
	}
	m.handleKey(keyPress('p', tea.ModCtrl))
	if m.dialog == nil || m.dialog.local != "palette" || m.composer.Focused() {
		t.Fatal("toolbar failed to open Commands exclusively")
	}
	m.handleKey(keyPress(tea.KeyEscape))
	if m.dialog != nil || !m.navigator.focused || !m.navigator.list.FilterInput.Focused() || m.composer.Focused() {
		t.Fatal("closing toolbar Commands failed to restore its invoking pane")
	}
	if m.currentDraft() != draft || m.status.ThreadID != target || m.snapshot != snapshot {
		t.Fatal("local toolbar navigation changed work or attachment")
	}
}

func TestWorkspaceCompactRecoveryActionsMatchRenderedHitZones(t *testing.T) {
	for _, size := range [][2]int{{40, 12}, {60, 16}, {120, 35}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := navigatorFixture(t, size[0], size[1])
			m.status.Requests = 2
			m.status.PendingAction, m.status.PendingStop, m.status.DraftSaveFailed = true, true, true
			m.status.Receipt = &client.Receipt{Phase: "acknowledged", OperationID: "exact-operation", MessageID: "exact-message", ThreadID: "design"}
			m.resize()
			view := ansi.Strip(m.View().Content)
			lines := strings.Split(view, "\n")
			if len(lines) != size[1] {
				t.Fatal("recovery chrome displaced final controls")
			}
			for _, line := range lines {
				if ansi.StringWidth(line) != size[0] {
					t.Fatalf("invalid cell width: %q", line)
				}
			}
			for _, action := range []string{"requests", "stop", "pending"} {
				found := false
				for _, z := range m.hitZones {
					if z.action == "toolbar" && z.args == action {
						found = true
						if z.y != 2 || z.x+z.width > m.mainOriginX()+m.workspaceWidth() {
							t.Fatal("toolbar action points outside its rendered row")
						}
					}
				}
				if !found {
					t.Fatalf("compact operational action %q is hidden", action)
				}
			}
			durable := ansi.Strip(m.operationView(m.workspaceWidth()))
			for _, lane := range []string{"action", "Stop", "Draft"} {
				if !strings.Contains(durable, lane) {
					t.Fatalf("receipt crowded out %s: %s", lane, durable)
				}
			}
			detail := m.conversationDetailText()
			for _, identity := range []string{"exact-operation", "exact-message", "acknowledged", "receipt only"} {
				if !strings.Contains(detail, identity) {
					t.Fatalf("receipt detail lost %q", identity)
				}
			}
		})
	}
}

func TestWorkspaceContextSelectionScrollAndInspectionIdentity(t *testing.T) {
	m := navigatorFixture(t, 170, 40)
	m.sidebar, m.contextFocused = true, true
	items := make([]wire.ActivityItem, 24)
	for i := range items {
		items[i] = wire.ActivityItem{Key: fmt.Sprintf("design/item-%d", i), SourceThreadID: "design", ItemID: fmt.Sprint(i), Title: fmt.Sprintf("Activity item %02d", i), Status: "completed"}
	}
	m.snapshot.Metadata.Activity.Items = items
	m.resize()
	m.reviewContextSelection()
	m.restoreFocus()
	draft, snapshot := m.currentDraft(), m.snapshot
	for range 23 {
		m.contextKey(keyPress(tea.KeyDown))
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "> + Activity item 23") || m.contextFocusKey != "design/item-23" {
		t.Fatal("keyboard focus reached an invisible or different activity")
	}
	visibleHit := false
	for _, z := range m.hitZones {
		if z.action == "activity" && z.args == "design/item-23" && z.x > m.mainOriginX()+m.contentWidth {
			visibleHit = z.y > m.transcriptTop() && z.y < m.transcriptTop()+m.bodyHeight
		}
	}
	if !visibleHit {
		t.Fatal("scrolled activity does not have matching source-bound mouse geometry")
	}
	m.snapshot.Metadata.Activity.Items[23].Key = "design/replacement"
	if cmd := m.contextKey(keyPress(tea.KeyEnter)); cmd != nil || !strings.Contains(m.notice, "changed") {
		t.Fatal("background activity replacement silently retargeted inspection")
	}
	if m.currentDraft() != draft || m.snapshot != snapshot || m.actionBusy {
		t.Fatal("inspection focus changed human work or dispatched an action")
	}
}

func TestWorkspaceRemovedActivityAndHiddenContextReleaseKeyboard(t *testing.T) {
	m := navigatorFixture(t, 170, 40)
	m.focusActivity()
	draft := m.currentDraft()
	next := *m.snapshot
	next.Metadata.Activity = &wire.ActivityPage{}
	m.Update(sessionMsg{generation: m.generation, event: client.Event{Revision: m.revision + 1, Snapshot: &next, Status: m.status}})
	if m.activityFocused || !m.transcriptFocus || m.composer.Focused() {
		t.Fatal("a removed activity strip retained keyboard focus")
	}
	m.sidebar, m.contextFocused, m.transcriptFocus = true, true, false
	m.restoreFocus()
	m.Update(settingMsg{kind: "sidebar", value: "off"})
	if m.contextFocused || !m.transcriptFocus || m.currentDraft() != draft {
		t.Fatal("hiding context retained invisible focus or edited draft")
	}
}

func TestWorkspaceSlashCompletionOnlyOverComposerAndClosedDetailCannotCopy(t *testing.T) {
	m := navigatorFixture(t, 120, 35)
	m.restoreDraft("/mo")
	m.refreshCompletion()
	m.focusNavigator()
	if m.completionVisible() {
		t.Fatal("slash completion obscured a different focused pane")
	}
	m.openDetail("Exact inspected item", "result")
	m.detailCopy = "exact\tresult"
	m.handleKey(keyPress(tea.KeyEscape))
	if m.detailCopyText() != "" || m.currentDraft() != "/mo" {
		t.Fatal("closed inspection remained a copy target or changed draft")
	}
}

func TestWorkspacePaletteAvailabilityKeepsIndependentRecoveryLanes(t *testing.T) {
	m := testModel(t)
	m.status.PendingStop = true
	for _, raw := range m.paletteItems() {
		item := raw.(choiceItem)
		switch item.command {
		case "retry", "resolve":
			if item.disabled != (item.args != "stop") {
				t.Fatal("ordinary and Stop recovery availability were conflated")
			}
		case "stop", "requests", "earlier", "activity":
			if !item.disabled || !strings.Contains(item.description, "Unavailable:") {
				t.Fatalf("unsupported action %s pretends to be usable", item.command)
			}
		}
	}
	if m.actionBusy || m.stopBusy || !m.status.PendingStop {
		t.Fatal("rendering available commands changed recovery state")
	}
}
