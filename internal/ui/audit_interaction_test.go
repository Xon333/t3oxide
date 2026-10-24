package ui

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
	"github.com/charmbracelet/x/ansi"
)

func TestAuditInteractionCommandSearchAndBoundVariants(t *testing.T) {
	m := testModel(t)
	items := m.paletteItems()
	targets := make([]string, len(items))
	for i, item := range items {
		targets[i] = item.FilterValue()
	}
	for _, spec := range m.commandCatalog() {
		ranks := commandFilter(spec.name, targets)
		if len(ranks) == 0 || items[ranks[0].Index].(choiceItem).command != spec.name {
			t.Fatalf("canonical %q missing or outranked", spec.name)
		}
	}
	if ranks := commandFilter("keys", targets); len(ranks) == 0 || items[ranks[0].Index].(choiceItem).command != "help" {
		t.Fatal("help alias not indexed")
	}
	for _, query := range []string{"copy draft", "retry stop", "resolve stop"} {
		ranks := commandFilter(query, targets)
		if len(ranks) == 0 {
			t.Fatalf("variant missing: %s", query)
		}
		row := items[ranks[0].Index].(choiceItem)
		parts := strings.Fields(query)
		if row.command != parts[0] || row.args != parts[1] {
			t.Fatalf("%q selected %q %q", query, row.command, row.args)
		}
	}
	if m.commandUnavailable("presets") != "" {
		t.Fatal("offline presets blanket disabled")
	}
}

func TestAuditInteractionShortAndFilteredMenusUsePanel(t *testing.T) {
	for _, size := range [][2]int{{60, 16}, {120, 35}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := testModel(t)
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m.openThemes()
			view := ansi.Strip(m.dialog.list.View())
			for _, label := range []string{"Environment default", "Terminal", "Electric violet", "Electric cyan", "Acid lime"} {
				if !strings.Contains(view, label) {
					t.Fatalf("available row %q hidden:\n%s", label, view)
				}
			}
			m.openPalette()
			m.dialog.list.SetFilterText("copy")
			m.dialog.list.SetFilterState(list.Filtering)
			m.dialog.filterQuery = "copy"
			m.resize()
			view = ansi.Strip(m.dialog.list.View())
			if strings.Count(view, "Copy ·") < 2 {
				t.Fatalf("filtered panel still shows one row:\n%s", view)
			}
		})
	}
}

