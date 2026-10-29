package ui

import (
	"fmt"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/wire"
	"github.com/charmbracelet/x/ansi"
)

func browserQuestion(profile string) client.Question {
	return client.Question{Surface: client.SurfaceBrowser, Kind: client.ChooseOne, Title: "Conversations", Navigation: true,
		Choices:     []client.Choice{{ID: "opaque-a", Label: "Entry A"}, {ID: "opaque-b", Label: "Entry B"}},
		BrowserPage: &client.BrowserPage{ProfileIDs: []string{"a", "b"}, ProfileID: profile, Location: "Environment " + profile, Profiles: []client.Choice{{ID: "a", Label: "Alpha"}, {ID: "b", Label: "Beta"}}, Actions: []string{"environments", "new", "settings", "resume", "profile:a", "profile:b"}, Groups: []client.BrowserGroup{{ID: "project-id", Label: "Project", ChoiceIDs: []string{"opaque-a", "opaque-b"}}}}}
}

func launchQuestion(input client.LaunchInput) client.Question {
	return client.Question{Surface: client.SurfaceLaunch, Kind: client.EnterText, Title: "New conversation", LaunchReview: &client.LaunchReview{Input: input, Location: client.Location{Environment: "Alpha", ProjectID: "project-id", Workspace: "Existing workspace"}, Summary: "model-a · high effort · Standard", Actions: []string{"create", "cancel", "model", "effort", "tier"}}}
}

func TestRefinementFilteredRowSelectionUsesNativeCursor(t *testing.T) {
	m := testModel(t)
	m.openPrompt(retiredPrompt(6100, browserQuestion("a")))
	m.restoreFocus()
	for _, r := range "Entry" {
		m.Update(keyPress(r))
	}
	m.Update(keyPress(tea.KeyDown))
	if m.dialog.list.SelectedItem().(choiceItem).id != "opaque-b" {
		t.Fatal("Down did not move the native result")
	}
	for range 4 {
		view := ansi.Strip(m.dialog.list.View())
		var marked string
		for _, line := range strings.Split(view, "\n") {
			if strings.Contains(line, "│") {
				marked += line
			}
		}
		if !strings.Contains(marked, "Entry B") || strings.Contains(marked, "Entry A") {
			t.Fatalf("selected style disagrees with cursor:\n%s", view)
		}
		m.View()
		if m.dialog.list.Index() != 1 || strings.TrimSpace(ansi.Strip(m.pickerSelectionView(60))) != "" || !strings.Contains(m.dialog.detailText, "Entry B") {
			t.Fatal("render changed selected row/detail identity or repeated its complete unique title")
		}
	}
	m.Update(keyPress(tea.KeyEnter))
	if m.dialog != nil || !strings.Contains(m.notice, "retired") {
		t.Fatal("Enter did not target the current exact prompt")
	}
}

func TestRefinementBrowserPreservesComposerSelectionAndReadingAnchor(t *testing.T) {
	for _, size := range [][2]int{{60, 16}, {80, 24}, {120, 35}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := testModel(t)
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m.Update(tea.PasteMsg{Content: "exact source draft\nsecond line"})
			m.Update(keyPress(tea.KeyLeft, tea.ModShift))
			line, col, selection := m.composer.Line(), m.composer.Column(), m.composer.SelectedText()
			m.transcript = strings.Repeat("PRIVATE TRANSCRIPT\n", 60)
			m.viewport.SetContent(m.transcript)
			m.follow = false
			m.viewport.SetYOffset(9)
			anchor := m.viewport.YOffset()
			m.openPrompt(retiredPrompt(6101, browserQuestion("a")))
			m.restoreFocus()
			view := ansi.Strip(m.View().Content)
			for _, want := range []string{"Conversations", "Environments", "New", "Settings", "Resume", "Project"} {
				if !strings.Contains(view, want) {
					t.Fatalf("missing %q:\n%s", want, view)
				}
			}
			if strings.Contains(view, "PRIVATE TRANSCRIPT") || strings.Contains(view, "exact source draft") || strings.Contains(view, "╭─ Message") {
				t.Fatal("conversation leaked behind browser")
			}
			m.Update(keyPress(tea.KeyEscape))
			if m.browserPage || m.composer.Value() != "exact source draft\nsecond line" || m.composer.Line() != line || m.composer.Column() != col || m.composer.SelectedText() != selection || m.viewport.YOffset() != anchor || m.follow {
				t.Fatal("browser dismissal changed live editor or reading anchor")
			}
		})
	}
}

