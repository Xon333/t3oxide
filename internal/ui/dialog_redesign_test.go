package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
)

func typeDialogQuery(m *Model, query string) {
	for _, r := range query {
		m.dialogKey(keyPress(r))
	}
}

func TestPaletteTypesImmediatelyAndEnterRunsCurrentResult(t *testing.T) {
	m := testModel(t)
	m.composer.SetValue("unsent draft 你好")
	m.openPalette()
	if m.dialog.list.FilterState() != list.Filtering || !m.dialog.list.FilterInput.Focused() {
		t.Fatal("palette did not open ready for typing")
	}
	var commands []string
	for _, raw := range m.dialog.list.Items() {
		commands = append(commands, raw.(choiceItem).command)
	}
	if !slices.Equal(commands[:6], []string{"model", "effort", "new", "new-same", "review", "switch"}) {
		t.Fatalf("everyday commands do not lead the palette: %v", commands[:6])
	}
	typeDialogQuery(m, "Find in current page")
	if got := m.dialog.list.SelectedItem().(choiceItem).command; got != "search" {
		t.Fatalf("typing did not filter directly: %q", got)
	}
	m.dialogKey(keyPress(tea.KeyEnter))
	if m.dialog != nil || !m.searching || m.composer.Value() != "unsent draft 你好" {
		t.Fatal("Enter did not run the selected command once while preserving the draft")
	}
}

func TestPaletteLateFilterCannotReplaceNewQueryOrItsEnterSelection(t *testing.T) {
	m := testModel(t)
	m.openPalette()
	typeDialogQuery(m, "earlier")
	// Obtain a real Bubbles async result for the old query, wrapped with the
	// current dialog identity. Modal identity alone cannot distinguish it.
	old := m.dialog.list
	late := m.componentCmd("list", m.dialog.serial, old.SetItem(0, old.Items()[0]))
	if late == nil {
		t.Fatal("fixture did not produce an asynchronous native filter")
	}
	m.dialogKey(keyPress('u', tea.ModCtrl))
	typeDialogQuery(m, "Keyboard help")
	if got := m.dialog.list.SelectedItem().(choiceItem).command; got != "help" {
		t.Fatalf("new query retained an old result: %q", got)
	}
	m.Update(late())
	if got := m.dialog.list.SelectedItem().(choiceItem).command; got != "help" {
		t.Fatalf("late native filter replaced the new query results: %q", got)
	}
	m.dialogKey(keyPress(tea.KeyEnter))
	if m.dialog != nil || m.detail == "" {
		t.Fatal("one Enter did not open help for the current query")
	}
	// A result arriving after selection also cannot touch a replacement modal.
	m.openPrompt(retiredPrompt(811, client.Question{Kind: client.ChooseOne, Choices: []client.Choice{{ID: "exact-new-id", Label: "New request"}}}))
	m.Update(late())
	if m.dialog.prompt.ID != 811 || m.dialog.list.SelectedItem().(choiceItem).id != "exact-new-id" {
		t.Fatal("late palette work changed a replacement prompt")
	}
}

func TestPaletteNoMatchesAndDisabledActionsRemainOpen(t *testing.T) {
	m := testModel(t)
	m.openPalette()
	typeDialogQuery(m, "zzzzzzzzzzzzzzzzzzzz")
	m.dialogKey(keyPress(tea.KeyEnter))
	if m.dialog == nil || m.dialog.list.SelectedItem() != nil || m.searching || m.detail != "" {
		t.Fatal("no-match Enter selected an older or unfiltered command")
	}
	m.dialogKey(keyPress('u', tea.ModCtrl))
	typeDialogQuery(m, "Model")
	item := m.dialog.list.SelectedItem().(choiceItem)
	if item.command != "model" || !item.disabled || !strings.Contains(item.description, "Unavailable") {
		t.Fatal("filter lost model capability guard or offered description")
	}
	m.dialogKey(keyPress(tea.KeyEnter))
	if m.dialog == nil || !strings.Contains(m.notice, "Unavailable") || m.actionBusy {
		t.Fatal("disabled filtered action ran or closed the palette")
	}
}

func TestFilteringArrowsAndPagesKeepQueryAndSelectionReachable(t *testing.T) {
	m := testModel(t)
	var choices []client.Choice
	for i := range 29 {
		choices = append(choices, client.Choice{ID: fmt.Sprintf("offered-id-%02d", i), Label: fmt.Sprintf("Entry %02d", i)})
	}
	p := retiredPrompt(812, client.Question{Kind: client.ChooseOne, Choices: choices})
	m.openPrompt(p)
	typeDialogQuery(m, "Entry")
	query := m.dialog.list.FilterValue()
	m.dialogKey(keyPress(tea.KeyDown))
	if m.dialog.list.Index() != 1 {
		t.Fatal("Down accepted the filter instead of moving to the next result")
	}
	m.dialogKey(keyPress(tea.KeyUp))
	if m.dialog.list.Index() != 0 {
		t.Fatal("Up did not move through the active query")
	}
	for range m.dialog.list.Paginator.TotalPages {
		m.dialogKey(keyPress(tea.KeyPgDown))
	}
	for range m.dialog.list.Paginator.PerPage {
		m.dialogKey(keyPress(tea.KeyDown))
	}
	if item := m.dialog.list.SelectedItem().(choiceItem); item.id != "offered-id-28" {
		t.Fatalf("last filtered result is unreachable: %q", item.id)
	}
	m.dialogKey(keyPress(tea.KeyPgUp))
	if m.dialog.list.Paginator.OnLastPage() || m.dialog.list.FilterState() != list.Filtering || m.dialog.list.FilterValue() != query || !m.dialog.list.FilterInput.Focused() || m.dialog.prompt != p {
		t.Fatal("page navigation changed the query, editor focus or exact prompt")
	}
}