func auditSettingsReview() *client.ModelSettingsReview {
	selection := config.ModelSelection{InstanceID: "provider", Model: "model-a"}
	return &client.ModelSettingsReview{Selection: selection, Scope: "Current thread", Location: client.Location{Environment: "Source environment", ThreadID: "source-thread", Title: "Source title"}, Actions: []string{"apply", "cancel"}, Catalog: client.ModelCatalog{Available: true, InstanceID: "provider", Current: selection, Models: []client.CatalogModel{
		{Slug: "model-a", Name: "Model A", Options: []client.OptionDescriptor{{ID: "reasoningEffort", Label: "Effort", Type: "select", Options: []client.OptionChoice{{ID: "low", Label: "Low"}, {ID: "high", Label: "High"}}}, {ID: "serviceTier", Label: "Tier", Type: "select", Options: []client.OptionChoice{{ID: "standard", Label: "Standard"}, {ID: "priority", Label: "Priority"}}}}},
		{Slug: "model-b", Name: "Model B", Options: []client.OptionDescriptor{{ID: "effort", Label: "Effort", Type: "select", Options: []client.OptionChoice{{ID: "high", Label: "High", IsDefault: true}}}}},
	}}}
}
func TestAuditInteractionCombinedSettingsVisibleAndStaged(t *testing.T) {
	for _, size := range [][2]int{{60, 16}, {120, 35}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := testModel(t)
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			review := auditSettingsReview()
			p, result, _ := intuitivePrompt(t, client.Question{Title: "Settings", ModelSettings: review})
			m.openPrompt(p)
			if !strings.Contains(m.dialog.title, "Source environment / Source title") {
				t.Fatal("settings target came from attachment")
			}
			view := ansi.Strip(m.modalView())
			for _, label := range []string{"Model  model-a", "Effort", "Tier", "Apply changes", "Cancel"} {
				if !strings.Contains(view, label) {
					t.Fatalf("field %q missing at %v:\n%s", label, size, view)
				}
			}
			m.modelSettingsKey(keyPress(tea.KeyDown))
			m.modelSettingsKey(keyPress(tea.KeyRight))
			assertIntuitiveNoReply(t, result)
			if review.Selection.Options != nil {
				t.Fatal("staged edit mutated original review")
			}
			if _, ok := m.dialog.settingsForm.selection.Option("reasoningEffort"); !ok {
				t.Fatal("option wasn't staged")
			}
			m.cancelForm()
			if m.dialog != nil {
				t.Fatal("cancel left form open")
			}
		})
	}
}
func TestAuditInteractionSettingsSearchChooserReturnsStagedForm(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 35})
	review := auditSettingsReview()
	for i := 0; i < 128; i++ {
		review.Catalog.Models = append(review.Catalog.Models, client.CatalogModel{Slug: fmt.Sprintf("extra-%03d", i), Name: fmt.Sprintf("Extra %03d", i)})
	}
	p, result, _ := intuitivePrompt(t, client.Question{ModelSettings: review})
	m.openPrompt(p)
	source := m.dialog
	m.modelSettingsKey(keyPress(tea.KeyEnter))
	if m.dialog.formSource != source {
		t.Fatal("Enter did not open native chooser")
	}
	m.dialog.list.SetFilterText("extra-127")
	m.dialog.filterQuery = "extra-127"
	m.chooseDialog()
	if m.dialog != source || source.settingsForm.selection.Model != "extra-127" {
		t.Fatal("search choice didn't return to staged form")
	}
	assertIntuitiveNoReply(t, result)
	m.openModelSettingChooser()
	m.cancelDialog()
	if m.dialog != source {
		t.Fatal("chooser Back lost staged form")
	}
}
func TestAuditInteractionPortableCommandsPreserveSlashAndForm(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.composer.SetValue("/mod")
	m.composer.CursorStart()
	m.handleKey(keyPress('p', tea.ModCtrl))
	if m.dialog == nil || m.dialog.local != "palette" || m.composer.Value() != "/mod" {
		t.Fatal("Ctrl+P edited slash or failed commands")
	}
	m.cancelDialog()
	if m.composer.Value() != "/mod" {
		t.Fatal("palette Back changed draft")
	}
	p, result, _ := intuitivePrompt(t, client.Question{ModelSettings: auditSettingsReview()})
	m.openPrompt(p)
	source := m.dialog
	m.handleKey(keyPress('p', tea.ModCtrl))
	if m.dialog.local != "palette" || m.dialog.paletteSource != source {
		t.Fatal("form commands didn't preserve source")
	}
	m.cancelDialog()
	if m.dialog != source {
		t.Fatal("palette Back lost active form")
	}
	assertIntuitiveNoReply(t, result)
}
func TestAuditInteractionDetailsRoutesAndLocalRetirement(t *testing.T) {
	m := testModel(t)
	m.status.Context = "attached context"
	m.handleKey(keyPress('l', tea.ModCtrl))
	byKey := m.detail
	if !strings.Contains(byKey, "attached context") {
		t.Fatal("Ctrl+L has no conversation fallback")
	}
	m.detail = ""
	m.composer.SetValue("/details")
	m.slash("/details")
	if m.detail != byKey || m.currentDraft() != "" {
		t.Fatal("slash details differs or left command token")
	}
}
func TestAuditInteractionBrowserIdentityAndMeaningfulMore(t *testing.T) {
	m := testModel(t)
	q := browserQuestion("a")
	q.BrowserPage.Actions = []string{"types", "filter", "toggle", "expand", "collapse", "parent", "lifecycle", "next-page", "previous-page"}
	p, _, _ := intuitivePrompt(t, q)
	m.openPrompt(p)
	m.dialog.list.Select(1)
	identity := m.dialog.list.SelectedItem().(choiceItem).id
	m.saveBrowserState(m.dialog)
	slices.Reverse(q.Choices)
	p2, _, _ := intuitivePrompt(t, q)
	m.openPrompt(p2)
	if m.dialog.list.SelectedItem().(choiceItem).id != identity {
		t.Fatal("refresh selected numeric row instead of identity")
	}
	m.openBrowserMore()
	actions := []string{}
	for _, item := range m.dialog.list.Items() {
		row := item.(choiceItem)
		actions = append(actions, row.id)
	}
	if slices.Contains(actions, "toggle") || slices.Contains(actions, "expand") || slices.Contains(actions, "collapse") || slices.Contains(actions, "parent") {
		t.Fatalf("leaf exposes inert controls: %v", actions)
	}
	types := 0
	for _, id := range actions {
		if id == "types" || id == "filter" {
			types++
		}
	}
	if types != 1 || actionLabel("next-page") != "Next page" || actionLabel("lifecycle") != "Lifecycle" {
		t.Fatal("aliases/raw controls leaked")
	}
}
func TestAuditInteractionBrowserResultsFocusAndSnapshot(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	q := browserQuestion("a")
	q.BrowserPage.ObservedAt = "2026-10-24T10:00:00Z"
	q.BrowserPage.Lifecycle = "all"
	q.BrowserPage.More = true
	p, result, _ := intuitivePrompt(t, q)
	m.openPrompt(p)
	m.dialog.list.SetFilterText("")
	m.dialog.list.SetFilterState(list.Filtering)
	m.browserKey(keyPress(tea.KeyTab))
	if !m.dialog.resultsFocused {
		t.Fatal("Tab didn't focus Results after filtering")
	}
	if !strings.Contains(m.dialog.description, "Snapshot") || !strings.Contains(m.dialog.description, "outside this page") {
		t.Fatal("snapshot/completeness missing")
	}
	assertIntuitiveNoReply(t, result)
	m.status.ThreadID = "attached-thread"
	m.status.Location = client.Location{Environment: "Attached env", Title: "Attached title"}
	if status := m.attachedTargetStatus(); !strings.Contains(status, "Attached env / Attached title") || !strings.Contains(status, "Stop attached turn") {
		t.Fatal("Stop attachment not named")
	}
}
func TestAuditInteractionRequestBackPreservesExactAnswerAndCaret(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	q := client.Question{Kind: client.EnterText, RequestQuestions: []client.RequestQuestion{{ID: "first", Header: "First", Question: "Answer first", Required: true, InitialAnswer: "alpha beta"}, {ID: "second", Header: "Second", Question: "Answer second", Required: true}}}
	p, result, _ := intuitivePrompt(t, q)
	m.openPrompt(p)
	m.modalInput.CursorStart()
	m.modalInput.Focus()
	m.modalInput, _ = m.modalInput.Update(keyPress(tea.KeyRight))
	before := m.modalInput.Column()
	m.advanceRequest(false)
	if m.dialog.requestForm.index != 1 {
		t.Fatal("Next didn't navigate locally")
	}
	m.advanceRequest(true)
	if m.dialog.requestForm.index != 0 || m.modalInput.Value() != "alpha beta" || m.modalInput.Column() != before {
		t.Fatal("Back lost exact answer/caret")
	}
	assertIntuitiveNoReply(t, result)
	m.advanceRequest(false)
	m.advanceRequest(false)
	if m.dialog.requestForm.problem == "" {
		t.Fatal("required blank allowed submit")
	}
}
func TestAuditInteractionRequestMultiSelectAndPreservedCode(t *testing.T) {
	m := testModel(t)
	q := client.Question{Kind: client.EnterText, RequestQuestions: []client.RequestQuestion{{ID: "code", Header: "Code", InitialAnswer: "a\tb\r\nexact", Required: true}, {ID: "choices", MultiSelect: true, Required: true, Options: []client.RequestOption{{Value: "a", Label: "A"}, {Value: "b", Label: "B"}}}}}
	p, result, _ := intuitivePrompt(t, q)
	m.openPrompt(p)
	if m.dialog.preservedInitial != "a\tb\r\nexact" {
		t.Fatal("code normalized")
	}
	m.advanceRequest(false)
	f := m.dialog.requestForm
	m.toggleRequestOption()
	f.option = 1
	m.toggleRequestOption()
	values, ok := f.answers["choices"].([]string)
	if !ok || !slices.Equal(values, []string{"a", "b"}) || f.answers["code"] != "a\tb\r\nexact" {
		t.Fatal("exact answers not preserved")
	}
	assertIntuitiveNoReply(t, result)
}
func TestAuditInteractionExactRejectedInputExplicitReplacementAndOwnership(t *testing.T) {
	m := testModel(t)
	m.composer.SetValue("old draft")
	if !m.rejectInput(tea.PasteMsg{Content: "a\tb"}, "Draft") {
		t.Fatal("expected preserved editor-incompatible paste")
	}
	if !strings.HasPrefix(m.exactRejectedInputLabel(), "Replace draft") {
		t.Fatal("replacement mislabeled as insertion")
	}
	m.useExactRejectedInput()
	if m.currentDraft() != "a\tb" {
		t.Fatal("exact replacement changed bytes")
	}
	found := false
	for _, entry := range m.promptHistory() {
		if entry.Text == "old draft" {
			found = true
		}
	}
	if !found {
		t.Fatal("displaced draft not retained")
	}
	m.storeDraft("")
	m.rejectInput(tea.PasteMsg{Content: "x\ty"}, "Draft")
	m.generation++
	m.useExactRejectedInput()
	if m.currentDraft() != "" {
		t.Fatal("stale rejected input crossed attachment")
	}
	m.rejectInput(tea.PasteMsg{Content: strings.Repeat("x", maxInputBytes+1)}, "Draft")
	if _, path, ok := strings.Cut(m.problem, "exact attempt saved at "); ok {
		t.Cleanup(func() { _ = os.Remove(path) })
	}
	if m.exactRejectedInputAvailable() {
		t.Fatal("oversized rejected input enabled replacement")
	}
}

