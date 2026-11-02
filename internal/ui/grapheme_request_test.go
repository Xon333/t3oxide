package ui

import (
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestGraphemeMotionSelectionDeletion(t *testing.T) {
	for _, cluster := range []string{"e\u0301", "👨‍👩‍👧‍👦", "🇦🇹", "👍🏽"} {
		t.Run(cluster, func(t *testing.T) {
			e := textarea.New()
			e.Focus()
			e.SetValue(cluster + "x")
			e.CursorStart()
			updateGraphemeEditor(&e, tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModShift})
			if e.SelectedText() != cluster {
				t.Fatalf("selection %q", e.SelectedText())
			}
			updateGraphemeEditor(&e, tea.KeyPressMsg{Code: tea.KeyBackspace})
			if e.Value() != "x" {
				t.Fatalf("selection deletion %q", e.Value())
			}
			e.SetValue(cluster + "x")
			e.CursorStart()
			updateGraphemeEditor(&e, tea.KeyPressMsg{Code: tea.KeyDelete})
			if e.Value() != "x" {
				t.Fatalf("forward deletion %q", e.Value())
			}
			e.SetValue(cluster)
			e.CursorEnd()
			updateGraphemeEditor(&e, tea.KeyPressMsg{Code: tea.KeyBackspace})
			if e.Value() != "" {
				t.Fatalf("backward deletion %q", e.Value())
			}
			e.SetValue(cluster)
			e.CursorStart()
			updateGraphemeEditor(&e, tea.KeyPressMsg{Code: tea.KeyRight})
			if e.Column() != utf8.RuneCountInString(cluster) {
				t.Fatal("partial caret")
			}
		})
	}
}

func TestRequestExactRecoveryStaysLocalAndRejectsStaleCaret(t *testing.T) {
	m := testModel(t)
	p, reply, _ := intuitivePrompt(t, client.Question{Kind: client.EnterText, RequestQuestions: []client.RequestQuestion{{ID: "answer", Required: true, InitialAnswer: "ab"}}})
	m.openPrompt(p)
	m.restoreFocus()
	m.modalInput.SetCursorColumn(1)
	raw := "\t\r\n e\u0301 "
	if !m.rejectInput(tea.PasteMsg{Content: raw}, "Answer") {
		t.Fatal("expected exact recovery")
	}
	if !m.stageRequestExactInput() {
		t.Fatal("not staged")
	}
	want := "a" + raw + "b"
	if m.dialog.preservedInitial != want {
		t.Fatalf("changed bytes: %q", m.dialog.preservedInitial)
	}
	if !m.saveRequestAnswer() {
		t.Fatal("save failed")
	}
	if got := m.dialog.requestForm.answers["answer"]; got != want {
		t.Fatalf("staged %q", got)
	}
	select {
	case <-reply:
		t.Fatal("staging sent answer")
	default:
	}
	m.advanceRequest(false)
	<-reply // The local broker completes; its env command intentionally has no environment.

	m = testModel(t)
	p, _, _ = intuitivePrompt(t, client.Question{Kind: client.EnterText, RequestQuestions: []client.RequestQuestion{{ID: "a", InitialAnswer: "ab"}}})
	m.openPrompt(p)
	m.restoreFocus()
	if !m.retainRequestExactInput("\t") {
		t.Fatal("not retained")
	}
	m.modalInput.SetCursorColumn(0)
	if m.stageRequestExactInput() {
		t.Fatal("stale caret accepted")
	}
	if m.retainRequestExactInput(strings.Repeat("x", maxInputBytes+1)) {
		t.Fatal("oversize retained")
	}
}

func TestPartialGraphemeSelectionExpandsAndPointerSnaps(t *testing.T) {
	e := textarea.New()
	e.Focus()
	e.SetWidth(30)
	e.SetValue("Ae\u0301Z")
	// Simulate upstream rune-based selection ending inside the accent cluster.
	e.CursorStart()
	editorKey(&e, tea.KeyRight, 0)
	editorKey(&e, tea.KeyRight, tea.ModShift)
	normalizeEditorSelection(&e)
	if e.SelectedText() != "e\u0301" {
		t.Fatalf("partial selection: %q", e.SelectedText())
	}
	p := snapGrapheme(&e, textarea.Position{Row: 0, Col: 2}, false)
	if p.Col != 1 {
		t.Fatalf("pointer start: %+v", p)
	}
	end := snapGrapheme(&e, textarea.Position{Row: 0, Col: 2}, true)
	selectEditorRange(&e, p, end)
	if e.SelectedText() != "e\u0301" {
		t.Fatalf("drag selection: %q", e.SelectedText())
	}
	selectEditorRange(&e, end, p)
	if e.SelectedText() != "e\u0301" {
		t.Fatalf("reverse drag: %q", e.SelectedText())
	}
}
