package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/charmbracelet/x/ansi"
)

func TestTopLevelRouterPreservesFocusedEditorKeys(t *testing.T) {
	m := testModel(t)
	finishCommand(t, m, m.dispatch("keyboard", "legacy"))
	m.Update(tea.PasteMsg{Content: "first\nsecond"})
	m.Update(keyPress('a', tea.ModCtrl))
	m.Update(keyPress('e', tea.ModCtrl))
	m.Update(keyPress('!'))
	if m.composer.Value() != "first\nsecond!" || m.dialog != nil || m.actionBusy || m.problem != "" {
		t.Fatal("Ctrl+E escaped the focused editor")
	}
	m.Update(keyPress('p', tea.ModCtrl))
	if m.composer.Line() != 0 || m.dialog != nil {
		t.Fatal("Ctrl+P did not reach native previous-line editing")
	}
	m.composer.SetValue("abcd")
	m.composer.CursorEnd()
	m.Update(keyPress('b', tea.ModCtrl))
	m.Update(keyPress('d', tea.ModCtrl))
	if m.composer.Value() != "abc" || m.closed || m.settingBusy {
		t.Fatal("Ctrl+B/Ctrl+D were intercepted as application actions")
	}
	m.Update(keyPress('a', tea.ModCtrl))
	m.Update(keyPress('f', tea.ModCtrl))
	m.Update(keyPress('!'))
	if m.composer.Value() != "a!bc" || m.searching {
		t.Fatal("Ctrl+F did not reach native character movement")
	}
	m.composer.SetValue("ab")
	m.composer.CursorEnd()
	m.Update(keyPress('t', tea.ModCtrl))
	if m.composer.Value() != "ba" || m.actionBusy || m.dialog != nil {
		t.Fatal("Ctrl+T did not reach native transpose")
	}
	m.composer.SetValue("alpha beta gamma")
	m.composer.CursorEnd()
	m.Update(keyPress(tea.KeyLeft, tea.ModAlt))
	m.Update(keyPress('|'))
	if m.composer.Value() != "alpha beta |gamma" || m.problem != "" {
		t.Fatal("decoded Alt+Left escaped word movement")
	}
}

func TestExactDraftAcknowledgmentRestoresImmediateTyping(t *testing.T) {
	m := testModel(t)
	text := "\tcode\r\n�"
	m.restoreDraft(text)
	m.restoreFocus()
	if m.composer.Focused() {
		t.Fatal("exact draft was passed to a normalizing editor")
	}
	m.Update(sessionMsg{generation: m.generation, event: client.Event{Revision: 1, DraftAccepted: &text}})
	m.Update(keyPress('x'))
	if m.currentDraft() != "x" || !m.composer.Focused() || m.problem != "" {
		t.Fatal("exact ACK left composer disabled or retained stale input feedback")
	}
}

func TestTopLevelPaletteAndContextHelpPreserveQuery(t *testing.T) {
	m := testModel(t)
	m.Update(keyPress(tea.KeyF3))
	for _, r := range "keyboard" {
		m.Update(keyPress(r))
	}
	d := m.dialog
	m.Update(keyPress('p', tea.ModCtrl))
	m.Update(keyPress(tea.KeyF3))
	if m.dialog != d || d.list.FilterValue() != "keyboard" {
		t.Fatal("reentry/up reset the palette filter")
	}
	m.Update(keyPress(tea.KeyF1))
	m.Update(keyPress(tea.KeyEscape))
	if m.dialog != d || d.list.FilterValue() != "keyboard" || !d.list.FilterInput.Focused() {
		t.Fatal("context help lost the original list editor")
	}
	m.cancelDialog()
	p, result, _ := intuitivePrompt(t, client.Question{Kind: client.EnterText, Initial: "first\nsecond"})
	m.openPrompt(p)
	m.restoreFocus()
	m.Update(keyPress('a', tea.ModCtrl))
	m.Update(keyPress('e', tea.ModCtrl))
	m.Update(keyPress('!'))
	if m.modalInput.Value() != "first\nsecond!" || m.problem != "" {
		t.Fatal("modal Ctrl+E escaped native editing")
	}
	m.Update(keyPress(tea.KeyF3))
	if m.dialog == nil || m.dialog.local != "palette" || m.dialog.paletteSource == nil || m.dialog.paletteSource.prompt != p {
		t.Fatal("request commands lost their live source")
	}
	m.Update(keyPress(tea.KeyEscape))
	if m.dialog == nil || m.dialog.prompt != p || m.modalInput.Value() != "first\nsecond!" || !m.modalInput.Focused() {
		t.Fatal("request help lost the exact answer")
	}
	assertIntuitiveNoReply(t, result)
}

