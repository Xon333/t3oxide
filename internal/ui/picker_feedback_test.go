package ui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
	"github.com/charmbracelet/x/ansi"
)

func pickerFeedbackType(m *Model, query string) {
	for _, r := range query {
		m.Update(keyPress(r))
	}
}

func TestPickerCompactLayoutsKeepQuerySelectionAndPortableCompletion(t *testing.T) {
	for _, size := range [][2]int{{60, 16}, {80, 24}, {120, 35}, {28, 12}, {20, 8}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m := testModel(t)
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			var choices []client.Choice
			for i := range 12 {
				choices = append(choices, client.Choice{ID: fmt.Sprintf("exact-id-%02d", i), Label: fmt.Sprintf("Item %02d", i), Description: "Optional explanation"})
			}
			m.openPrompt(retiredPrompt(1001, client.Question{Kind: client.ChooseMany, Title: "Answers", Prompt: strings.Repeat("Full question ", 30), Choices: choices}))
			pickerFeedbackType(m, "Item 00")
			m.Update(keyPress(tea.KeyEnter))
			if !m.dialog.selected["exact-id-00"] {
				t.Fatal("filtered Enter did not toggle the exact offered ID")
			}
			m.Update(keyPress('u', tea.ModCtrl))
			pickerFeedbackType(m, "Item 11")
			panel := ansi.Strip(m.modalView())
			for _, want := range []string{"Filter: Item 11", "→ Item 11", "Continue", "(1)", "continue"} {
				if want == "(1)" && m.modalWidth >= 25 {
					want = "1 selected"
				}
				if !strings.Contains(panel, want) {
					t.Fatalf("missing %q while the checked answer is filtered out:\n%s", want, panel)
				}
			}
			if lipgloss.Width(m.modalView()) > size[0] || lipgloss.Height(m.modalView()) > size[1] {
				t.Fatalf("picker exceeds terminal: %dx%d", lipgloss.Width(m.modalView()), lipgloss.Height(m.modalView()))
			}
			if size[0] >= 60 && m.dialog.list.FilterInput.Width() < 40 {
				t.Fatal("supported compact layout squeezed the native query editor")
			}
			if size[1] < 24 && strings.Contains(panel, "Optional explanation") {
				t.Fatal("optional descriptions survived at the expense of compact controls")
			}
			m.Update(keyPress(tea.KeyTab))
			if !m.dialog.finishFocused || m.dialog.list.FilterInput.Focused() {
				t.Fatal("Tab did not focus the fixed completion control")
			}
			m.Update(tea.PasteMsg{Content: "must not edit hidden query"})
			if m.dialog.list.FilterValue() != "Item 11" || !m.dialog.selected["exact-id-00"] {
				t.Fatal("completion focus changed the query or selected answer")
			}
			m.Update(keyPress(tea.KeyTab))
			m.Update(keyPress('u', tea.ModCtrl))
			pickerFeedbackType(m, "no matches here")
			if m.dialog.list.SelectedItem() != nil {
				t.Fatal("no-match query retained an old current selection")
			}
			m.Update(keyPress(tea.KeyTab))
			m.Update(keyPress(tea.KeyEnter))
			if m.dialog != nil || !strings.Contains(m.notice, "retired") {
				t.Fatal("Tab then ordinary Enter did not complete the exact prompt with zero matches")
			}
		})
	}
}

func TestPickerCompletionIgnoresLateComponentWorkAndPreservesFullSelectionDetails(t *testing.T) {
	m := testModel(t)
	m.openPrompt(retiredPrompt(1002, client.Question{Kind: client.ChooseMany, Title: "Answers", Choices: []client.Choice{
		{ID: "offered:alpha", Label: "Alpha", Description: strings.Repeat("Complete consequence\n", 8)},
		{ID: "offered:zebra", Label: "Zebra"},
	}}))
	pickerFeedbackType(m, "Alpha")
	m.Update(keyPress(tea.KeyEnter))
	old := m.dialog.list
	late := m.componentCmd("list", m.dialog.serial, old.SetItem(0, old.Items()[0]))
	m.Update(keyPress('u', tea.ModCtrl))
	pickerFeedbackType(m, "Zebra")
	m.Update(keyPress(tea.KeyTab))
	m.Update(late())
	if m.dialog.list.FilterValue() != "Zebra" || m.dialog.list.SelectedItem().(choiceItem).id != "offered:zebra" || !m.dialog.selected["offered:alpha"] {
		t.Fatal("late filtering changed completion's query, current result or offered IDs")
	}
	if !strings.Contains(m.dialog.detailText, "ID: offered:alpha") || !strings.Contains(m.dialog.detailText, strings.Repeat("Complete consequence\n", 8)) {
		t.Fatal("filtering hid the complete selected answer from details")
	}
	m.Update(keyPress(tea.KeyTab))
	if m.dialog.finishFocused || m.dialog.list.FilterState() != list.Filtering || !m.dialog.list.FilterInput.Focused() {
		t.Fatal("returning to choices lost query-editing focus")
	}
}

func TestPickerPaletteReentryKeepsQueryAndCurrentResult(t *testing.T) {
	m := testModel(t)
	m.Update(keyPress(tea.KeyF3))
	pickerFeedbackType(m, "help")
	d, selected := m.dialog, m.dialog.list.SelectedItem().(choiceItem)
	m.Update(keyPress(tea.KeyF3))
	if m.dialog != d || m.dialog.list.FilterValue() != "help" || m.dialog.list.SelectedItem().(choiceItem).command != selected.command {
		t.Fatal("F3 rebuilt the active palette or reset its filter/selection")
	}
}