func TestPickerTypingPreservesNavigationGroupsAndFullOfferedDetails(t *testing.T) {
	m := testModel(t)
	label := strings.Repeat("Exact offered title ", 12) + "界"
	description := strings.Repeat("Complete consequence\n", 15)
	p := retiredPrompt(813, client.Question{Kind: client.ChooseOne, Navigation: true, Initial: "child", Choices: []client.Choice{
		{ID: "root", Label: "Root"},
		{ID: "child", Label: label, Description: description},
	}})
	m.openPrompt(p)
	m.dialogKey(keyPress(tea.KeyUp))
	if m.dialog.list.SelectedItem().(choiceItem).id != "root" || m.dialog.list.FilterState() != list.Unfiltered {
		t.Fatal("unfiltered picker lost arrow navigation")
	}
	m.dialogKey(keyPress(tea.KeyDown))
	if m.dialog.list.SelectedItem().(choiceItem).id != "child" {
		t.Fatal("unfiltered picker lost arrow navigation")
	}
	typeDialogQuery(m, "Exact offered")
	m.dialogKey(keyPress(' '))
	if m.dialog == nil || m.dialog.prompt != p || m.dialog.list.FilterValue() != "Exact offered " {
		t.Fatal("typing a query space toggled a navigation group")
	}
	m.dialogKey(keyPress(tea.KeyF2))
	if !m.dialog.details || !strings.Contains(m.dialog.detailText, label) || !strings.Contains(m.dialog.detailText, description) || m.dialog.list.FilterInput.Focused() {
		t.Fatal("F2 lost the full offered text or left the query editor active")
	}
	m.dialogKey(keyPress(tea.KeyEscape))
	if m.dialog == nil || m.dialog.details || !m.dialog.list.FilterInput.Focused() {
		t.Fatal("closing F2 details retired the exact prompt or lost query focus")
	}
	m.cancelDialog()
	m.openPrompt(p)
	m.dialogKey(keyPress(' '))
	if m.dialog != nil || !strings.Contains(m.notice, "retired") {
		t.Fatal("unfiltered Space no longer acts on the offered navigation group")
	}
}

func TestFilteredChooseManyEnterTogglesExactAnswerAndKeepsEditing(t *testing.T) {
	m := testModel(t)
	p := retiredPrompt(814, client.Question{Kind: client.ChooseMany, Choices: []client.Choice{{ID: "a", Label: "Alpha"}, {ID: "z", Label: "Zebra"}}})
	m.openPrompt(p)
	typeDialogQuery(m, "Zebra")
	m.dialogKey(keyPress(tea.KeyEnter))
	if m.dialog == nil || m.dialog.prompt != p || !m.dialog.selected["z"] || m.dialog.selected["a"] || m.dialog.list.FilterState() != list.Filtering {
		t.Fatal("one Enter did not toggle the exact filtered answer")
	}
	if item := m.dialog.list.SelectedItem().(choiceItem); !item.checked || item.id != "z" {
		t.Fatal("checked master item was not synchronously reflected in filtered results")
	}
	m.dialogKey(keyPress(tea.KeyEnter))
	if m.dialog.selected["z"] {
		t.Fatal("second Enter did not toggle the same choice off")
	}
	m.dialogKey(keyPress('u', tea.ModCtrl))
	m.dialogKey(keyPress(tea.KeyDown))
	m.dialogKey(keyPress(tea.KeyEnter))
	if !m.dialog.selected["z"] || m.dialog.selected["a"] {
		t.Fatal("empty-query filtered index changed the wrong master choice")
	}
	m.dialogKey(keyPress(tea.KeyEnter, tea.ModCtrl))
	if m.dialog != nil || !strings.Contains(m.notice, "retired") {
		t.Fatal("Ctrl+Enter stopped completing choose-many through the exact prompt")
	}
}

func TestQueryEditingKeepsCursorAndClearingRestoresAllResults(t *testing.T) {
	m := testModel(t)
	m.openPalette()
	typeDialogQuery(m, "hep")
	m.dialogKey(keyPress(tea.KeyLeft))
	m.dialogKey(keyPress('l'))
	if m.dialog.list.FilterValue() != "help" {
		t.Fatalf("query edit moved its cursor to the end: %q", m.dialog.list.FilterValue())
	}
	if m.dialog.list.SelectedItem().(choiceItem).command != "help" {
		t.Fatal("middle-of-query edit did not compute the current matches")
	}
	m.dialogKey(keyPress('u', tea.ModCtrl))
	// Ctrl+U is the native editor's delete-to-start, so clear the suffix too.
	m.dialogKey(keyPress('k', tea.ModCtrl))
	if m.dialog.list.FilterValue() != "" || len(m.dialog.list.VisibleItems()) != len(m.dialog.list.Items()) {
		t.Fatal("clearing the query did not synchronously restore every result")
	}
}