func TestSlashCompletionAcceptsBeforeExecutionAndKeepsInvalidArguments(t *testing.T) {
	m := testModel(t)
	m.Update(tea.PasteMsg{Content: "/hel"})
	if !m.completionVisible() || m.dialog != nil {
		t.Fatal("slash completion did not preserve composer focus")
	}
	m.Update(keyPress(tea.KeyTab))
	if m.currentDraft() != "/help " || m.detail != "" {
		t.Fatal("accepting partial completion executed it")
	}
	m.Update(keyPress(']', tea.ModCtrl))
	if m.detail == "" || m.currentDraft() != "" {
		t.Fatal("exact command did not dispatch after explicit acceptance")
	}
	m.Update(keyPress(tea.KeyEscape))
	m.Update(tea.PasteMsg{Content: "/mouse invalid"})
	m.Update(keyPress(']', tea.ModCtrl))
	if m.currentDraft() != "/mouse invalid" || m.settingBusy || !strings.Contains(m.problem, "Use /mouse") && !strings.Contains(m.problem, "use /mouse") {
		t.Fatal("invalid local arguments lost editable command")
	}
	m.composer.SetValue("/Users/example/file.go")
	m.refreshCompletion()
	if m.isSlashCommand(m.currentDraft()) || m.completionVisible() {
		t.Fatal("an absolute path was interpreted as a command")
	}
	m.composer.SetValue("/help")
	m.Update(keyPress(tea.KeyEnter, tea.ModCtrl))
	if m.detail != "" || m.currentDraft() != "/help" || m.problem != "Choose a conversation first" {
		t.Fatal("literal-send shortcut executed a known slash command")
	}
}

func TestSlashDismissalAndCompletionStayBounded(t *testing.T) {
	for _, size := range [][2]int{{60, 16}, {80, 24}, {120, 35}} {
		m := testModel(t)
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.Update(keyPress('/'))
		view := ansi.Strip(m.View().Content)
		if !m.completionVisible() || !strings.Contains(view, "complete") || !strings.Contains(view, "send text") || !strings.Contains(view, "Stop/hold") {
			t.Fatalf("completion hid essential controls at %v:\n%s", size, view)
		}
		m.Update(keyPress(tea.KeyEscape))
		if m.currentDraft() != "/" || m.completionVisible() {
			t.Fatal("Escape discarded slash text or retained popup")
		}
		m.Update(keyPress('h'))
		if !m.completionVisible() {
			t.Fatal("changing the command token did not reopen suggestions")
		}
	}
}

func TestCommandResultClearsOnlyItsExactSuccessfulDraft(t *testing.T) {
	for _, tc := range []struct {
		text string
		err  error
		want string
	}{
		{"/delivery invalid", errors.New("rejected"), "/delivery invalid"},
		{"/delivery queue", nil, ""},
		{"new human draft", nil, "new human draft"},
	} {
		m := testModel(t)
		m.composer.SetValue(tc.text)
		m.actionID = 4
		m.Update(actionMsg{generation: m.generation, id: 4, commandDraft: "/delivery queue", err: tc.err})
		if m.currentDraft() != tc.want {
			t.Fatalf("command result changed an unrelated or rejected draft: %q", m.currentDraft())
		}
	}
}