func TestPickerTinyFallbackPreservesRequestUntilResize(t *testing.T) {
	m := testModel(t)
	m.openPrompt(retiredPrompt(1004, client.Question{Kind: client.ChooseMany, Choices: []client.Choice{{ID: "exact-id", Label: "Offered answer"}}}))
	pickerFeedbackType(m, "Offered")
	m.Update(keyPress(tea.KeyEnter))
	d := m.dialog
	m.Update(tea.WindowSizeMsg{Width: 15, Height: 6})
	if !strings.Contains(ansi.Strip(m.View().Content), "Resize") {
		t.Fatal("tiny terminal has no resize fallback")
	}
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	if m.dialog != d || m.dialog.list.FilterValue() != "Offered" || !m.dialog.selected["exact-id"] {
		t.Fatal("resizing through the fallback discarded the request, query or selection")
	}
}

func TestFeedbackDetailKeepsCompleteErrorsKnownTargetAndRedactsCredentials(t *testing.T) {
	m := testModel(t)
	m.status = client.Status{ProfileID: "profile-a", EnvironmentID: "environment-a", ThreadID: "thread-a"}
	m.snapshot = &wire.Snapshot{Metadata: wire.Metadata{ModelSelection: &config.ModelSelection{InstanceID: "provider-a", Model: "model-a", Options: []config.ModelOption{{ID: "private-option", Value: "never-include-options"}}}}}
	full := "HTTP 401 unauthorized\n" + strings.Repeat("Complete backend explanation\n", 80) + "Last diagnostic line"
	m.problem = full + "\nAuthorization: Bearer secret-bearer-value\nhttps://user:password@example.invalid/callback?token=secret-query-value&code=123\n{\"api_key\":\"secret-key-value\", \"password\":\"secret-password-value\"}"
	m.streamProblem = "Connection ended: actual stream diagnosis"
	got := m.feedbackDetailText()
	for _, want := range []string{full, "actual stream diagnosis", "Profile: profile-a", "Environment: environment-a", "Thread: thread-a", "Thread provider: provider-a", "Thread model: model-a", "/web", "/reconnect", "[redacted]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("detail lost %q", want)
		}
	}
	for _, secret := range []string{"secret-bearer-value", "user:password", "secret-query-value", "secret-key-value", "secret-password-value", "private-option", "never-include-options"} {
		if strings.Contains(got, secret) {
			t.Fatalf("detail exposed %q", secret)
		}
	}
	if !strings.HasSuffix(ansi.Strip(m.feedbackView(60)), "F2 error") {
		t.Fatal("compact feedback truncated the error expansion affordance")
	}
	if m.problem != full+"\nAuthorization: Bearer secret-bearer-value\nhttps://user:password@example.invalid/callback?token=secret-query-value&code=123\n{\"api_key\":\"secret-key-value\", \"password\":\"secret-password-value\"}" {
		t.Fatal("presentation mutated the retained complete error")
	}
}

func TestFeedbackOverlayPreservesRequestQueryAndAnswer(t *testing.T) {
	for _, kind := range []client.Kind{client.EnterText, client.ChooseOne, client.ChooseMany} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			m := testModel(t)
			m.composer.SetValue("preserved human draft")
			m.openPrompt(retiredPrompt(1003, client.Question{Kind: kind, Title: "Exact request", Initial: "exact answer", Choices: []client.Choice{{ID: "exact-id", Label: "Exact offered answer"}}}))
			if kind != client.EnterText {
				pickerFeedbackType(m, "Exact")
			}
			d, query, answer := m.dialog, m.dialog.list.FilterValue(), m.modalInput.Value()
			m.problem = "connection refused\n" + strings.Repeat("Entire backend error\n", 60) + "Visible error tail"
			m.Update(keyPress(tea.KeyF2))
			if m.detail == "" {
				t.Fatal("F2 did not open the full error above a request")
			}
			m.Update(tea.PasteMsg{Content: "hidden input"})
			m.Update(keyPress('x'))
			m.Update(keyPress(tea.KeyEnter))
			for range 16 {
				m.Update(keyPress(tea.KeyPgDown))
			}
			if m.detailViewport.YOffset() == 0 || !strings.Contains(ansi.Strip(m.detailViewport.View()), "Visible error tail") {
				t.Fatal("full error tail is not reachable through the existing viewport")
			}
			m.Update(keyPress(tea.KeyEscape))
			if m.detail != "" || m.dialog != d || m.dialog.list.FilterValue() != query || m.modalInput.Value() != answer || m.composer.Value() != "preserved human draft" {
				t.Fatal("error review discarded or edited the active request/draft")
			}
		})
	}
}

func TestFeedbackDoesNotInventIdentityOrOpenWithoutError(t *testing.T) {
	m := testModel(t)
	if m.openFeedbackDetail() || m.feedbackDetailText() != "" || m.detail != "" {
		t.Fatal("empty error state opened a detail")
	}
	m.problem = "timeout while connecting"
	text := m.feedbackDetailText()
	for _, absent := range []string{"Profile:", "Environment:", "Thread:", "Thread provider:", "Account:", "http://", "https://", "t3-thread://"} {
		if strings.Contains(text, absent) {
			t.Fatalf("unreported target information was invented: %q", absent)
		}
	}
}