func TestRefinementBrowserTabQueriesKeepCaretAndOfferedSelection(t *testing.T) {
	m := testModel(t)
	m.openPrompt(retiredPrompt(6102, browserQuestion("a")))
	m.restoreFocus()
	for _, r := range "Entry" {
		m.Update(keyPress(r))
	}
	m.Update(keyPress(tea.KeyDown))
	m.Update(keyPress(tea.KeyLeft))
	m.browserAction("profile:b")
	m.openPrompt(retiredPrompt(6103, browserQuestion("b")))
	m.restoreFocus()
	m.Update(keyPress('B'))
	m.browserAction("profile:a")
	m.openPrompt(retiredPrompt(6104, browserQuestion("a")))
	m.restoreFocus()
	if m.dialog.list.SelectedItem().(choiceItem).id != "opaque-b" || m.dialog.list.FilterValue() != "Entry" {
		t.Fatal("tab return lost native query/result")
	}
	m.Update(keyPress('X'))
	if m.dialog.list.FilterValue() != "EntrXy" {
		t.Fatalf("query caret moved: %q", m.dialog.list.FilterValue())
	}
	m.saveBrowserState(m.dialog)
	q := browserQuestion("a")
	q.BrowserPage.Query = "Entry B"
	m.openPrompt(retiredPrompt(6113, q))
	m.restoreFocus()
	if m.dialog.list.FilterValue() != "Entry B" || m.dialog.list.SelectedItem().(choiceItem).id != "opaque-b" {
		t.Fatal("saved local query overrode explicit host search")
	}
	m.Update(keyPress(tea.KeyTab))
	m.Update(keyPress('l', tea.ModCtrl))
	if !m.dialog.details {
		t.Fatal("action focus blocked Details")
	}
	m.Update(keyPress(tea.KeyEscape))
	m.Update(keyPress(tea.KeyEscape))
	if m.dialog != nil {
		t.Fatal("action focus blocked Escape")
	}
}

func TestRefinementBrowserScopeHiddenChecksAndLocalSettingsRestore(t *testing.T) {
	m := testModel(t)
	q := client.Question{Surface: client.SurfaceBrowser, Kind: client.ChooseMany, Title: "Environments", Choices: []client.Choice{{ID: "a", Label: "Alpha"}, {ID: "b", Label: "Beta"}}, BrowserPage: &client.BrowserPage{ProfileIDs: []string{"a"}}}
	m.openPrompt(retiredPrompt(6105, q))
	m.restoreFocus()
	for _, r := range "Beta" {
		m.Update(keyPress(r))
	}
	m.Update(keyPress(tea.KeyEnter))
	if !m.dialog.selected["a"] || !m.dialog.selected["b"] {
		t.Fatal("filtering lost a hidden checked environment")
	}
	m.Update(keyPress(tea.KeyTab))
	m.Update(tea.PasteMsg{Content: "hidden query edit"})
	if m.dialog.list.FilterValue() != "Beta" {
		t.Fatal("Continue focus edited query")
	}
	m.openPrompt(retiredPrompt(6106, browserQuestion("a")))
	m.restoreFocus()
	m.Update(keyPress('E'))
	d := m.dialog
	m.browserAction("settings")
	m.Update(keyPress(tea.KeyEnter)) // local Appearance
	if m.dialog.local != "theme" || m.browserDialog != d {
		t.Fatal("appearance discarded exact browser prompt")
	}
	m.Update(keyPress(tea.KeyEscape))
	if m.dialog != d || m.dialog.list.FilterValue() != "E" || !m.browserPage {
		t.Fatal("local appearance cancel lost browser/query")
	}
}

