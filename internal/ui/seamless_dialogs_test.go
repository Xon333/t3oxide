package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/charmbracelet/x/ansi"
)

func TestSeamlessSettingsChooserKeepsStagedIdentityAndOptions(t *testing.T) {
	m := testModel(t)
	review := auditSettingsReview()
	review.Selection.Model = "model-b"
	review.Selection = review.Selection.WithOption("effort", "high")
	p, result, _ := intuitivePrompt(t, client.Question{ModelSettings: review})
	m.openPrompt(p)
	source := m.dialog
	before := source.settingsForm.selection.Clone()
	m.openModelSettingChooser()
	if got := m.dialog.list.SelectedItem().(choiceItem).id; got != "model-b" {
		t.Fatalf("model chooser lost the staged choice: %q", got)
	}
	m.chooseDialog()
	if m.dialog != source || !source.settingsForm.selection.Equal(before) {
		t.Fatal("accepting the current model reset its options")
	}
	source.settingsForm.focus = 1
	m.openModelSettingChooser()
	if got := m.dialog.list.SelectedItem().(choiceItem).id; got != "value:high" {
		t.Fatalf("option chooser lost the staged value: %q", got)
	}
	m.chooseDialog()
	if !source.settingsForm.selection.Equal(before) || !review.Selection.Equal(before) {
		t.Fatal("accepting the current option changed the source or staged choice")
	}
	assertIntuitiveNoReply(t, result)
	m.applyModelSettings()
	awaitIntuitiveReply(t, result)
	if m.dialog != nil || p.Respond(client.Answer{Action: "apply"}) || !source.settingsForm.selection.Equal(before) {
		t.Fatal("Apply did not retire the exact explicitly reviewed selection once")
	}
}

func TestSeamlessSingleModelCyclingDoesNotResetOptions(t *testing.T) {
	m := testModel(t)
	review := auditSettingsReview()
	review.Catalog.Models = review.Catalog.Models[:1]
	review.Selection = review.Selection.WithOption("reasoningEffort", "high")
	p, result, _ := intuitivePrompt(t, client.Question{ModelSettings: review})
	m.openPrompt(p)
	m.modelSettingsKey(keyPress(tea.KeyRight))
	m.modelSettingsKey(keyPress(tea.KeyLeft))
	if !m.dialog.settingsForm.selection.Equal(review.Selection) {
		t.Fatal("cycling a single available model reset reviewed options")
	}
	assertIntuitiveNoReply(t, result)
}

func TestSeamlessSettingsDetailsRetainCompleteProviderValues(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	review := auditSettingsReview()
	long := strings.Repeat("Long provider explanation 界 ", 20)
	review.Catalog.Models[0].Options[0].Description = long
	review.Catalog.Models[0].Options[0].Options[0].Description = long + "final consequence"
	review.Location.Workspace = strings.Repeat("workspace/", 20)
	p, _, _ := intuitivePrompt(t, client.Question{ModelSettings: review})
	m.openPrompt(p)
	m.handleKey(keyPress('l', tea.ModCtrl))
	if !strings.Contains(m.dialog.detailText, long+"final consequence") || !strings.Contains(m.dialog.detailText, review.Location.Workspace) {
		t.Fatal("Details reused the clipped form instead of the complete review values")
	}
	if !strings.Contains(m.dialog.detailText, review.Location.ThreadID) || !strings.Contains(m.dialog.detailText, review.Selection.InstanceID) {
		t.Fatal("Details lost source identity")
	}
}

func TestSeamlessRequestCancelCapturesTheActiveExactEditor(t *testing.T) {
	m := testModel(t)
	progress := &client.RequestProgress{Answers: map[string]any{}}
	p, result, _ := intuitivePrompt(t, client.Question{RequestQuestions: []client.RequestQuestion{{ID: "answer", Required: true}}, RequestProgress: progress})
	m.openPrompt(p)
	m.restoreFocus()
	want := "  Exact answer 界\nsecond line  "
	m.Update(tea.PasteMsg{Content: want})
	form := m.dialog.requestForm
	m.cancelDialog()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("cancel answered instead of cancelling the local broker: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel failed to retire the local broker")
	}
	if form.progress().Answers["answer"] != want {
		t.Fatal("cancel lost the active editor before capturing progress")
	}
	if len(progress.Answers) != 0 || m.dialog != nil {
		t.Fatal("cancel mutated the original progress or kept the review active")
	}
}

