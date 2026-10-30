package ui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/charmbracelet/x/ansi"
)

func TestIntuitivePaletteHelpMatchesContextualDispatch(t *testing.T) {
	for _, tc := range []struct {
		name, draft, label     string
		reading, opens, legacy bool
	}{
		{"empty-composer", "", "Ctrl+Space", false, true, false},
		{"nonempty-composer", "first\nsecond", "Ctrl+Space", false, true, false},
		{"reading-with-draft", "first\nsecond", "Ctrl+Space", true, true, false},
		{"legacy-nonempty-composer", "first\nsecond", "Ctrl+Space", false, false, true},
		{"legacy-reading-with-draft", "first\nsecond", "Ctrl+Space", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := testModel(t)
			if tc.legacy {
				finishCommand(t, m, m.dispatch("keyboard", "legacy"))
			}
			m.Update(tea.PasteMsg{Content: tc.draft})
			if tc.reading {
				m.Update(keyPress(tea.KeyTab, tea.ModShift))
			}
			k := m.keyMap()
			if k.Palette.Help().Key != tc.label || key.Matches(keyPress('p', tea.ModCtrl), k.Palette) != tc.opens {
				t.Fatalf("palette help/dispatch disagree: %#v", k.Palette.Help())
			}
			m.Update(keyPress('p', tea.ModCtrl))
			opened := m.dialog != nil && m.dialog.local == "palette"
			if opened != tc.opens || m.currentDraft() != tc.draft {
				t.Fatalf("Ctrl+P changed context/draft: opened=%v draft=%q", opened, m.currentDraft())
			}
			if !tc.opens {
				if m.composer.Line() != 0 {
					t.Fatal("Ctrl+P did not retain native previous-line editing")
				}
				m.Update(keyPress(tea.KeyTab, tea.ModShift))
				m.Update(keyPress('p', tea.ModCtrl))
				if m.dialog == nil || m.dialog.local != "palette" || m.currentDraft() != tc.draft {
					t.Fatal("reading Ctrl+P fallback changed the draft or failed to open commands")
				}
			}
		})
	}
}

func TestIntuitivePaletteAcceptsSlashName(t *testing.T) {
	m := testModel(t)
	m.composer.SetValue("exact retained draft")
	m.Update(keyPress('p', tea.ModCtrl))
	m.Update(tea.PasteMsg{Content: "/pending"})
	row, ok := m.dialog.list.SelectedItem().(choiceItem)
	if !ok || row.command != "pending" || m.dialog.list.FilterValue() != "/pending" || m.currentDraft() != "exact retained draft" {
		t.Fatalf("slash-name palette query changed target/query/draft: %#v", row)
	}
	m.Update(keyPress(tea.KeyEscape))
	if m.dialog != nil || m.currentDraft() != "exact retained draft" {
		t.Fatal("palette query changed the composer on return")
	}
}

func TestIntuitiveReadingAliasesPreserveDraftCaretSelectionAndAnchor(t *testing.T) {
	for _, draft := range []string{"first 界\nsecond? /", "\tcanonical\r\n�\n "} {
		for _, action := range []string{"palette", "help", "search"} {
			t.Run(fmt.Sprintf("%q/%s", draft, action), func(t *testing.T) {
				m, state := draftSessionModel(t)
				m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
				if err := m.storeDraft(draft); err != nil {
					t.Fatal(err)
				}
				if m.preservedDraft == "" {
					m.Update(keyPress(tea.KeyLeft, tea.ModShift))
				}
				line, column, selection := m.composer.Line(), m.composer.Column(), m.composer.SelectedText()
				m.transcript = strings.Repeat("current page only\n", 70)
				m.viewport.SetContent(m.transcript)
				m.viewport.SetYOffset(9)
				m.follow = false
				m.Update(keyPress(tea.KeyTab))
				anchor := m.viewport.YOffset()
				switch action {
				case "palette":
					m.Update(keyPress('p', tea.ModCtrl))
					if m.dialog == nil || m.dialog.local != "palette" {
						t.Fatal("reading Ctrl+P did not open commands")
					}
				case "help":
					m.Update(keyPress('?'))
					if m.detail == "" || !strings.Contains(m.detail, "COMMANDS") {
						t.Fatal("reading ? did not dispatch existing Help")
					}
				case "search":
					m.Update(keyPress('/'))
					if !m.searching || m.searchInput.Value() != "" || m.actionBusy || m.browserBusy {
						t.Fatal("reading / did not open local current-page Search")
					}
				}
				m.Update(keyPress(tea.KeyEscape))
				if !m.transcriptFocus || m.dialog != nil || m.detail != "" || m.searching || m.follow || m.viewport.YOffset() != anchor {
					t.Fatal("closing the local surface lost reading focus/anchor")
				}
				if m.currentDraft() != draft || m.session.Draft() != draft || state.Draft("env", "thread") != draft || m.composer.Line() != line || m.composer.Column() != column || m.composer.SelectedText() != selection {
					t.Fatal("reading shortcut changed exact canonical draft, caret, selection or durable draft")
				}
			})
		}
	}
}

