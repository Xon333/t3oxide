package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
	"github.com/charmbracelet/x/ansi"
)

func navigatorReviewToken(t *testing.T, m *Model, action string) string {
	t.Helper()
	for _, raw := range m.dialog.list.VisibleItems() {
		item := raw.(choiceItem)
		if strings.HasSuffix(item.args, ":"+action) {
			return item.args
		}
	}
	t.Fatalf("missing reviewed action %q", action)
	return ""
}

func TestNavigationReviewOpensReviewedIdentityAfterHighlightChanges(t *testing.T) {
	m := navigatorFixture(t, 120, 35)
	state, err := config.OpenState(filepath.Join(t.TempDir(), "go-drafts.json"))
	if err != nil {
		t.Fatal(err)
	}
	manager := client.NewManager([]config.Environment{{ID: "build-host", EnvironmentID: "build-id"}, {ID: "linux-host", EnvironmentID: "linux-id"}}, state)
	session, err := manager.NewSession("build-host", "design")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	m.session = session
	m.openNavigatorActions()
	token := navigatorReviewToken(t, m, "open")
	m.navigator.list.Select(1) // The menu still names Runtime ownership / fleet.
	cmd := m.chooseDialog()    // Activate the initial visible Open row as a user does.
	if cmd == nil {
		t.Fatal("reviewed open did not produce the lazy target")
	}
	var msg actionMsg
	var run func(tea.Cmd)
	run = func(next tea.Cmd) {
		if next == nil {
			return
		}
		switch result := next().(type) {
		case tea.BatchMsg:
			for _, child := range result {
				run(child)
			}
		case actionMsg:
			msg = result
		}
	}
	run(cmd)
	if msg.err != nil || msg.result.Next == nil {
		t.Fatalf("reviewed target failed: %#v", msg)
	}
	if target := msg.result.Next.Status(); target.ProfileID != "linux-host" || target.ThreadID != "fleet" {
		t.Fatalf("highlight silently retargeted the menu: %#v", target)
	}
	if m.status.ThreadID != "design" || m.status.ProfileID != "build-host" || m.snapshot.Metadata.ThreadID != "design" {
		t.Fatal("creating a lazy offered target adopted it before the command result")
	}
	if again := m.navigatorAction(token); again != nil {
		t.Fatal("consumed menu token remained actionable")
	}
}

func TestNavigationReviewRejectsRetiredPageAndAttachment(t *testing.T) {
	for _, retire := range []string{"page", "attachment", "loading", "failure", "replacement-menu"} {
		t.Run(retire, func(t *testing.T) {
			m := navigatorFixture(t, 120, 35)
			draft, snapshot := m.currentDraft(), m.snapshot
			m.openNavigatorActions()
			token := navigatorReviewToken(t, m, "details")
			switch retire {
			case "page":
				m.navigator.serial++
			case "attachment":
				m.generation++
			case "loading":
				m.navigator.loading = true
			case "failure":
				m.navigator.error = "unavailable"
			case "replacement-menu":
				m.openNavigatorActions()
			}
			if cmd := m.navigatorAction(token); cmd != nil || m.detail != "" || m.actionBusy || m.browserBusy {
				t.Fatal("retired menu performed an action")
			}
			if m.currentDraft() != draft || m.snapshot != snapshot || !strings.Contains(m.notice, "changed") {
				t.Fatal("retired menu lost working context or omitted recovery")
			}
		})
	}
}

