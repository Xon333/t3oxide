package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/charmbracelet/x/ansi"
)

func TestIntuitiveFlowFormMouseRejectsStaleFramesAndRetiredPrompts(t *testing.T) {
	forms := []struct {
		name, label string
		question    func() client.Question
	}{
		{"launch", "Create", func() client.Question {
			return launchQuestion(client.LaunchInput{Title: "Exact launch", Message: "exact\ninput"})
		}},
		{"settings", "Apply changes", func() client.Question { return client.Question{ModelSettings: auditSettingsReview()} }},
		{"request", "Submit answers", func() client.Question {
			return client.Question{Kind: client.EnterText, Title: "Exact request", RequestQuestions: []client.RequestQuestion{{ID: "answer", Question: "Exact answer", Required: true, InitialAnswer: "exact"}}}
		}},
		{"text", "Submit", func() client.Question {
			return client.Question{Kind: client.EnterText, Title: "Exact input", Initial: "exact"}
		}},
	}
	for _, form := range forms {
		for _, change := range []string{"resize", "generation", "replacement", "mode", "retired", "mouse-off", "details"} {
			t.Run(form.name+"/"+change, func(t *testing.T) {
				m := testModel(t)
				m.mouse = true
				m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
				m.composer.SetValue("underlying exact draft")
				p, reply, cancel := intuitivePrompt(t, form.question())
				m.openPrompt(p)
				m.restoreFocus()
				oldClick := flowPoint(t, m, form.label)
				source := m.dialog
				var replacement <-chan error
				switch change {
				case "resize":
					m.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
				case "generation":
					m.generation++
				case "replacement":
					newPrompt, result, _ := intuitivePrompt(t, form.question())
					replacement = result
					m.openPrompt(newPrompt)
					source = m.dialog
				case "mode":
					m.setMode("agentbox")
				case "retired":
					cancel()
					select {
					case <-p.Done:
					case <-time.After(time.Second):
						t.Fatal("request did not retire")
					}
				case "mouse-off":
					m.mouse = false
				case "details":
					m.Update(keyPress('l', tea.ModCtrl))
				}
				m.Update(oldClick)
				if m.dialog != source || m.currentDraft() != "underlying exact draft" || m.actionBusy || m.browserBusy {
					t.Fatal("old form click changed/answered a source or activated hidden workspace")
				}
				if change != "retired" {
					assertIntuitiveNoReply(t, reply)
				}
				if replacement != nil {
					assertIntuitiveNoReply(t, replacement)
				}
			})
		}
	}
}

func TestIntuitiveFlowSettingsScrollNestedReturnAndCatalogIdentity(t *testing.T) {
	for _, size := range flowSizes {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := testModel(t)
			m.mouse = true
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			review := auditSettingsReview()
			for i := 0; i < 28; i++ {
				review.Catalog.Models[0].Options = append(review.Catalog.Models[0].Options, client.OptionDescriptor{ID: fmt.Sprintf("option-%02d", i), Label: fmt.Sprintf("Field %02d 界\nlabel", i), Type: "boolean"})
			}
			p, reply, _ := intuitivePrompt(t, client.Question{ModelSettings: review})
			m.openPrompt(p)
			source := m.dialog
			old := flowPoint(t, m, "Model  ")
			for range 28 {
				m.Update(keyPress(tea.KeyDown))
			}
			m.Update(old)
			if m.dialog != source {
				t.Fatal("a scrolled-out field remained active at its former coordinates")
			}
			flowClick(t, m, "Field 25 界 label")
			if m.dialog == source || m.dialog.formSource != source {
				t.Fatal("the rendered scrolled field did not open its chooser")
			}
			flowClick(t, m, "On")
			if m.dialog != source {
				t.Fatal("nested chooser lost its exact form owner")
			}
			if value, ok := source.settingsForm.selection.Option("option-25"); !ok || value != true {
				t.Fatal("scrolled mouse choice edited another option")
			}
			m.Update(keyPress(tea.KeyDown))
			m.Update(keyPress(tea.KeyDown))
			m.Update(keyPress(tea.KeyDown))
			apply := flowPoint(t, m, "Apply changes")
			source.settingsForm.selection = source.settingsForm.selection.WithOption("option-25", false)
			m.Update(apply)
			if m.dialog != source {
				t.Fatal("old Apply geometry accepted an unseen changed selection")
			}
			assertIntuitiveNoReply(t, reply)
			m.Update(keyPress(tea.KeyDown)) // reveal the final Cancel row
			flowClick(t, m, "Cancel")
			flowAwaitReply(t, reply)
		})
	}
}

