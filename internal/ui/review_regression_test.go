package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/charmbracelet/x/ansi"
)

func TestDiagnosticRedactionConsumesEscapedQuotedCredentials(t *testing.T) {
	for _, text := range []string{
		`{"password":"prefix\"secret-tail"}`,
		`token='prefix\'secret-tail'`,
		`{"api_key":"prefix\\\"secret-tail","safe":"visible"}`,
		`{"password":"Bearer prefix\"secret-tail"}`,
		`{"password":"Basic prefix\"secret-tail"}`,
		`Authorization: Bearer prefix\"secret-tail`,
		`Authorization: Basic prefix\"secret-tail`,
		`Authorization: Bearer "secret-tail"`,
		`Authorization: Basic 'secret-tail'`,
		`Authorization: Bearer "prefix\"secret-tail"`,
		`Authorization: Basic "prefix\"secret-tail"`,
	} {
		got := redactFeedback(text)
		if strings.Contains(got, "prefix") || strings.Contains(got, "secret-tail") || !strings.Contains(got, "[redacted]") {
			t.Fatalf("escaped credential leaked: %s", got)
		}
	}
}

func TestMultiselectHelpMatchesFilterAndContinueFocus(t *testing.T) {
	m := testModel(t)
	m.openPrompt(retiredPrompt(4002, client.Question{Kind: client.ChooseMany, Choices: []client.Choice{{ID: "a", Label: "Alpha"}}}))
	m.restoreFocus()
	m.Update(keyPress('a'))
	m.Update(keyPress(tea.KeyF1))
	h := strings.Join(strings.Fields(ansi.Strip(m.detail)), " ")
	if !strings.Contains(h, "Enter") || strings.Contains(h, "Space toggle") {
		t.Fatal("filter help advertised a toggle key owned by query editing")
	}
	m.Update(keyPress(tea.KeyEscape))
	m.Update(keyPress(tea.KeyTab))
	m.Update(keyPress(tea.KeyF1))
	h = strings.Join(strings.Fields(ansi.Strip(m.detail)), " ")
	if !strings.Contains(h, "Enter continue") && !strings.Contains(h, "Enter  continue") || strings.Contains(h, "toggle") {
		t.Fatalf("Continue help disagrees with its focused action:\n%s", h)
	}
}

func TestExactDetailCopyUsesOriginalHumanBytes(t *testing.T) {
	m := testModel(t)
	exact := "\tcode\r\ntoken=example\n� "
	m.restoreDraft(exact)
	m.Update(keyPress(tea.KeyF2))
	if m.detailTitle != "Exact draft" || m.detailCopyText() != exact {
		t.Fatal("exact draft detail copy changed human text")
	}
	m.openDetail("Error details", `{"password":"prefix\"secret-tail"}`)
	if strings.Contains(m.detailCopyText(), "secret-tail") {
		t.Fatal("diagnostic copy reused an exact human-text buffer")
	}
}

func TestPreservedAnswerReviewAndErrorRemainSeparatelyReachable(t *testing.T) {
	m := testModel(t)
	answer := "\tinitial\r\nanswer"
	m.openPrompt(retiredPrompt(4001, client.Question{Kind: client.EnterText, Title: "Question", Initial: answer}))
	m.restoreFocus()
	m.Update(keyPress(tea.KeyEnter))
	m.Update(keyPress(tea.KeyF2))
	if m.detail != "" || !m.dialog.details || !strings.Contains(m.dialog.detailText, "initial") {
		t.Fatal("preserved-answer error blocked original review")
	}
	m.Update(keyPress(tea.KeyF2))
	m.Update(keyPress(tea.KeyF8))
	if m.detail == "" || m.dialog == nil || m.dialog.preservedInitial != answer {
		t.Fatal("error review lost the exact preserved answer")
	}
	m.Update(keyPress(tea.KeyEscape))
	m.Update(keyPress(tea.KeyF2))
	if !m.dialog.details {
		t.Fatal("closing error detail left original answer inaccessible")
	}
}

func TestPaletteDeliveryVariantsDispatchValidArguments(t *testing.T) {
	for _, mode := range []string{"auto", "queue", "steer"} {
		m, _ := draftSessionModel(t)
		m.Update(keyPress(tea.KeyF3))
		for _, r := range "Delivery " + mode {
			m.Update(keyPress(r))
		}
		item, ok := m.dialog.list.SelectedItem().(choiceItem)
		if !ok || item.command != "delivery" || item.args != mode {
			t.Fatalf("delivery %s missing or unusable: %+v", mode, item)
		}
		_, cmd := m.Update(keyPress(tea.KeyEnter))
		finishCommand(t, m, cmd)
		if m.problem != "" || m.dialog != nil || m.actionBusy {
			t.Fatalf("delivery %s was rejected: %s", mode, m.problem)
		}
	}
}

func TestHiddenCompletionCannotConsumeInput(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 28, Height: 12})
	m.Update(tea.PasteMsg{Content: "/hel"})
	m.Update(keyPress(tea.KeyEnter))
	if m.currentDraft() != "/hel\n" || m.detail != "" || m.problem != "" {
		t.Fatal("invisible popup accepted or executed a command")
	}
	m.Update(keyPress(']', tea.ModCtrl))
	if m.currentDraft() != "/hel\n" || m.detail != "" || m.problem != "Choose a conversation first" {
		t.Fatal("explicit Send accepted an invisible command completion or changed the exact draft")
	}
}
