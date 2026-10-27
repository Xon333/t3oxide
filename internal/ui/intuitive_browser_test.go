package ui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/charmbracelet/x/ansi"
)

// Reuse a real local prompt broker without configuring any environment or
// starting a helper. Its scope command rejects non-scope actions before I/O.
func intuitivePrompt(t *testing.T, q client.Question) (*client.Prompt, <-chan error, context.CancelFunc) {
	t.Helper()
	manager := client.NewManager(nil, nil)
	session, err := manager.NewSession("", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { _, err := session.Command(ctx, "env", ""); result <- err }()
	t.Cleanup(func() { cancel(); _ = manager.Close() })
	select {
	case p := <-session.Prompts():
		p.Question = q
		return p, result, cancel
	case <-time.After(2 * time.Second):
		t.Fatal("local prompt broker did not open")
		return nil, nil, cancel
	}
}

func assertIntuitiveNoReply(t *testing.T, result <-chan error) {
	t.Helper()
	select {
	case err := <-result:
		t.Fatalf("local navigation answered client prompt: %v", err)
	default:
	}
}

func awaitIntuitiveReply(t *testing.T, result <-chan error) {
	t.Helper()
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "action was not offered") {
			t.Fatalf("expected non-scope action to reach the local broker: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("explicit action did not answer exact client prompt")
	}
}

func TestIntuitiveBrowserVisibleTraversal(t *testing.T) {
	for _, size := range [][2]int{{60, 16}, {80, 24}, {120, 35}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := testModel(t)
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			q := browserQuestion("a")
			q.BrowserPage.Actions = []string{"search", "resume", "profile:b", "settings", "refresh", "new", "profile:a", "environments", "future:opaque"}
			q.BrowserPage.Profiles[0].Label = "Alpha " + strings.Repeat("long profile 界 ", 12)
			q.BrowserPage.Profiles[1].Label = "Beta " + strings.Repeat("other profile ", 12)
			m.openPrompt(retiredPrompt(6400, q))
			controls := m.visibleBrowserControls()
			var order []string
			for _, control := range controls {
				order = append(order, control.action)
			}
			if !slices.Equal(order, []string{"profile:b", "profile:a", "environments", "new", "settings", "resume", browserMore}) {
				t.Fatalf("visible order differs from hierarchy: %v", order)
			}
			check := func(index int) {
				t.Helper()
				if m.dialog.actionFocus != index+1 {
					t.Fatalf("focus=%d, expected %d", m.dialog.actionFocus, index+1)
				}
				label := controls[index].label
				if head, _, found := strings.Cut(label, " long"); found {
					label = head
				} else if head, _, found := strings.Cut(label, " other"); found {
					label = head
				}
				if view := ansi.Strip(m.View().Content); !strings.Contains(view, "["+label) {
					t.Fatalf("focused control %q invisible at %v:\n%s", label, size, view)
				}
				if height := m.browserControlsHeight(m.modalWidth); height != lipgloss.Height(m.browserControls(m.modalWidth)) {
					t.Fatal("control reservation differs from rendered rows")
				}
			}
			for i := range controls {
				m.Update(keyPress(tea.KeyTab))
				check(i)
			}
			m.Update(keyPress(tea.KeyTab))
			if m.dialog.actionFocus != 0 {
				t.Fatal("forward traversal did not return to query")
			}
			for i := len(controls) - 1; i >= 0; i-- {
				m.Update(keyPress(tea.KeyTab, tea.ModShift))
				check(i)
			}
			m.Update(keyPress(tea.KeyTab, tea.ModShift))
			if m.dialog.actionFocus != 0 {
				t.Fatal("reverse traversal did not return to query")
			}
		})
	}
}

func TestIntuitiveBrowserMoreHelpRendersOneDetailPanel(t *testing.T) {
	for _, size := range [][2]int{{60, 16}, {80, 24}, {120, 35}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := testModel(t)
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			p, result, _ := intuitivePrompt(t, browserQuestion("a"))
			m.openPrompt(p)
			m.dialog.actionFocus = len(m.visibleBrowserControls())
			m.Update(keyPress(tea.KeyEnter))
			more := m.dialog
			for _, r := range "Help" {
				m.Update(keyPress(r))
			}
			m.Update(keyPress(tea.KeyEnter))
			view := ansi.Strip(m.View().Content)
			if m.detailTitle != "Help" || strings.Count(view, "╭") != 1 {
				t.Fatalf("full-page Help rendered duplicate or missing detail panels:\n%s", view)
			}
			for _, corner := range []string{"╮", "╰", "╯"} {
				if strings.Count(view, corner) != 1 {
					t.Fatalf("full-page Help clipped its %s frame corner at %v:\n%s", corner, size, view)
				}
			}
			if strings.Count(view, "Ctrl+Y copy") != 1 {
				t.Fatal("Help acquired duplicated or missing contextual copy controls")
			}
			assertIntuitiveNoReply(t, result)
			m.Update(keyPress(tea.KeyEscape))
			if m.dialog != more || m.dialog.list.FilterValue() != "Help" || m.detail != "" {
				t.Fatal("closing the sole Help panel changed the suspended More query")
			}
		})
	}
}