func TestSeamlessRequestClearCustomDoesNotSubmitAnInvisibleAnswer(t *testing.T) {
	m := testModel(t)
	q := client.RequestQuestion{ID: "answer", Required: true, AllowCustomAnswer: true, Options: []client.RequestOption{{Value: "offered", Label: "Offered"}}}
	p, result, _ := intuitivePrompt(t, client.Question{RequestQuestions: []client.RequestQuestion{q}})
	m.openPrompt(p)
	m.requestFormKey(keyPress(tea.KeyTab))
	m.modalInput.SetValue("custom answer")
	m.requestFormKey(keyPress(tea.KeyTab))
	m.requestFormKey(keyPress(tea.KeyTab, tea.ModShift))
	m.modalInput.SetValue("")
	m.requestFormKey(keyPress(tea.KeyTab))
	m.requestFormKey(keyPress(tea.KeyEnter))
	if m.dialog == nil || m.dialog.requestForm.problem == "" || requestAnswered(m.dialog.requestForm.answers[q.ID]) {
		t.Fatal("a cleared custom answer remained silently eligible for submission")
	}
	assertIntuitiveNoReply(t, result)
	m.dialog.requestForm.focus = 0
	m.toggleRequestOption()
	m.requestFormKey(keyPress(tea.KeyTab))
	if m.modalInput.Value() != "" {
		t.Fatal("offered value was copied into the custom editor")
	}
	m.requestFormKey(keyPress(tea.KeyTab))
	form := m.dialog.requestForm
	m.requestFormKey(keyPress(tea.KeyEnter))
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("submit was treated as cancellation by the non-request fixture")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("explicit Submit did not complete the prompt")
	}
	if form.progress().Answers[q.ID] != "offered" || m.dialog != nil {
		t.Fatal("visiting an empty custom editor lost the exact offered answer")
	}
}

func TestSeamlessRequestCurrentChoiceAndPinnedProgressAtCompactSizes(t *testing.T) {
	for _, size := range [][2]int{{60, 16}, {80, 24}, {120, 35}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := testModel(t)
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			q := client.RequestQuestion{ID: "many", Required: true, AllowCustomAnswer: true, Header: "Scope", Question: strings.Repeat("Review scope ", 50), InitialAnswer: "23"}
			for i := range 24 {
				q.Options = append(q.Options, client.RequestOption{Value: fmt.Sprint(i), Label: fmt.Sprintf("Option %02d", i)})
			}
			p, result, _ := intuitivePrompt(t, client.Question{RequestQuestions: []client.RequestQuestion{q}})
			m.openPrompt(p)
			if m.dialog.requestForm.option != 23 || m.modalInput.Value() != "" {
				t.Fatal("revisited form did not focus the selected option independently of custom text")
			}
			view := ansi.Strip(m.modalView())
			for _, want := range []string{"Question 1/1", "Required", "Option 23", "Submit answers", "Cancel"} {
				if !strings.Contains(view, want) {
					t.Fatalf("focused option or pinned form control %q is missing:\n%s", want, view)
				}
			}
			if lipgloss.Width(m.modalView()) > size[0] || lipgloss.Height(m.modalView()) > size[1] {
				t.Fatal("request review exceeds terminal bounds")
			}
			assertIntuitiveNoReply(t, result)
		})
	}
}

func TestSeamlessPaletteBackRestoresSearchQueryCaretAndResultScroll(t *testing.T) {
	m := testModel(t)
	m.beginSearch()
	m.searchInput.SetValue("unsubmitted query 界")
	m.searchInput.SetCursor(4)
	m.openPalette()
	m.cancelDialog()
	if !m.searching || m.searchInput.Value() != "unsubmitted query 界" || m.searchInput.Position() != 4 || !m.searchInput.Focused() {
		t.Fatal("Commands Back lost the original search editor/focus")
	}
	m.searching = false
	m.openDetail("Source result", strings.Repeat("exact result row\n", 60))
	m.detailCopy = "exact\tcopy\r\n"
	m.detailViewport.SetYOffset(18)
	before := m.detail
	m.openPalette()
	m.cancelDialog()
	if m.detail != before || m.detailTitle != "Source result" || m.detailCopy != "exact\tcopy\r\n" || m.detailViewport.YOffset() != 18 {
		t.Fatal("Commands Back lost the inspected source, copy value or reading position")
	}
	m.openPalette()
	m.dialog.list.SetFilterText("copy detail")
	m.dialog.list.SetFilterState(list.Filtering)
	m.dialog.filterQuery = "copy detail"
	if cmd := m.chooseDialog(); cmd == nil || m.detail != before || m.detailCopy != "exact\tcopy\r\n" {
		t.Fatal("Copy from Commands no longer addresses the inspected detail")
	}
}

func TestSeamlessPaletteCannotRestoreInspectionAcrossAttachments(t *testing.T) {
	m := testModel(t)
	m.openDetail("Old result", "exact old source")
	m.openPalette()
	m.generation++
	m.cancelDialog()
	if m.detail != "" || m.detailTitle != "" || m.detailCopy != "" || m.searching || m.dialog != nil {
		t.Fatal("a suspended inspection crossed attachment generation")
	}
}