func TestAuditInteractionMultiSelectCustomRetainsOfferedValues(t *testing.T) {
	m := testModel(t)
	q := client.Question{Kind: client.EnterText, RequestQuestions: []client.RequestQuestion{{ID: "choices", MultiSelect: true, AllowCustomAnswer: true, Options: []client.RequestOption{{Value: "a", Label: "A"}, {Value: "b", Label: "B"}}}}}
	p, result, _ := intuitivePrompt(t, q)
	m.openPrompt(p)
	f := m.dialog.requestForm
	m.toggleRequestOption()
	f.option = 1
	m.toggleRequestOption()
	f.focus = 1
	m.modalInput.SetValue("custom exact")
	m.saveRequestAnswer()
	if values := f.answers["choices"].([]string); !slices.Equal(values, []string{"a", "b", "custom exact"}) {
		t.Fatalf("custom answer erased choices: %v", values)
	}
	m.modalInput.SetValue("")
	m.saveRequestAnswer()
	if slices.Contains(f.answers["choices"].([]string), "custom exact") {
		t.Fatal("cleared custom answer remained hidden in submission")
	}
	if !slices.Contains(f.answers["choices"].([]string), "a") || !slices.Contains(f.answers["choices"].([]string), "b") {
		t.Fatal("blank custom erased choices")
	}
	assertIntuitiveNoReply(t, result)
}
func TestAuditInteractionLegacyShortcutAndLocalReturnOwnership(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if err := m.settings.SetLegacyEditorCtrlP(true); err != nil {
		t.Fatal(err)
	}
	m.composer.SetValue("/mod")
	m.composer.CursorEnd()
	m.refreshCompletion()
	m.handleKey(keyPress(tea.KeyTab, tea.ModShift))
	if !m.transcriptFocus || m.currentDraft() != "/mod" {
		t.Fatal("portable legacy route completed/mutated slash")
	}
	m.handleKey(keyPress('p', tea.ModCtrl))
	if m.dialog == nil || m.dialog.local != "palette" {
		t.Fatal("legacy reading route failed")
	}
	m.cancelDialog()
	local := &dialog{local: "settings", list: m.newList("Settings", []list.Item{choiceItem{title: "Appearance", command: "theme"}})}
	local.list.SetFilterText("Appear")
	local.list.FilterInput.SetCursor(2)
	for range 12 {
		m.pushLocalSurface(local)
	}
	if len(m.localBack) != 8 {
		t.Fatal("local Back unbounded")
	}
	m.popLocalSurface()
	if m.dialog != local || m.dialog.list.FilterInput.Position() != 2 {
		t.Fatal("local Back lost native query caret")
	}
	m.localBack = nil
	m.pushLocalSurface(&dialog{prompt: retiredPrompt(9000, client.Question{})})
	if len(m.localBack) != 0 {
		t.Fatal("local Back admitted remote prompt")
	}
}
func TestAuditInteractionRetiredPaletteAndChooserNeverReviveForm(t *testing.T) {
	for _, chooser := range []bool{false, true} {
		m := testModel(t)
		p, _, cancel := intuitivePrompt(t, client.Question{ModelSettings: auditSettingsReview()})
		m.openPrompt(p)
		if chooser {
			m.openModelSettingChooser()
		} else {
			m.openPalette()
		}
		cancel()
		<-p.Done
		m.cancelDialog()
		if m.dialog != nil {
			t.Fatal("Back revived retired remote settings")
		}
	}
}
func TestAuditInteractionRequestControlsRemainVisibleWithManyOptions(t *testing.T) {
	for _, size := range [][2]int{{60, 16}, {120, 35}} {
		m := testModel(t)
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		question := client.RequestQuestion{ID: "many", Header: "Many", Question: strings.Repeat("Long question ", 40)}
		for i := 0; i < 24; i++ {
			question.Options = append(question.Options, client.RequestOption{Value: fmt.Sprint(i), Label: fmt.Sprintf("Option %02d", i)})
		}
		p, _, _ := intuitivePrompt(t, client.Question{RequestQuestions: []client.RequestQuestion{question}})
		m.openPrompt(p)
		m.dialog.requestForm.option = 23
		view := ansi.Strip(m.modalView())
		if !strings.Contains(view, "Option 23") || !strings.Contains(view, "Submit answers") || !strings.Contains(view, "Cancel") {
			t.Fatalf("focused option/control missing at %v:\n%s", size, view)
		}
	}
}