func TestIntuitiveBrowserMoreHelpCancelPreservesExactState(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	m.Update(tea.PasteMsg{Content: "source draft"})
	m.Update(keyPress(tea.KeyLeft, tea.ModShift))
	column, selection := m.composer.Column(), m.composer.SelectedText()
	m.viewport.SetContent(strings.Repeat("source transcript\n", 60))
	m.viewport.SetYOffset(9)
	m.follow = false
	p, result, _ := intuitivePrompt(t, browserQuestion("a"))
	m.openPrompt(p)
	d := m.dialog
	d.list.SetFilterText("Entry")
	d.list.SetFilterState(list.Filtering)
	d.list.FilterInput.SetCursor(2)
	d.filterQuery = "Entry"
	d.list.Select(1)
	d.actionFocus = len(m.visibleBrowserControls())
	m.Update(keyPress(tea.KeyEnter))
	more := m.dialog
	if more.local != "browser-more" || m.browserDialog != d {
		t.Fatal("More did not suspend exact browser")
	}
	for _, r := range "Help" {
		m.Update(keyPress(r))
	}
	m.Update(keyPress(tea.KeyLeft))
	caret := more.list.FilterInput.Position()
	m.Update(keyPress(tea.KeyEnter))
	if m.detailTitle != "Help" || m.detail == "" || m.dialog != more {
		t.Fatal("portable Help did not overlay More")
	}
	assertIntuitiveNoReply(t, result)
	m.Update(keyPress(tea.KeyEscape))
	if m.detail != "" || m.dialog != more || more.list.FilterValue() != "Help" || more.list.FilterInput.Position() != caret {
		t.Fatal("Help return changed More query/caret")
	}
	m.Update(keyPress(tea.KeyEscape))
	if m.dialog != d || m.browserDialog != nil || d.list.FilterValue() != "Entry" || d.list.FilterInput.Position() != 2 || d.list.SelectedItem().(choiceItem).id != "opaque-b" || d.actionFocus != len(m.visibleBrowserControls()) {
		t.Fatal("More cancellation lost exact browser state")
	}
	if m.currentDraft() != "source draft" || m.composer.Column() != column || m.composer.SelectedText() != selection || m.viewport.YOffset() != 9 || m.follow {
		t.Fatal("local navigation changed retained source editor/reading anchor")
	}
	assertIntuitiveNoReply(t, result)
}

func TestIntuitiveBrowserMoreOfferedActionsAndExactSource(t *testing.T) {
	for _, action := range []string{"refresh", "search", "types", "parent", "root", "groups", "toggle", "expand", "collapse", "future:opaque/ID"} {
		t.Run(action, func(t *testing.T) {
			m := testModel(t)
			q := browserQuestion("a")
			q.BrowserPage.Actions = append(q.BrowserPage.Actions, action)
			switch action {
			case "parent":
				q.Choices[1].Description = "Subagent · parent: Entry A"
			case "toggle", "expand":
				q.Choices[1].Label = "▸ Entry B"
			case "collapse":
				q.Choices[1].Label = "▾ Entry B"
			}
			p, result, _ := intuitivePrompt(t, q)
			m.openPrompt(p)
			source := m.dialog
			source.list.Select(1)
			m.openBrowserMore()
			item := m.dialog.list.Items()[0].(choiceItem)
			if item.id != action || item.command != "browser-more-action" {
				t.Fatalf("offered ID was changed: %#v", item)
			}
			m.Update(keyPress(tea.KeyEnter))
			awaitIntuitiveReply(t, result)
			if m.dialog != nil || m.browserDialog != nil || m.browserStates["a"].index != 1 || source.list.SelectedItem().(choiceItem).id != "opaque-b" {
				t.Fatal("secondary action did not restore/dispatch original source")
			}
		})
	}
	for _, mismatch := range []bool{false, true} {
		t.Run(fmt.Sprintf("changed-source=%t", mismatch), func(t *testing.T) {
			m := testModel(t)
			q := browserQuestion("a")
			q.BrowserPage.Actions = append(q.BrowserPage.Actions, "refresh")
			p, result, _ := intuitivePrompt(t, q)
			m.openPrompt(p)
			m.openBrowserMore()
			if mismatch {
				m.browserDialog = &dialog{prompt: retiredPrompt(6401, browserQuestion("b"))}
			} else {
				p.Question.BrowserPage.Actions = slices.DeleteFunc(p.Question.BrowserPage.Actions, func(id string) bool { return id == "refresh" })
			}
			m.Update(keyPress(tea.KeyEnter))
			assertIntuitiveNoReply(t, result)
			if m.notice == "" {
				t.Fatal("stale source/action was not reported")
			}
		})
	}
}

