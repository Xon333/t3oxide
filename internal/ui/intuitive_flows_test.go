package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/charmbracelet/x/ansi"
)

var flowSizes = [][2]int{{60, 16}, {80, 24}, {120, 35}, {170, 40}}

// Locate the visible label in the real composed View, independently of the
// hit-zone implementation. All input enters the production top-level router.
func flowPoint(t *testing.T, m *Model, label string) tea.MouseClickMsg {
	t.Helper()
	view := ansi.Strip(m.View().Content)
	for y, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "Filter: ") {
			continue // the query can repeat a result label without being its row
		}
		if index := strings.Index(line, label); index >= 0 {
			return tea.MouseClickMsg{X: ansi.StringWidth(line[:index]), Y: y, Button: tea.MouseLeft}
		}
	}
	t.Fatalf("visible control %q not found:\n%s", label, view)
	return tea.MouseClickMsg{}
}

func flowClick(t *testing.T, m *Model, label string) tea.Cmd {
	t.Helper()
	_, cmd := m.Update(flowPoint(t, m, label))
	return cmd
}

func flowAwaitReply(t *testing.T, reply <-chan error) {
	t.Helper()
	// This UI-only broker originally asked an environment question. It proves
	// explicit reply/lifetime, not payload validity; the program fixtures below
	// this package verify exact native payloads and dispatch counts.
	select {
	case <-reply:
	case <-time.After(2 * time.Second):
		t.Fatal("explicit action did not answer its live prompt")
	}
}

func TestIntuitiveFlowLaunchMouseFieldsAndActions(t *testing.T) {
	for _, size := range flowSizes {
		for _, mode := range []string{"developer", "agentbox"} {
			t.Run(fmt.Sprintf("%s/%dx%d", mode, size[0], size[1]), func(t *testing.T) {
				m := testModel(t)
				m.mode, m.mouse = mode, true
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				m.composer.SetValue("source draft stays exact\n  ")
				q := launchQuestion(client.LaunchInput{Title: "Literal title", Message: "  first\nsecond  "})
				p, reply, _ := intuitivePrompt(t, q)
				m.openPrompt(p)
				m.restoreFocus()
				flowClick(t, m, "Title  ")
				if m.dialog == nil || m.dialog.launch.focus != 1 || !m.launchTitle.Focused() {
					t.Fatal("clicking Title did not focus its native editor")
				}
				assertIntuitiveNoReply(t, reply)
				flowClick(t, m, "First message")
				if m.dialog.launch.focus != 0 || !m.modalInput.Focused() {
					t.Fatal("clicking First message did not focus its native editor")
				}
				if m.launchInput() != q.LaunchReview.Input || m.currentDraft() != "source draft stays exact\n  " {
					t.Fatal("mouse focus changed canonical launch/source input")
				}
				flowClick(t, m, "Create")
				if m.dialog != nil {
					t.Fatal("one explicit Create click did not answer the reviewed launch")
				}
				awaitIntuitiveReply(t, reply)
			})
		}
	}
}

func TestIntuitiveFlowLaunchActionArrowsClampAndEditorsOwnArrows(t *testing.T) {
	for _, size := range flowSizes {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := testModel(t)
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			q := launchQuestion(client.LaunchInput{Title: "Title", Message: "first\nsecond"})
			q.LaunchReview.Actions = append(q.LaunchReview.Actions, "workspace", "defaults", "project", "environment")
			p, reply, _ := intuitivePrompt(t, q)
			m.openPrompt(p)
			m.restoreFocus()
			m.Update(keyPress(tea.KeyUp))
			if m.dialog.launch.focus != 0 || m.modalInput.Line() != 0 {
				t.Fatal("message Up did not remain native editing")
			}
			m.Update(keyPress(tea.KeyTab))
			before := m.launchTitle.Position()
			m.Update(keyPress(tea.KeyLeft))
			if m.dialog.launch.focus != 1 || m.launchTitle.Position() != before-1 {
				t.Fatal("title Left did not remain native editing")
			}
			m.Update(keyPress(tea.KeyEnter))
			if m.dialog.launch.focus != 2 {
				t.Fatal("Title Enter did not focus Create")
			}
			actions := m.launchActions()
			for i := 1; i < len(actions); i++ {
				m.Update(keyPress(tea.KeyRight))
				if m.dialog.launch.focus != i+2 {
					t.Fatalf("Right failed to focus action %q", actions[i])
				}
				if view := ansi.Strip(m.View().Content); !strings.Contains(view, "["+m.launchActionLabel(actions[i])+"]") {
					t.Fatalf("focused wrapped action %q is hidden:\n%s", actions[i], view)
				}
				assertIntuitiveNoReply(t, reply)
			}
			m.Update(keyPress(tea.KeyDown))
			if m.dialog.launch.focus != len(actions)+1 {
				t.Fatal("last action arrow wrapped")
			}
			for range len(actions) + 2 {
				m.Update(keyPress(tea.KeyUp))
			}
			if m.dialog.launch.focus != 2 {
				t.Fatal("previous action arrows failed to clamp at Create")
			}
			m.Update(keyPress(tea.KeyTab, tea.ModShift))
			if m.dialog.launch.focus != 1 {
				t.Fatal("complete Tab order changed")
			}
			assertIntuitiveNoReply(t, reply)
		})
	}
}