func TestRefinementPortableActionsAndNativeSelectAll(t *testing.T) {
	m := testModel(t)
	m.Update(tea.PasteMsg{Content: "human text"})
	m.Update(keyPress('g', tea.ModCtrl))
	if m.composer.SelectedText() != "human text" || m.dialog != nil {
		t.Fatal("Ctrl+G was stolen from native SelectAll")
	}
	m.Update(keyPress('x'))
	if m.currentDraft() != "x" {
		t.Fatal("native selected edit failed")
	}
	for _, r := range []rune{'e', 'm', 's'} {
		m.Update(keyPress(r, tea.ModAlt))
		if m.dialog != nil || m.actionBusy {
			t.Fatal("Option-letter opened application controls")
		}
	}
	m.Update(keyPress(tea.KeyTab))
	m.Update(keyPress('p', tea.ModCtrl))
	if m.dialog == nil || m.dialog.local != "palette" {
		t.Fatal("portable Tab/Ctrl+P did not open Commands")
	}
	for _, name := range []string{"new", "settings", "edit", "recall", "error", "details"} {
		if _, ok := m.commandSpec(name); !ok {
			t.Fatalf("missing non-F command %s", name)
		}
	}
	m.Update(keyPress('l', tea.ModCtrl))
	if !m.dialog.details {
		t.Fatal("non-F Details route missing")
	}
}

func TestRefinementLaunchFocusLiteralTitleAndCanonicalRoundTrip(t *testing.T) {
	m := testModel(t)
	m.composer.SetValue("source exact draft")
	input := client.LaunchInput{Title: "Human title", Message: "First message"}
	m.openPrompt(retiredPrompt(6107, launchQuestion(input)))
	m.restoreFocus()
	m.Update(keyPress(tea.KeyEnter))
	m.Update(keyPress('!'))
	if m.launchInput().Message != "First message\n!" {
		t.Fatalf("Enter did not insert first-message newline: %q", m.launchInput().Message)
	}
	m.Update(keyPress(tea.KeyTab))
	if !m.launchTitle.Focused() || m.modalInput.Focused() {
		t.Fatal("Tab failed to focus one-line title")
	}
	m.Update(keyPress('a', tea.ModCtrl))
	m.Update(keyPress('k', tea.ModCtrl))
	m.Update(tea.PasteMsg{Content: "/model"})
	if m.launchInput().Title != "/model" || m.actionBusy {
		t.Fatal("title syntax became a command")
	}
	preserved := m.launchInput()
	m.Update(keyPress(tea.KeyTab)) // Create
	m.Update(keyPress(tea.KeyTab)) // Cancel
	m.Update(keyPress(tea.KeyTab)) // Model
	if m.dialog.launch.focus != 4 {
		t.Fatal("launch controls are not Tab reachable")
	}
	m.Update(keyPress(tea.KeyEnter))
	if m.dialog != nil || m.currentDraft() != "source exact draft" || m.actionID != 0 {
		t.Fatal("launch model action changed source attachment/action")
	}
	m.openPrompt(retiredPrompt(6108, launchQuestion(preserved)))
	m.restoreFocus()
	if m.launchInput() != preserved {
		t.Fatal("settings return lost form values")
	}
	canonical := client.LaunchInput{Title: "exact\ttitle", Message: "\tcode\r\n�"}
	m.openPrompt(retiredPrompt(6109, launchQuestion(canonical)))
	m.restoreFocus()
	m.Update(tea.PasteMsg{Content: "must not replace canonical"})
	if m.launchInput() != canonical {
		t.Fatal("normalizing editor changed canonical launch bytes")
	}
	m.Update(keyPress('l', tea.ModCtrl))
	if !strings.Contains(m.dialog.detailText, safeText(canonical.Message)) || !strings.Contains(m.dialog.detailText, "Existing workspace") {
		t.Fatal("launch Details lost canonical message/workspace")
	}
}

func TestRefinementLaunchEditorReturnPreservesBothValuesAndRejectsStale(t *testing.T) {
	m := testModel(t)
	m.composer.SetValue("source draft")
	m.openPrompt(retiredPrompt(6110, launchQuestion(client.LaunchInput{Title: "retained title", Message: "original"})))
	path, err := writeEditorFile("\tnew\r\n�")
	if err != nil {
		t.Fatal(err)
	}
	finishCommand(t, m, m.finishEditor(editorFinishedMsg{generation: m.generation, launchSerial: m.dialog.serial, path: path, original: "original", text: "\tnew\r\n�", read: true}))
	if m.launchInput() != (client.LaunchInput{Title: "retained title", Message: "\tnew\r\n�"}) || m.currentDraft() != "source draft" {
		t.Fatal("external editor changed title or source draft")
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("consumed editor file was not removed")
	}
	path, err = writeEditorFile("unique stale")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	m.finishEditor(editorFinishedMsg{generation: m.generation, launchSerial: m.dialog.serial + 1, path: path, original: "original", text: "unique stale", read: true})
	if m.launchInput().Message != "\tnew\r\n�" {
		t.Fatal("stale editor overwrote current launch")
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatal("unique stale edit was discarded")
	}
}