func TestIntuitiveFlowRequestClippedOptionsMultiSelectAndClearedCustom(t *testing.T) {
	for _, size := range flowSizes {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := testModel(t)
			m.mouse = true
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			q := client.RequestQuestion{ID: "many", Required: true, MultiSelect: true, AllowCustomAnswer: true, Question: strings.Repeat("Wrapped question 界. ", 20)}
			for i := 0; i < 22; i++ {
				q.Options = append(q.Options, client.RequestOption{Label: fmt.Sprintf("Choice %02d 界", i), Value: fmt.Sprintf("opaque/%02d", i)})
			}
			p, reply, _ := intuitivePrompt(t, client.Question{Kind: client.EnterText, Title: "Multiple", RequestQuestions: []client.RequestQuestion{q}})
			m.openPrompt(p)
			m.restoreFocus()
			old := flowPoint(t, m, "Choice 00 界")
			for range 21 {
				m.Update(keyPress(tea.KeyDown))
			}
			m.Update(old)
			if requestAnswered(m.dialog.requestForm.answers["many"]) {
				t.Fatal("hidden option was active after request scroll")
			}
			flowClick(t, m, "Choice 21 界")
			flowClick(t, m, "Choice 20 界")
			if got, _ := m.dialog.requestForm.answers["many"].([]string); !slices.Equal(got, []string{"opaque/21", "opaque/20"}) {
				t.Fatalf("multi-select did not toggle exact visible values: %v", got)
			}
			// Keyboard Tab reveals the offscreen custom editor; the visible
			// label then focuses the same editor for a mouse-only continuation.
			m.Update(keyPress(tea.KeyTab))
			flowClick(t, m, "Custom answer")
			m.Update(tea.PasteMsg{Content: "  custom  "})
			m.Update(keyPress(tea.KeyTab))
			m.Update(keyPress(tea.KeyTab, tea.ModShift))
			m.modalInput.SelectAll()
			m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
			m.Update(keyPress(tea.KeyTab))
			if got, _ := m.dialog.requestForm.answers["many"].([]string); !slices.Equal(got, []string{"opaque/21", "opaque/20"}) {
				t.Fatalf("clearing custom text did not retain only offered selections: %v", got)
			}
			flowClick(t, m, "Submit answers")
			if m.dialog != nil {
				t.Fatal("clearing only custom text erased required offered selections")
			}
			flowAwaitReply(t, reply)
		})
	}
}

func TestIntuitiveFlowRequestRequiredCustomClearAndOldNext(t *testing.T) {
	m := testModel(t)
	m.mouse = true
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	p, reply, _ := intuitivePrompt(t, client.Question{Kind: client.EnterText, Title: "Required", RequestQuestions: []client.RequestQuestion{
		{ID: "first", Question: "Required first", Required: true, AllowCustomAnswer: true, Options: []client.RequestOption{{Label: "Literal offered", Value: "literal-id"}}},
		{ID: "second", Question: "Optional second"},
	}})
	m.openPrompt(p)
	flowClick(t, m, "Next")
	if m.dialog.requestForm.index != 0 || m.dialog.requestForm.problem != "An answer is required" {
		t.Fatal("Next bypassed required validation")
	}
	flowClick(t, m, "Custom answer")
	m.Update(tea.PasteMsg{Content: "custom"})
	m.Update(keyPress(tea.KeyTab))
	m.Update(keyPress(tea.KeyTab, tea.ModShift))
	m.modalInput.SelectAll()
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.modalInput.Value() != "" || m.dialog.requestForm.focus != 1 {
		t.Fatalf("custom clear did not reach editor: focus=%d input=%q selected=%q", m.dialog.requestForm.focus, m.modalInput.Value(), m.modalInput.SelectedText())
	}
	flowClick(t, m, "Next")
	if m.dialog.requestForm.index != 0 || requestAnswered(m.dialog.requestForm.answers["first"]) {
		t.Fatalf("cleared custom answer was submitted as a hidden value: index=%d answers=%#v custom=%#v input=%q", m.dialog.requestForm.index, m.dialog.requestForm.answers, m.dialog.requestForm.custom, m.modalInput.Value())
	}
	flowClick(t, m, "Literal offered")
	oldNext := flowPoint(t, m, "Next")
	m.Update(oldNext)
	if m.dialog.requestForm.index != 1 || m.dialog.requestForm.answers["first"] != "literal-id" {
		t.Fatal("Next lost exact staged answer")
	}
	m.Update(oldNext) // no new render/offer yet
	if m.dialog == nil || m.dialog.requestForm.index != 1 {
		t.Fatal("old Next click became final Submit on the new question")
	}
	assertIntuitiveNoReply(t, reply)
	flowClick(t, m, "Submit answers")
	flowAwaitReply(t, reply)
}