func TestSeamlessPaletteCannotReviveAChooserWhosePromptRetired(t *testing.T) {
	for _, origin := range []string{"settings-choice", "browser-more"} {
		t.Run(origin, func(t *testing.T) {
			m := testModel(t)
			question := client.Question{ModelSettings: auditSettingsReview()}
			if origin == "browser-more" {
				question = browserQuestion("a")
			}
			p, _, cancel := intuitivePrompt(t, question)
			m.openPrompt(p)
			if origin == "settings-choice" {
				m.openModelSettingChooser()
			} else {
				m.openBrowserMore()
			}
			m.openPalette()
			cancel()
			<-p.Done
			m.cancelDialog()
			if m.dialog != nil || !strings.Contains(m.notice, "retired") {
				t.Fatal("Commands Back revived local controls owned by a retired prompt")
			}
			if origin == "browser-more" && (m.browserDialog != nil || m.browserPage) {
				t.Fatal("retired browser controls left an idle loading page behind")
			}
		})
	}
}

func TestSeamlessCompactFormsKeepEveryControlReachable(t *testing.T) {
	for _, size := range [][2]int{{40, 12}, {60, 16}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := testModel(t)
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			review := auditSettingsReview()
			for i := range 12 {
				review.Catalog.Models[0].Options = append(review.Catalog.Models[0].Options, client.OptionDescriptor{ID: fmt.Sprintf("option-%d", i), Label: fmt.Sprintf("Option %d", i), Type: "boolean"})
			}
			p, result, _ := intuitivePrompt(t, client.Question{ModelSettings: review})
			m.openPrompt(p)
			fields := m.dialog.settingsForm.optionCount() + 3
			for i := range fields {
				panel := m.modalView()
				if lipgloss.Width(panel) > size[0] || lipgloss.Height(panel) > size[1] {
					t.Fatalf("settings field %d escaped the terminal", i)
				}
				if i == fields-2 && !strings.Contains(ansi.Strip(panel), "Apply changes") || i == fields-1 && !strings.Contains(ansi.Strip(panel), "Cancel") {
					t.Fatal("focused settings action was outside the viewport")
				}
				m.modelSettingsKey(keyPress(tea.KeyTab))
			}
			assertIntuitiveNoReply(t, result)
			m.cancelDialog()
			q := client.RequestQuestion{ID: "q", Required: true, AllowCustomAnswer: true, Header: "Pick a scope", Question: "Which scope?", Options: []client.RequestOption{{Value: "one", Label: "One"}, {Value: "two", Label: "Two"}}}
			p2, result2, _ := intuitivePrompt(t, client.Question{RequestQuestions: []client.RequestQuestion{q}})
			m.openPrompt(p2)
			for range 4 { // options, custom editor, Submit, Cancel
				panel := m.modalView()
				if lipgloss.Width(panel) > size[0] || lipgloss.Height(panel) > size[1] {
					t.Fatalf("request focus %d escaped the terminal", m.dialog.requestForm.focus)
				}
				if !strings.Contains(ansi.Strip(panel), "Submit answers") || !strings.Contains(ansi.Strip(panel), "Cancel") {
					t.Fatal("request actions disappeared while moving through the form")
				}
				m.requestFormKey(keyPress(tea.KeyTab))
			}
			assertIntuitiveNoReply(t, result2)
		})
	}
}

func TestSeamlessFullPageFormsKeepGlobalControlsAndExactDetails(t *testing.T) {
	for _, size := range [][2]int{{40, 12}, {60, 16}} {
		for _, kind := range []string{"settings", "request"} {
			t.Run(fmt.Sprint(size)+"/"+kind, func(t *testing.T) {
				m := testModel(t)
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				m.status.ThreadID, m.status.ActiveRunID = "attached-thread", "reviewed-run"
				long := strings.Repeat("Exact option consequence 界 ", 12)
				review := auditSettingsReview()
				review.Catalog.Models[0].Options[0].Description = long
				q := client.Question{ModelSettings: review}
				want := "Apply changes"
				if kind == "request" {
					q = client.Question{RequestQuestions: []client.RequestQuestion{{ID: "q", Question: long, Required: true, InitialAnswer: "exact answer"}}}
					want = "Submit answers"
				}
				p, result, _ := intuitivePrompt(t, q)
				m.browserPage = true
				m.openPrompt(p)
				if kind == "settings" {
					m.dialog.settingsForm.focus = m.dialog.settingsForm.optionCount() + 1
				} else {
					m.dialog.requestForm.focus = 3
				}
				m.resize()
				view := m.View().Content
				for _, text := range []string{want, "Ctrl+S", "Ctrl+Q"} {
					if !strings.Contains(ansi.Strip(view), text) {
						t.Fatalf("full-page %s hid %q at %v:\n%s", kind, text, size, ansi.Strip(view))
					}
				}
				if lipgloss.Width(view) > size[0] || lipgloss.Height(view) > size[1] {
					t.Fatal("full-page form escaped terminal bounds")
				}
				m.handleKey(keyPress('l', tea.ModCtrl))
				if !m.dialog.details || !strings.Contains(m.dialog.detailText, long) {
					t.Fatal("compact full-page details lost exact provider/question content")
				}
				m.handleKey(keyPress(tea.KeyEscape))
				if m.dialog == nil || m.dialog.prompt != p || m.dialog.details {
					t.Fatal("Details Back retired or replaced the exact form prompt")
				}
				assertIntuitiveNoReply(t, result)
			})
		}
	}
}