func TestRefinementTypedStatusLateAckAndNewInput(t *testing.T) {
	m := testModel(t)
	m.actionID = 4
	m.actionBusy = true
	m.actionName = "send"
	m.composer.SetValue("new human input")
	m.Update(sessionMsg{generation: m.generation, event: client.Event{Revision: 2, Status: client.Status{ConnectionState: "synchronized", RunState: "completed", Requests: 2, ModelName: "model-a · high", Receipt: &client.Receipt{OperationID: "operation-a", Phase: "completed"}}}})
	m.Update(actionMsg{generation: m.generation, id: 4, name: "send", result: client.Result{Text: "T3 acknowledged the message"}})
	view := ansi.Strip(m.View().Content)
	if strings.Contains(view, "T3 acknowledged") || !strings.Contains(view, "completed") || !strings.Contains(view, "Awaiting input (2)") || m.currentDraft() != "new human input" {
		t.Fatal("late ACK revived transient feedback or cleared requests/new input")
	}
	m.Update(sessionMsg{generation: m.generation, event: client.Event{Revision: 1, Status: client.Status{RunState: "running", Receipt: &client.Receipt{Phase: "acknowledged"}}}})
	if m.status.RunState != "completed" {
		t.Fatal("stale event revived older status")
	}
	m.actionBusy = true
	m.actionName = "model"
	if !strings.Contains(ansi.Strip(m.View().Content), "completed") {
		t.Fatal("local progress replaced authoritative run state")
	}
}

func TestRefinementLaunchCompactLayoutsAndSingleLineInputs(t *testing.T) {
	for _, size := range [][2]int{{60, 16}, {80, 24}, {120, 35}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := testModel(t)
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m.transcript = "PRIVATE SOURCE"
			m.viewport.SetContent(m.transcript)
			m.composer.SetValue("PRIVATE DRAFT")
			m.openPrompt(retiredPrompt(6111, launchQuestion(client.LaunchInput{Title: "Visible title", Message: "Visible first message"})))
			m.restoreFocus()
			view := ansi.Strip(m.View().Content)
			for _, want := range []string{"New conversation", "First message", "Visible title", "Visible first message", "Model", "Effort", "Tier", "Create", "Cancel", "Editor"} {
				if !strings.Contains(view, want) {
					t.Fatalf("missing %q at %v:\n%s", want, size, view)
				}
			}
			if strings.Contains(view, "PRIVATE SOURCE") || strings.Contains(view, "PRIVATE DRAFT") {
				t.Fatal("source conversation leaked behind launch")
			}
			for range 9 {
				m.Update(keyPress(tea.KeyTab))
			}
			if strings.Count(m.View().Content, "\n") != size[1]-1 {
				t.Fatal("full surface height escaped terminal")
			}
		})
	}
	m := testModel(t)
	m.openPrompt(retiredPrompt(6112, client.Question{Kind: client.EnterText, SingleLine: true, Initial: "query"}))
	m.restoreFocus()
	m.Update(keyPress('j', tea.ModCtrl))
	m.Update(tea.PasteMsg{Content: "two\nlines"})
	if m.modalInput.Value() != "query" || m.modalInput.Height() != 1 {
		t.Fatal("single-line prompt accepted normalized/multiline input")
	}
}

func TestRefinementPendingSurfaceCannotEditHiddenComposer(t *testing.T) {
	m := testModel(t)
	m.composer.SetValue("source exact")
	m.browserPage = true
	m.dialog = nil
	m.restoreFocus()
	m.Update(keyPress('x'))
	m.Update(tea.PasteMsg{Content: "hidden edit"})
	if m.currentDraft() != "source exact" || m.composer.Focused() {
		t.Fatal("navigation transition edited hidden composer")
	}
	m.launchPage = true
	m.browserPage = false
	m.Update(keyPress('x'))
	if m.currentDraft() != "source exact" {
		t.Fatal("launch transition edited hidden composer")
	}
}