func TestIntuitiveFlowPreservedTextCannotSubmitAndCancelIsReachable(t *testing.T) {
	m := testModel(t)
	m.mouse = true
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	const exact = "  original\tCRLF\r\n�  "
	p, reply, _ := intuitivePrompt(t, client.Question{Kind: client.EnterText, Title: "Original", Initial: exact})
	m.openPrompt(p)
	flowClick(t, m, "Submit")
	if m.dialog == nil || m.dialog.preservedInitial != exact {
		t.Fatal("disabled Submit consumed preserved exact input")
	}
	assertIntuitiveNoReply(t, reply)
	flowClick(t, m, "Cancel")
	if m.dialog != nil {
		t.Fatal("preserved input has no mouse Cancel")
	}
	flowAwaitReply(t, reply)
}

func TestIntuitiveFlowApprovalStagesExactChoiceBeforeSubmit(t *testing.T) {
	for _, size := range flowSizes {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := testModel(t)
			m.mouse = true
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			p, reply, _ := intuitivePrompt(t, client.Question{Kind: client.ChooseOne, ConfirmChoice: true, Title: "Approval", Prompt: "Review exactly", Choices: []client.Choice{{ID: "acceptForSession", Label: "This session", Description: "Complete offered consequence"}, {ID: "decline", Label: "Decline"}}})
			m.openPrompt(p)
			flowClick(t, m, "This session")
			if m.dialog == nil || m.dialog.requestForm.answers["decision"] != "acceptForSession" {
				t.Fatal("approval click bypassed local exact-choice staging")
			}
			assertIntuitiveNoReply(t, reply)
			m.Update(keyPress(']', tea.ModCtrl))
			assertIntuitiveNoReply(t, reply)
			m.Update(keyPress('l', tea.ModCtrl))
			if !strings.Contains(m.dialog.detailText, "Complete offered consequence") {
				t.Fatal("approval lost the offered consequence in full Info")
			}
			m.Update(keyPress(tea.KeyEscape))
			flowClick(t, m, "Submit decision")
			flowAwaitReply(t, reply)
		})
	}
}

func TestIntuitiveFlowApprovalWarningIsVisibleAndStagesItsExactOption(t *testing.T) {
	for _, size := range flowSizes {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := testModel(t)
			m.mouse = true
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			const warning = "Allows future commands without another approval."
			p, reply, _ := intuitivePrompt(t, client.Question{
				Kind: client.ChooseOne, ConfirmChoice: true, Title: "Approval", Prompt: "Review exactly",
				Choices: []client.Choice{{ID: "acceptAlways", Label: "Always allow", Description: warning}, {ID: "decline", Label: "Decline"}},
			})
			m.openPrompt(p)
			view := ansi.Strip(m.View().Content)
			if !strings.Contains(view, warning) {
				t.Fatalf("approval warning is missing from the decision screen:\n%s", view)
			}
			flowClick(t, m, warning)
			if m.dialog == nil || m.dialog.requestForm.answers["decision"] != "acceptAlways" {
				t.Fatal("warning click did not stage its exact offered option")
			}
			assertIntuitiveNoReply(t, reply)
			flowClick(t, m, "Submit decision")
			flowAwaitReply(t, reply)
		})
	}
}