func TestAuditInteractionApplyAndSubmitAreSingleExplicitAnswers(t *testing.T) {
	m := testModel(t)
	p, result, _ := intuitivePrompt(t, client.Question{ModelSettings: auditSettingsReview()})
	m.openPrompt(p)
	m.modelSettingsKey(keyPress(tea.KeyRight))
	assertIntuitiveNoReply(t, result)
	m.applyModelSettings()
	if m.dialog != nil || p.Respond(client.Answer{Action: "apply"}) {
		t.Fatal("settings Apply accepted twice or left form open")
	}
	q := client.Question{RequestQuestions: []client.RequestQuestion{{ID: "q", Required: true, InitialAnswer: "exact answer"}}}
	p2, result2, _ := intuitivePrompt(t, q)
	m.openPrompt(p2)
	assertIntuitiveNoReply(t, result2)
	m.advanceRequest(false)
	if m.dialog != nil || p2.Respond(client.Answer{RequestAction: "submit"}) {
		t.Fatal("request Submit accepted twice or left form open")
	}
}
func TestAuditInteractionFormDetailsUsesFocusedSource(t *testing.T) {
	m := testModel(t)
	p, _, _ := intuitivePrompt(t, client.Question{ModelSettings: auditSettingsReview()})
	m.openPrompt(p)
	m.handleKey(keyPress('l', tea.ModCtrl))
	if !m.dialog.details || !strings.Contains(m.dialog.detailText, "source-thread") {
		t.Fatal("Ctrl+L inspected attachment instead of source form")
	}
	m.handleKey(keyPress('l', tea.ModCtrl))
	m.openPalette()
	m.dialog.list.SetFilterText("details")
	m.dialog.filterQuery = "details"
	m.chooseDialog()
	if !m.dialog.details || !strings.Contains(m.dialog.detailText, "source-thread") {
		t.Fatal("palette Details differs from Ctrl+L")
	}
}