func TestRefinementBrowserAdmissionPreservesPendingSend(t *testing.T) {
	m := testModel(t)
	m.session = &client.Session{}
	m.status = client.Status{ThreadID: "attached-source", Writable: true}
	m.navigator.focused = false
	m.restoreFocus()
	m.actionBusy = true
	m.actionName = "send"
	m.actionID = 8
	canceled := false
	m.cancelAction = func() { canceled = true }
	m.composer.SetValue("next human draft")
	_, cmd := m.Update(keyPress('o', tea.ModCtrl))
	if canceled || !m.navigator.focused || !m.actionBusy || m.actionID != 8 || m.browserPage {
		t.Fatalf("opening unified navigator changed pending source: cmd=%t canceled=%t focused=%t busy=%t action=%d browser=%t", cmd != nil, canceled, m.navigator.focused, m.actionBusy, m.actionID, m.browserPage)
	}
	m.Update(keyPress('x'))
	if m.currentDraft() != "next human draft" {
		t.Fatal("navigator query edited the hidden composer")
	}
	m.Update(actionMsg{generation: m.generation, id: 8, name: "send"})
	if !m.navigator.focused || m.actionBusy || m.composer.Focused() || canceled {
		t.Fatal("send receipt changed active navigator or focused hidden draft")
	}
	m.Update(keyPress('o', tea.ModCtrl))
	if m.navigator.focused || !m.composer.Focused() || m.currentDraft() != "next human draft" {
		t.Fatal("leaving navigator failed to resume exact draft")
	}
}

func TestRefinementSmallLaunchTabRevealsControlsAndDurableErrorsRemain(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 12})
	m.openPrompt(retiredPrompt(6114, launchQuestion(client.LaunchInput{Message: "first"})))
	m.restoreFocus()
	m.Update(keyPress(tea.KeyTab))
	m.Update(keyPress(tea.KeyTab))
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Create") || !strings.Contains(view, "Cancel") || !strings.Contains(view, "Editor") {
		t.Fatalf("compact form did not scroll controls into view:\n%s", view)
	}
	m.status.PendingAction = true
	m.problem = "Uncertain saved action"
	m.actionID = 7
	m.Update(actionMsg{generation: m.generation, id: 7, name: "latest"})
	if m.problem != "Uncertain saved action" {
		t.Fatal("unrelated successful action erased durable uncertainty")
	}
	m.beginCommandNotice("stop")
	if m.problem != "Uncertain saved action" {
		t.Fatal("Stop admission erased ordinary-lane uncertainty")
	}
}

func TestRefinementStartupBrowserAdoptionRevealsConversationOnlyOnce(t *testing.T) {
	for _, closeFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(closeFirst), func(t *testing.T) {
			m := testModel(t)
			m.startupBrowser = true
			m.openPrompt(retiredPrompt(6300, browserQuestion("b")))
			m.restoreFocus()
			if closeFirst {
				m.Update(promptClosedMsg{generation: m.generation, id: 6300})
			}
			m.revision = 4
			attached := client.Event{Revision: 5, Status: client.Status{ThreadID: "beta", RunState: "running"}, Snapshot: &wire.Snapshot{Metadata: wire.Metadata{ThreadID: "beta", Title: "Beta"}}}
			m.Update(sessionMsg{generation: m.generation - 1, event: attached})
			stale := attached
			stale.Revision = 4
			m.Update(sessionMsg{generation: m.generation, event: stale})
			if !m.browserPage || !m.startupBrowser {
				t.Fatal("stale attachment retired startup browser")
			}
			m.Update(sessionMsg{generation: m.generation, event: attached})
			if m.browserPage || m.launchPage || m.startupBrowser || m.dialog != nil || !m.composer.Focused() {
				t.Fatal("startup adoption left browser/loading over attached conversation")
			}
			view := ansi.Strip(m.View().Content)
			if !strings.Contains(view, "Message") || strings.Contains(view, "Loading…") || strings.Contains(view, "Browser") {
				t.Fatalf("conversation/composer not revealed:\n%s", view)
			}
			m.openPrompt(retiredPrompt(6301, browserQuestion("b")))
			m.browserBusy = true
			m.restoreFocus()
			m.composer.SetValue("exact draft")
			m.composer.CursorStart()
			m.viewport.SetContent(strings.Repeat("live\n", 60))
			m.viewport.SetYOffset(9)
			m.follow = false
			live := attached
			live.Revision = 6
			m.Update(sessionMsg{generation: m.generation, event: live})
			if !m.browserPage || m.dialog == nil || m.dialog.prompt.ID != 6301 || m.composer.Value() != "exact draft" || m.composer.Column() != 0 || m.viewport.YOffset() != 9 || m.follow {
				t.Fatal("live update dismissed explicit browser or changed retained conversation")
			}
		})
	}
}