func TestIntuitiveFlowWrappedApprovalWarningStaysVisibleAtSubmit(t *testing.T) {
	for _, size := range flowSizes {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := testModel(t)
			m.mouse = true
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			p, reply, _ := intuitivePrompt(t, client.Question{
				Kind: client.ChooseOne, ConfirmChoice: true, Title: "Approval", Prompt: "Review exactly",
				Choices: []client.Choice{
					{ID: "decline", Label: "Decline"},
					{ID: "acceptAlways", Label: "Always allow", Description: "Permanent permission. Future commands may run without another approval. Review this scope before granting it."},
				},
			})
			m.openPrompt(p)
			m.Update(keyPress(tea.KeyDown))
			flowClick(t, m, "Permanent permission.")
			m.Update(keyPress(tea.KeyTab))
			view := ansi.Strip(m.View().Content)
			for _, want := range []string{"✓ Always allow", "Permanent permission.", "granting it.", "[Submit decision]"} {
				if !strings.Contains(view, want) {
					t.Fatalf("selected consequence %q is hidden at Submit:\n%s", want, view)
				}
			}
			if m.dialog.requestForm.answers["decision"] != "acceptAlways" {
				t.Fatal("wrapped warning click staged a different decision")
			}
			assertIntuitiveNoReply(t, reply)
		})
	}
}

func TestIntuitiveFlowAgentBoxRejectsChangedOffersAndStaleGeometry(t *testing.T) {
	for _, change := range []string{"request", "revision", "selection", "loading", "resize", "generation", "mode", "mouse-off"} {
		t.Run(change, func(t *testing.T) {
			m := testModel(t)
			seedAgentBox(m)
			m.mouse = true
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			m.composer.SetValue("exact underlying reply")
			old := flowPoint(t, m, "3 Reviewed")
			row, _ := m.selectedInbox()
			switch change {
			case "request":
				m.status.Requests = 1 // old Reviewed must not become Later
			case "revision":
				m.agentbox.rows[0].Target.Revision = "changed"
			case "selection":
				m.agentbox.selected = m.agentbox.rows[1].Target.Key()
			case "loading":
				m.agentbox.pendingSources = map[string]bool{row.Target.ProfileID: true}
			case "resize":
				m.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
			case "generation":
				m.generation++
			case "mode":
				m.setMode("developer")
			case "mouse-off":
				m.mouse = false
			}
			m.Update(old)
			if m.dialog != nil || m.actionBusy || m.agentbox.offer != nil || m.agentbox.reply || m.currentDraft() != "exact underlying reply" {
				t.Fatal("stale AgentBox decision click changed current source")
			}
			if change == "request" {
				flowClick(t, m, "3 Later")
				if m.dialog == nil || m.dialog.local != "inbox-later" {
					t.Fatal("fresh Later choice was not reachable")
				}
			}
		})
	}
}

func TestIntuitiveFlowAgentBoxRowsReorderAndSummaryTextAreInert(t *testing.T) {
	m := testModel(t)
	seedAgentBox(m)
	m.mouse = true
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 35})
	old := flowPoint(t, m, "Review export flow")
	selected := m.agentbox.selected
	m.agentbox.rows[0], m.agentbox.rows[1] = m.agentbox.rows[1], m.agentbox.rows[0]
	m.Update(old)
	if m.agentbox.selected != selected || m.browserBusy {
		t.Fatal("row reordering retargeted an old click")
	}
	flowClick(t, m, "selected destination")
	if m.dialog != nil || m.actionBusy || m.agentbox.reply || m.agentbox.reviewLoading {
		t.Fatal("summary text activated a decision")
	}
	for _, hit := range m.hitZones {
		if hit.x < 0 || hit.y < 0 || hit.x+hit.width > m.width || hit.y+hit.height > m.height {
			t.Fatal("offscreen AgentBox target")
		}
	}
}

func TestIntuitiveFlowPaletteClickUsesCurrentFilteredOffer(t *testing.T) {
	m := testModel(t)
	m.mouse = true
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	m.composer.SetValue("retained palette draft")
	m.Update(keyPress('p', tea.ModCtrl))
	m.Update(tea.PasteMsg{Content: "Help"})
	old := flowPoint(t, m, "Keyboard help")
	m.Update(tea.PasteMsg{Content: "no-such-command"})
	m.Update(old)
	if m.dialog == nil || m.dialog.local != "palette" || m.detail != "" {
		t.Fatal("filtered-away palette row remained actionable")
	}
	m.dialog.list.FilterInput.SetValue("Help")
	m.dialog.refilter(false)
	m.resize()
	flowClick(t, m, "Keyboard help")
	if !strings.Contains(ansi.Strip(m.View().Content), "Keyboard and commands") || m.currentDraft() != "retained palette draft" {
		t.Fatal("palette mouse activation differs from current offered Help")
	}
}