func TestIntuitiveFlowSettingsMouseStagesAndExplicitApply(t *testing.T) {
	for _, size := range flowSizes {
		for _, mode := range []string{"developer", "agentbox"} {
			t.Run(fmt.Sprintf("%s/%dx%d", mode, size[0], size[1]), func(t *testing.T) {
				m := testModel(t)
				m.mode, m.mouse = mode, true
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				p, reply, _ := intuitivePrompt(t, client.Question{ModelSettings: auditSettingsReview()})
				m.openPrompt(p)
				m.restoreFocus()
				source := m.dialog
				flowClick(t, m, "Effort  ")
				if m.dialog == source || m.dialog.formSource != source {
					t.Fatal("one option click did not open the staged chooser")
				}
				flowClick(t, m, "High")
				if m.dialog != source {
					t.Fatal("choice did not restore its settings owner")
				}
				value, _ := source.settingsForm.selection.Option("reasoningEffort")
				if value != "high" {
					t.Fatalf("choice staged wrong value: %v", value)
				}
				assertIntuitiveNoReply(t, reply)
				flowClick(t, m, "Apply changes")
				if m.dialog != nil {
					t.Fatal("Apply click did not answer its exact settings prompt")
				}
				awaitIntuitiveReply(t, reply)
			})
		}
	}
}

func TestIntuitiveFlowRequestMouseStagesCustomBackNextAndSubmit(t *testing.T) {
	for _, size := range flowSizes {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := testModel(t)
			m.mouse = true
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			p, reply, _ := intuitivePrompt(t, client.Question{Kind: client.EnterText, Title: "Exact request", RequestQuestions: []client.RequestQuestion{
				{ID: "choice", Question: "Choose the literal offered value", Required: true, AllowCustomAnswer: true, Options: []client.RequestOption{{Label: "First offered", Value: "opaque-first"}, {Label: "Second offered", Value: "opaque-second"}}},
				{ID: "text", Question: "Explain", Required: true},
			}})
			m.openPrompt(p)
			m.restoreFocus()
			flowClick(t, m, "Second offered")
			if m.dialog.requestForm.answers["choice"] != "opaque-second" {
				t.Fatal("option click did not stage the exact offered value")
			}
			assertIntuitiveNoReply(t, reply)
			flowClick(t, m, "Custom answer")
			m.Update(tea.PasteMsg{Content: "  custom literal  "})
			flowClick(t, m, "Next")
			if m.dialog.requestForm.index != 1 || m.dialog.requestForm.answers["choice"] != "  custom literal  " {
				t.Fatal("Next did not save the custom editor before moving focus")
			}
			m.Update(tea.PasteMsg{Content: "  exact\ntext  "})
			flowClick(t, m, "Back")
			if m.dialog.requestForm.index != 0 || m.dialog.requestForm.answers["text"] != "  exact\ntext  " {
				t.Fatal("Back lost the exact text answer")
			}
			flowClick(t, m, "Next")
			assertIntuitiveNoReply(t, reply)
			flowClick(t, m, "Submit answers")
			if m.dialog != nil {
				t.Fatal("explicit final Submit click did not answer")
			}
			flowAwaitReply(t, reply)
		})
	}
}

func TestIntuitiveFlowOrdinaryTextMouseControls(t *testing.T) {
	for _, size := range flowSizes {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := testModel(t)
			m.mouse = true
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			p, reply, _ := intuitivePrompt(t, client.Question{Kind: client.EnterText, Title: "Answer exactly", Prompt: "Provide the requested value", Initial: "  literal\nanswer  "})
			m.openPrompt(p)
			m.restoreFocus()
			flowClick(t, m, "literal")
			if !m.modalInput.Focused() || m.modalInput.Value() != "  literal\nanswer  " {
				t.Fatal("ordinary answer click changed input/focus")
			}
			assertIntuitiveNoReply(t, reply)
			flowClick(t, m, "Submit")
			if m.dialog != nil {
				t.Fatal("explicit text Submit click did not answer")
			}
			flowAwaitReply(t, reply)
		})
	}
}

func TestIntuitiveFlowAgentBoxDecisionMouseAndMouseOff(t *testing.T) {
	for _, size := range flowSizes {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := testModel(t)
			seedAgentBox(m)
			m.mouse = true
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m.composer.SetValue("  unsent reply\n  ")
			flowClick(t, m, "2 Reply")
			if !m.agentbox.reply || m.currentDraft() != "  unsent reply\n  " || m.actionBusy {
				t.Fatal("one Reply click did not enter the exact editor without sending")
			}
			m.Update(keyPress(tea.KeyEscape))
			flowClick(t, m, "3 Reviewed")
			if m.dialog == nil || m.dialog.local != "inbox-review" || m.actionBusy {
				t.Fatal("Reviewed click did not enter the existing native disposition review")
			}
			m.Update(keyPress(tea.KeyEscape))
			m.agentbox.decision, m.mouse = false, false
			flowClick(t, m, "Review export flow")
			if m.agentbox.decision || m.agentbox.reply || m.dialog != nil {
				t.Fatal("AgentBox acted on a click with mouse capture disabled")
			}
		})
	}
}