func TestNavigationDetailsPreserveDraftAnchorAndExactMetadataTarget(t *testing.T) {
	m := navigatorFixture(t, 120, 35)
	m.viewport.SetContent(strings.Repeat("reading anchor\n", 60))
	m.viewport.SetYOffset(11)
	m.follow = false
	draft, offset, snapshot := m.currentDraft(), m.viewport.YOffset(), m.snapshot
	m.navigator.focused = true
	m.openNavigatorActions()
	token := navigatorReviewToken(t, m, "details")
	m.navigator.list.Select(1)
	m.dialog = nil
	m.navigatorAction(token)
	for _, want := range []string{"Linux host", "Conversation ID: fleet", "Runtime ownership", "Metadata only"} {
		if !strings.Contains(m.detail, want) {
			t.Fatalf("detail lost reviewed identity %q: %s", want, m.detail)
		}
	}
	if strings.Contains(m.detail, "Conversation ID: design") {
		t.Fatal("details followed a changed highlight")
	}
	m.Update(keyPress(tea.KeyEscape))
	if !m.navigator.focused || m.currentDraft() != draft || m.viewport.YOffset() != offset || m.snapshot != snapshot || m.follow {
		t.Fatal("metadata inspection changed attachment, draft, focus, or reading anchor")
	}
	m.Update(keyPress('l', tea.ModCtrl))
	if !strings.Contains(m.detail, "Conversation ID: design") || !strings.Contains(m.detail, "Browse: Linux host") || m.currentDraft() != draft {
		t.Fatal("portable Details shortcut did not inspect the current browse target")
	}
}

func TestNavigationReverseFocusTraversesVisibleContext(t *testing.T) {
	m := navigatorFixture(t, 170, 40)
	m.sidebar = true
	m.navigator.focused = true
	m.resize()
	m.restoreFocus()
	m.Update(keyPress(tea.KeyTab, tea.ModShift))
	if m.navigator.focused || !m.contextFocused || m.composer.Focused() {
		t.Fatal("reverse traversal skipped the visible last pane")
	}
	m.Update(keyPress(tea.KeyTab))
	if !m.navigator.focused || m.contextFocused {
		t.Fatal("reverse/forward focus traversal is not symmetric")
	}
}

func TestNavigationNoMatchesCannotActOnHiddenSelectedSource(t *testing.T) {
	m := navigatorFixture(t, 120, 35)
	draft, snapshot := m.currentDraft(), m.snapshot
	m.navigator.list.SetFilterText("no such conversation anywhere")
	m.navigator.list.SetFilterState(list.FilterApplied)
	if len(m.navigator.list.VisibleItems()) != 0 {
		t.Fatal("fixture still has matching targets")
	}
	m.openNavigatorActions()
	dialog := m.dialog
	disabled := 0
	for i, raw := range dialog.list.Items() {
		item := raw.(choiceItem)
		_, action, _ := strings.Cut(strings.TrimPrefix(item.args, "review:"), ":")
		switch action {
		case "open", "new", "settings", "details":
			if !item.disabled || !strings.Contains(item.description, "Select") {
				t.Fatalf("hidden source action %q lacks disabled explanation", action)
			}
			dialog.list.Select(i)
			if cmd := m.chooseDialog(); cmd != nil || m.dialog != dialog {
				t.Fatalf("disabled %q acted or discarded the menu", action)
			}
			disabled++
		}
	}
	if disabled != 4 {
		t.Fatalf("expected four guarded source actions, got %d", disabled)
	}
	m.openNavigatorDetail()
	if m.detail != "" || m.currentDraft() != draft || m.snapshot != snapshot || m.actionBusy || m.browserBusy {
		t.Fatal("an empty filtered list exposed or acted on its hidden selected row")
	}
}

func TestNavigationLaunchPrimaryControlsAndCompactGeometry(t *testing.T) {
	for _, size := range [][2]int{{40, 12}, {60, 16}, {120, 35}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := testModel(t)
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			input := client.LaunchInput{Title: "Exact title", Message: "Exact first message"}
			p, replies, _ := intuitivePrompt(t, launchQuestion(input))
			m.openPrompt(p)
			m.Update(keyPress(tea.KeyTab))
			m.Update(keyPress(tea.KeyEnter))
			assertIntuitiveNoReply(t, replies)
			if m.dialog == nil || m.dialog.launch.focus != 2 || m.launchInput() != input {
				t.Fatal("title confirmation did more than focus primary Create")
			}
			view := ansi.Strip(m.View().Content)
			if !strings.Contains(view, "[Create]") {
				t.Fatalf("primary control missing at %v:\n%s", size, view)
			}
			if len(strings.Split(view, "\n")) != size[1] {
				t.Fatalf("unexpected height at %v", size)
			}
			for _, row := range strings.Split(view, "\n") {
				if ansi.StringWidth(row) > size[0] {
					t.Fatalf("launch overflow at %v: %q", size, row)
				}
			}
		})
	}
}
