package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/charmbracelet/x/ansi"
)

func TestWorkspaceComposerCursorKeepsVisibleCapAndExactDraft(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 32})
	draft := strings.Repeat("unchanged draft 界\n", 9) + "last line"
	m.Update(tea.PasteMsg{Content: draft})
	for _, k := range []tea.KeyPressMsg{keyPress(tea.KeyUp), keyPress(tea.KeyLeft), keyPress(tea.KeyHome), keyPress(tea.KeyEnd), keyPress(tea.KeyDown)} {
		m.Update(k)
		if m.composer.Height() > 5 || m.composer.Value() != draft {
			t.Fatalf("cursor %s changed display cap/draft: height=%d", k.String(), m.composer.Height())
		}
		view := ansi.Strip(m.View().Content)
		if !strings.Contains(view, "F3") || !strings.Contains(view, "Stop/hold") || len(strings.Split(view, "\n")) != 32 {
			t.Fatal("cursor movement clipped workspace controls")
		}
	}
}

func TestWorkspaceHelpPreservesRequestAndBlocksHiddenEditing(t *testing.T) {
	for _, kind := range []client.Kind{client.EnterText, client.ChooseOne} {
		m := testModel(t)
		m.composer.SetValue("human draft")
		p := retiredPrompt(900, client.Question{Kind: kind, Title: "Exact request", Initial: "answer", Choices: []client.Choice{{ID: "exact", Label: "Answer"}}})
		m.openPrompt(p)
		m.restoreFocus()
		if kind == client.ChooseOne {
			m.dialogKey(keyPress('a'))
		}
		answer, query := m.modalInput.Value(), m.dialog.list.FilterValue()
		m.Update(keyPress(tea.KeyF3))
		if m.detail == "" || m.dialog == nil || m.dialog.prompt != p {
			t.Fatal("context help replaced request")
		}
		m.Update(tea.PasteMsg{Content: "hidden paste"})
		m.Update(keyPress('x'))
		m.Update(keyPress(tea.KeyEnter))
		if m.modalInput.Value() != answer || m.dialog.list.FilterValue() != query || m.composer.Value() != "human draft" {
			t.Fatal("help allowed editing or answered hidden request")
		}
		m.Update(keyPress(tea.KeyEscape))
		if m.detail != "" || m.dialog == nil || m.dialog.prompt != p {
			t.Fatal("Escape did not return to exact request")
		}
		if kind == client.EnterText && !m.modalInput.Focused() || kind == client.ChooseOne && !m.dialog.list.FilterInput.Focused() {
			t.Fatal("request focus not restored")
		}
	}
}

func TestWorkspaceFilteredMultipleChoiceRetainsCompletionHint(t *testing.T) {
	for _, width := range []int{28, 44, 90} {
		m := testModel(t)
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		m.openPrompt(retiredPrompt(901, client.Question{Kind: client.ChooseMany, Title: "Select answers", Prompt: strings.Repeat("consequence ", 30), Choices: []client.Choice{{ID: "keep", Label: "Keep"}}}))
		m.dialogKey(keyPress('k'))
		if m.dialog.list.FilterValue() != "k" {
			t.Fatal("first k did not start a query")
		}
		if !strings.Contains(ansi.Strip(m.View().Content), "continue") {
			t.Fatalf("width %d hides completion for filtered multiple choice", width)
		}
		if m.View().BackgroundColor == nil || m.View().ForegroundColor == nil {
			t.Fatal("native terminal color ownership missing")
		}
	}
}
