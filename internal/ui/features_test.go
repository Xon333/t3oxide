package ui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/charmbracelet/x/ansi"
)

func TestModelPickerUsesExistingListAndSelectsCurrentChoice(t *testing.T) {
	m := testModel(t)
	m.composer.SetValue("human draft 你好")
	m.openPrompt(retiredPrompt(100, client.Question{Kind: client.ChooseOne, Title: "Model · Current thread", Initial: "b", Choices: []client.Choice{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}}))
	if m.dialog.list.SelectedItem().(choiceItem).id != "b" || m.composer.Value() != "human draft 你好" {
		t.Fatal("current model/draft not preserved")
	}
	m.cancelDialog()
	if m.composer.Value() != "human draft 你好" {
		t.Fatal("picker cancellation cleared composer")
	}
}
func TestNavigationShortcutsUseCorrelatedDialogAndPreserveComposer(t *testing.T) {
	for _, k := range []tea.KeyPressMsg{keyPress(' '), keyPress(tea.KeyRight), keyPress(tea.KeyLeft), keyPress(tea.KeyF6), keyPress('f', tea.ModCtrl), keyPress(tea.KeyUp, tea.ModAlt)} {
		m := testModel(t)
		m.composer.SetValue("draft")
		m.openPrompt(retiredPrompt(101, client.Question{Kind: client.ChooseOne, Navigation: true, Title: "Threads · all", Choices: []client.Choice{{ID: "root", Label: "▸ MAIN Root", Description: "2 subagents"}}}))
		m.dialogKey(k)
		if m.dialog != nil || m.composer.Value() != "draft" || !strings.Contains(m.notice, "retired") {
			t.Fatalf("navigation key was not handled locally/correlated: %s", k.String())
		}
	}
}
func TestNavigationDoesNotStealSpacesFromListFilter(t *testing.T) {
	m := testModel(t)
	m.openPrompt(retiredPrompt(102, client.Question{Kind: client.ChooseOne, Navigation: true, Title: "Threads", Choices: []client.Choice{{ID: "a", Label: "Alpha beta"}}}))
	m.dialog.list.SetFilterState(list.Filtering)
	m.dialog.list.FilterInput.SetValue("Alpha")
	m.dialogKey(keyPress(' '))
	if m.dialog == nil {
		t.Fatal("space in list filter toggled a group")
	}
}

func TestClearingPickerFilterRestoresEveryPage(t *testing.T) {
	m := testModel(t)
	var choices []client.Choice
	for i := range 30 {
		choices = append(choices, client.Choice{ID: fmt.Sprint(i), Label: fmt.Sprintf("Choice %02d", i)})
	}
	m.openPrompt(retiredPrompt(104, client.Question{Kind: client.ChooseOne, Choices: choices}))
	finishCommand(t, m, m.dialogKey(keyPress('/')))
	finishCommand(t, m, m.updateComponent("list", m.dialog.serial, tea.PasteMsg{Content: "Choice 29"}))
	if len(m.dialog.list.VisibleItems()) != 1 {
		t.Fatal("fixture filter did not narrow to one choice")
	}
	finishCommand(t, m, m.dialogKey(keyPress('u', tea.ModCtrl)))
	if len(m.dialog.list.VisibleItems()) != len(choices) || m.dialog.list.Paginator.TotalPages < 2 {
		t.Fatal("accepting the cleared filter did not restore pagination")
	}
	for range len(choices) - 1 {
		m.dialogKey(keyPress(tea.KeyDown))
	}
	if item := m.dialog.list.SelectedItem().(choiceItem); item.id != "29" {
		t.Fatalf("last matching choice is unreachable: %s", item.id)
	}
}
func TestUnavailableModelActionsAreDisabledInPalette(t *testing.T) {
	m := testModel(t)
	m.openPalette()
	for i, raw := range m.dialog.list.Items() {
		if raw.(choiceItem).command == "model" {
			m.dialog.list.Select(i)
			break
		}
	}
	item := m.dialog.list.SelectedItem().(choiceItem)
	if item.command != "model" || !item.disabled {
		t.Fatal("old/disconnected adapter masquerades as model-capable")
	}
	m.chooseDialog()
	if m.dialog == nil || !strings.Contains(m.notice, "Unavailable") {
		t.Fatal("disabled action executed")
	}
	m.dialog = nil
	m.status = client.Status{ModelControls: true, ModelCatalog: true, ThreadNavigation: true, RuntimeDetails: true, ParentAvailable: true}
	m.openPalette()
	for _, raw := range m.dialog.list.Items() {
		item := raw.(choiceItem)
		if item.disabled {
			t.Fatalf("supported action disabled: %s", item.command)
		}
	}
}
func TestFeatureHeaderAndNavigationRemainBoundedThroughResizeAndThemes(t *testing.T) {
	for _, theme := range []string{"terminal", "violet", "cyan", "lime"} {
		for _, size := range [][2]int{{18, 7}, {28, 10}, {44, 16}, {80, 24}, {160, 42}} {
			t.Run(fmt.Sprintf("%s-%dx%d", theme, size[0], size[1]), func(t *testing.T) {
				m := testModel(t)
				m.theme = theme
				m.applyTheme()
				m.status = client.Status{Context: "M1 / 工程 👩🏽‍💻 [SUBAGENT]", ModelName: "model · high effort · fast tier", NextModel: "other · low effort", State: "waiting · queue", Delivery: "queue"}
				m.composer.SetValue("unsent 界")
				m.openPrompt(retiredPrompt(103, client.Question{Kind: client.ChooseOne, Navigation: true, Title: "Threads · all", Prompt: "Space expands · parent context", Choices: []client.Choice{{ID: "child", Label: strings.Repeat("  ", 8) + "SUB child 界", Description: strings.Repeat("model waiting parent context ", 20)}}}))
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				v := m.View()
				if !v.AltScreen || !v.ReportFocus {
					t.Fatal("terminal lifecycle flags lost")
				}
				lines := strings.Split(v.Content, "\n")
				if len(lines) > size[1] {
					t.Fatal("height overflow")
				}
				for _, line := range lines {
					if ansi.StringWidth(line) > size[0] {
						t.Fatal("width overflow")
					}
				}
				if m.composer.Value() != "unsent 界" {
					t.Fatal("resize changed draft")
				}
			})
		}
	}
}
func TestModelStatusPreservesSavedSelectionWithoutInventingRuntime(t *testing.T) {
	if got := modelStatusLine("saved · high effort"); got != "saved · high effort" {
		t.Fatalf("saved preference was replaced or redundantly labeled: %q", got)
	}
	if !strings.Contains(modelStatusLine(""), "unreported") {
		t.Fatal("missing selection invented")
	}
}