func TestIntuitiveFlowWheelRevealsOffscreenControlsWithoutActivating(t *testing.T) {
	for _, size := range flowSizes {
		for _, mode := range []string{"developer", "agentbox"} {
			t.Run(fmt.Sprintf("%s/%dx%d", mode, size[0], size[1]), func(t *testing.T) {
				m := testModel(t)
				m.mode, m.mouse = mode, true
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				m.composer.SetValue("underlying exact draft")
				review := auditSettingsReview()
				for i := range 28 {
					review.Catalog.Models[0].Options = append(review.Catalog.Models[0].Options, client.OptionDescriptor{ID: fmt.Sprintf("field-%02d", i), Label: fmt.Sprintf("Field %02d", i), Type: "boolean"})
				}
				p, reply, _ := intuitivePrompt(t, client.Question{ModelSettings: review})
				m.openPrompt(p)
				for range 40 {
					point := flowPoint(t, m, "Current thread")
					m.Update(tea.MouseWheelMsg{X: point.X, Y: point.Y, Button: tea.MouseWheelDown})
				}
				f := m.dialog.settingsForm
				if f.focus != f.optionCount()+2 || !f.selection.Equal(review.Selection) {
					t.Fatal("wheel did not reveal final control without changing offered settings")
				}
				assertIntuitiveNoReply(t, reply)
				cancel := flowPoint(t, m, "Cancel")
				m.mouse = false
				m.Update(tea.MouseWheelMsg{X: cancel.X, Y: cancel.Y, Button: tea.MouseWheelUp})
				if f.focus != f.optionCount()+2 {
					t.Fatal("mouse-off allowed wheel focus movement")
				}
				m.mouse = true
				m.Update(cancel)
				flowAwaitReply(t, reply)

				q := client.RequestQuestion{ID: "long", Question: "Long choices", Required: true, AllowCustomAnswer: true}
				for i := range 24 {
					q.Options = append(q.Options, client.RequestOption{Label: fmt.Sprintf("Option %02d", i), Value: fmt.Sprintf("opaque-%02d", i)})
				}
				p, reply, _ = intuitivePrompt(t, client.Question{Kind: client.EnterText, Title: "Question", RequestQuestions: []client.RequestQuestion{q}})
				m.openPrompt(p)
				for range 23 {
					point := flowPoint(t, m, "Question 1/1")
					m.Update(tea.MouseWheelMsg{X: point.X, Y: point.Y, Button: tea.MouseWheelDown})
				}
				if m.dialog.requestForm.option != 23 || requestAnswered(m.dialog.requestForm.answers["long"]) {
					t.Fatal("wheel selected an answer or failed to reveal the last option")
				}
				flowClick(t, m, "Option 23")
				if m.dialog.requestForm.answers["long"] != "opaque-23" {
					t.Fatal("revealed last option did not stage its exact value")
				}
				point := flowPoint(t, m, "Question 1/1")
				m.Update(tea.MouseWheelMsg{X: point.X, Y: point.Y, Button: tea.MouseWheelDown})
				flowClick(t, m, "Custom answer")
				m.Update(tea.PasteMsg{Content: "  exact custom\nsecond line  "})
				point = flowPoint(t, m, "Custom answer")
				m.Update(tea.MouseWheelMsg{X: point.X, Y: point.Y, Button: tea.MouseWheelUp})
				if m.dialog.requestForm.focus != 1 || m.modalInput.Value() != "  exact custom\nsecond line  " {
					t.Fatal("wheel over focused editor stole ownership or changed text")
				}
				assertIntuitiveNoReply(t, reply)
				flowClick(t, m, "Cancel")
				flowAwaitReply(t, reply)
				if m.currentDraft() != "underlying exact draft" || m.actionBusy {
					t.Fatal("form pointer navigation affected underlying workspace")
				}
			})
		}
	}
}