func TestIntuitiveBrowserMoreStalePromptRetiresHelp(t *testing.T) {
	m := testModel(t)
	p, result, cancel := intuitivePrompt(t, browserQuestion("a"))
	m.openPrompt(p)
	m.openBrowserMore()
	m.Update(keyPress(tea.KeyEnter)) // Help is the only secondary control.
	cancel()
	select {
	case <-p.Done:
	case <-time.After(2 * time.Second):
		t.Fatal("local prompt did not retire")
	}
	m.Update(promptClosedMsg{generation: m.generation, id: p.ID})
	if m.dialog != nil || m.browserDialog != nil || m.detail != "" {
		t.Fatal("retired browser left More/Help active")
	}
	select {
	case <-result:
	case <-time.After(2 * time.Second):
		t.Fatal("retired local scope command did not finish")
	}
}

func TestIntuitiveBrowserReplacementRetiresSuspendedPrompt(t *testing.T) {
	m := testModel(t)
	p, result, _ := intuitivePrompt(t, browserQuestion("a"))
	m.openPrompt(p)
	m.openBrowserMore()
	m.Update(keyPress(tea.KeyEnter)) // local Help
	m.session = &client.Session{}    // no command is run and no connection is made
	if cmd := m.executeBrowser("env", ""); cmd == nil || m.dialog != nil || m.browserDialog != nil || m.detail != "" {
		t.Fatal("replacement did not retire exact suspended browser/Help")
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("old prompt did not receive local cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("replacement left original broker busy")
	}
}

func TestIntuitiveLaunchFocusedCreateVisibleWithFeedback(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	m.openPrompt(retiredPrompt(6403, launchQuestion(client.LaunchInput{Title: "title", Message: "message"})))
	m.problem = "Draft save failed"
	m.Update(keyPress(tea.KeyTab))
	m.Update(keyPress(tea.KeyEnter))
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "[Create]") || !strings.Contains(view, "Draft save failed") || !strings.Contains(view, "Ctrl+S") {
		t.Fatalf("focused control/feedback/footer was clipped:\n%s", view)
	}
}

func TestIntuitiveLaunchTitleEnterFocusesCreateWithoutEffects(t *testing.T) {
	m := testModel(t)
	input := client.LaunchInput{Title: "exact\ttitle", Message: "\tfirst\r\n�"}
	q := launchQuestion(input)
	var saved []client.LaunchInput
	q.LaunchReview.StoreDraft = func(input client.LaunchInput) error { saved = append(saved, input); return nil }
	p, result, _ := intuitivePrompt(t, q)
	m.openPrompt(p)
	m.restoreFocus()
	if m.modalInput.Placeholder != "" {
		t.Fatal("field label still repeats as a placeholder")
	}
	if !slices.Equal(m.launchActions(), []string{"create", "cancel", "model", "effort", "tier", "editor"}) {
		t.Fatal("primary launch actions must precede optional settings")
	}
	m.Update(keyPress(tea.KeyTab))
	m.Update(keyPress(tea.KeyEnter))
	if m.dialog == nil || m.dialog.launch.focus != 2+slices.Index(m.launchActions(), "create") || len(saved) != 0 || m.launchInput() != input {
		t.Fatal("Title Enter dispatched/saved/changed input instead of focusing Create")
	}
	assertIntuitiveNoReply(t, result)
	m.Update(keyPress(tea.KeyEnter))
	awaitIntuitiveReply(t, result)
	if m.dialog != nil || len(saved) != 1 || saved[0] != input || p.Respond(client.Answer{Action: "create"}) {
		t.Fatal("explicit Create did not save/respond exactly once")
	}
	m.chooseDialog()
	if len(saved) != 1 {
		t.Fatal("repeated activation saved the completed review again")
	}
	q = launchQuestion(client.LaunchInput{Title: "keep title"})
	q.LaunchReview.Actions = []string{"model", "cancel"}
	m.openPrompt(retiredPrompt(6402, q))
	m.Update(keyPress(tea.KeyTab))
	m.Update(keyPress(tea.KeyEnter))
	if m.dialog == nil || m.dialog.launch.focus != 1 || !m.launchTitle.Focused() || m.launchInput().Title != "keep title" {
		t.Fatal("Title Enter moved/dispatched without an offered Create")
	}
}