func TestIntuitiveAliasesStayLiteralInEveryEditor(t *testing.T) {
	for _, editor := range []string{"composer", "request", "launch-message", "launch-title", "search", "picker", "browser"} {
		t.Run(editor, func(t *testing.T) {
			m := testModel(t)
			m.composer.SetValue("retained composer")
			finishCommand(t, m, m.dispatch("keyboard", "legacy"))
			// A modal may cover reading focus; that must not enable ? or /.
			m.transcriptFocus = editor != "composer"
			switch editor {
			case "request":
				m.openPrompt(retiredPrompt(7100, client.Question{Kind: client.EnterText, Initial: "first\nsecond"}))
			case "launch-message", "launch-title":
				m.openPrompt(retiredPrompt(7101, launchQuestion(client.LaunchInput{Message: "first\nsecond", Title: "title"})))
				if editor == "launch-title" {
					m.Update(keyPress(tea.KeyTab))
				}
			case "search":
				m.beginSearch()
			case "picker":
				m.openPalette()
			case "browser":
				m.openPrompt(retiredPrompt(7102, browserQuestion("a")))
			}
			m.restoreFocus()
			k := m.keyMap()
			if key.Matches(keyPress('?'), k.Help) || key.Matches(keyPress('/'), k.Search) || key.Matches(keyPress('p', tea.ModCtrl), k.Palette) {
				t.Fatal("reading alias escaped its contextual guard")
			}
			original := m.dialog
			m.Update(keyPress('?'))
			m.Update(keyPress('/'))
			var text string
			switch editor {
			case "composer":
				text = m.currentDraft()
			case "request", "launch-message":
				text = m.modalInput.Value()
			case "launch-title":
				text = m.launchTitle.Value()
			case "search":
				text = m.searchInput.Value()
			case "picker", "browser":
				text = m.dialog.list.FilterValue()
			}
			if !strings.Contains(text, "?/") || m.detail != "" || m.dialog != original || m.searching != (editor == "search") {
				t.Fatalf("editor lost literal ?/: %q", text)
			}
			m.Update(keyPress('p', tea.ModCtrl))
			if m.detail != "" || m.dialog != original || editor != "composer" && m.currentDraft() != "retained composer" {
				t.Fatal("Ctrl+P replaced the editor or changed the covered draft")
			}
			if (editor == "request" || editor == "launch-message") && m.modalInput.Line() != 0 {
				t.Fatal("Ctrl+P did not move up in the multiline editor")
			}
		})
	}
}

func TestIntuitiveConversationHelpKeepsStopAndOneContextualRequestsShortcut(t *testing.T) {
	for _, width := range []int{60, 80, 120} {
		for _, reading := range []bool{false, true} {
			for _, draft := range []string{"", "unsent"} {
				for _, requests := range []int{0, 3} {
					m := testModel(t)
					m.composer.SetValue(draft)
					m.transcriptFocus, m.status.Requests = reading, requests
					text := ansi.Strip(m.bindingHelp(width, m.helpMap()))
					if !strings.Contains(text, "Ctrl+S Stop/hold") || !strings.Contains(text, m.keyMap().Palette.Help().Key) {
						t.Fatalf("width %d hid Stop or contextual commands: %q", width, text)
					}
					want := 0
					if requests > 0 {
						want = 1
					}
					if strings.Count(text, "Ctrl+R requests") != want || strings.Contains(text, "3") {
						t.Fatalf("requests repeated, hidden or counted again at width %d: %q", width, text)
					}
				}
			}
		}
	}
	m := testModel(t)
	m.problem, m.preservedDraft = "uncertain operation", "\texact\r\n"
	text := m.helpText()
	for _, want := range []string{"/error", "/edit", "/recall", "operation ID", "Ctrl+S"} {
		if !strings.Contains(text, want) {
			t.Fatalf("help lost essential recovery route %q", want)
		}
	}
}

func TestIntuitiveFKeyAliasesRemainAvailable(t *testing.T) {
	for _, draft := range []string{"", "nonempty"} {
		m := testModel(t)
		m.composer.SetValue(draft)
		m.Update(keyPress(tea.KeyF1))
		if m.detail == "" || m.currentDraft() != draft {
			t.Fatal("F1 no longer opens help with a preserved draft")
		}
		m.Update(keyPress(tea.KeyEscape))
		m.Update(keyPress(tea.KeyF3))
		if m.dialog == nil || m.dialog.local != "palette" || m.currentDraft() != draft {
			t.Fatal("F3 no longer opens commands with a preserved draft")
		}
	}
}
