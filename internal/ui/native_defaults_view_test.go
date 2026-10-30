package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/charmbracelet/x/ansi"
)

func defaultsViewQuestion() client.Question {
	return client.Question{Surface: client.SurfaceNativeDefaults, Kind: client.ChooseOne, Title: "Native defaults · exact environment",
		Prompt:  "Scope: Project on environment\nEnvironment: exact-environment\nProject: Exact project · project-a\n\nEditing unavailable: Native server.updateSettings has no revision/CAS or durable operation ID.\nNative Settings: select this exact environment and project\n\ndefaultModelSelection\nSource: project\n{\n  \"instanceId\": \"saved-instance\",\n  \"model\": \"saved-model\",\n  \"options\": [{\"id\": \"retained-opaque-option\", \"value\": 9007199254740991}]\n}\nProject override. Reset would remove only this field and follow: null\n\ndefaultRuntimeMode\nSource: environment\n\"approval-required\"\n\ndefaultThreadEnvMode\nSource: environment\n\"worktree\"\n\nSaved selection unavailable: exact saved instance is disabled.\nDesktop-local defaults: unreported\nRevision: exact-read-revision\nThis-launch controls remain separate; they do not save persistent defaults.",
		Choices: []client.Choice{{ID: "scope", Label: "Environment"}, {ID: "refresh", Label: "Refresh"}, {ID: "close", Label: "Back"}}}
}

func TestNativeDefaultsViewerExposesFullValuesAndLimitationAtEverySize(t *testing.T) {
	sizes := append(append([][2]int(nil), flowSizes...), [2]int{40, 12})
	for _, size := range sizes {
		for _, full := range []bool{false, true} {
			t.Run(fmt.Sprintf("%dx%d/full=%v", size[0], size[1], full), func(t *testing.T) {
				m := testModel(t)
				m.mouse, m.launchPage = true, full
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				m.composer.SetValue("exact suspended human draft\n  ")
				p, reply, _ := intuitivePrompt(t, defaultsViewQuestion())
				m.openPrompt(p)
				m.restoreFocus()
				initial := ansi.Strip(m.View().Content)
				for _, want := range []string{"Read only", "[Environment]", "[Refresh]", "[Back]"} {
					if !strings.Contains(initial, want) {
						t.Fatalf("missing persistent inspection/control %q:\n%s", want, initial)
					}
				}
				if size[0] >= 120 && size[1] >= 35 && (!strings.Contains(initial, "defaultModelSelection") || !strings.Contains(initial, "revision/CAS")) {
					t.Fatalf("roomy surface still hides values or the native limitation:\n%s", initial)
				}
				m.Update(keyPress('a'))
				m.Update(tea.PasteMsg{Content: "must not filter or overwrite a draft"})
				m.Update(keyPress(tea.KeyEnter))
				m.nativeDefaultsAction("unoffered-write")
				assertIntuitiveNoReply(t, reply)
				if m.dialog.list.FilterValue() != "" || m.currentDraft() != "exact suspended human draft\n  " {
					t.Fatal("read-only inspection consumed text input")
				}
				var observed strings.Builder
				for range 80 {
					m.View()
					// The maintained viewport wraps long JSON keys at terminal
					// cells. Compare the actual visible region across soft breaks,
					// rather than requiring a long token on one compact row.
					observed.WriteString(strings.Join(strings.Fields(ansi.Strip(m.questionViewport.View())), ""))
					observed.WriteByte('\n')
					if m.questionViewport.AtBottom() {
						break
					}
					m.Update(keyPress(tea.KeyDown))
				}
				for _, want := range []string{"defaultModelSelection", "saved-instance", "retained-opaque-option", "9007199254740991", "Source: project", "Source: environment", "Desktop-local defaults: unreported", "exact-read-revision"} {
					if !strings.Contains(observed.String(), strings.Join(strings.Fields(want), "")) {
						t.Fatalf("complete read-only value/provenance %q is unreachable", want)
					}
				}
				assertIntuitiveNoReply(t, reply)
				flowClick(t, m, "[Back]")
				flowAwaitReply(t, reply)
				if m.dialog != nil || m.currentDraft() != "exact suspended human draft\n  " {
					t.Fatal("explicit Back changed the suspended draft")
				}
			})
		}
	}
}

func TestNativeDefaultsViewerKeyboardWheelAndRetiredHits(t *testing.T) {
	for _, input := range []string{"keyboard", "wheel", "retired"} {
		t.Run(input, func(t *testing.T) {
			m := testModel(t)
			m.mouse = true
			m.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
			p, reply, cancel := intuitivePrompt(t, defaultsViewQuestion())
			m.openPrompt(p)
			m.restoreFocus()
			point := flowPoint(t, m, "[Refresh]")
			if input == "retired" {
				cancel()
				<-p.Done
				m.Update(point)
				if m.dialog == nil {
					t.Fatal("retired defaults control was activated")
				}
				return
			}
			if input == "wheel" {
				var body hitZone
				for _, hit := range m.hitZones {
					if hit.action == "defaults-body" {
						body = hit
					}
				}
				if body.height == 0 {
					t.Fatal("drawn read-only viewport has no mouse bounds")
				}
				m.Update(tea.MouseWheelMsg{X: body.x, Y: body.y, Button: tea.MouseWheelDown})
				if m.questionViewport.YOffset() == 0 || m.dialog.actionFocus != 0 {
					t.Fatal("wheel failed to scroll, or activated an offered control")
				}
				assertIntuitiveNoReply(t, reply)
				flowClick(t, m, "[Refresh]")
			} else {
				m.Update(keyPress(tea.KeyTab))
				m.Update(keyPress(tea.KeyTab))
				m.Update(keyPress(tea.KeyDown))
				assertIntuitiveNoReply(t, reply)
				m.Update(keyPress(tea.KeyEnter))
			}
			flowAwaitReply(t, reply)
			if m.dialog != nil {
				t.Fatal("explicit offered Refresh did not close this read generation")
			}
		})
	}
}

func TestNativeDefaultsNewScopeStartsAtVisibleProvenanceAndKeepsScopeTitle(t *testing.T) {
	for _, size := range flowSizes {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := testModel(t)
			m.launchPage, m.mouse = true, true
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m.composer.SetValue("exact suspended source draft")
			project := defaultsViewQuestion()
			project.Title = "Project on environment defaults · fixture"
			project.Prompt += "\n" + strings.Repeat("Bounded additional native field / inherited value\n", 50)
			p, reply, _ := intuitivePrompt(t, project)
			m.openPrompt(p)
			m.Update(keyPress(tea.KeyEnd))
			if m.questionViewport.YOffset() == 0 {
				t.Fatal("fixture did not scroll the project scope")
			}
			flowClick(t, m, "[Environment]")
			flowAwaitReply(t, reply)
			environment := defaultsViewQuestion()
			environment.Title = "Environment defaults · fixture"
			environment.Prompt += "\n" + strings.Repeat("Bounded additional native field / inherited value\n", 50)
			environment.Prompt = strings.Replace(environment.Prompt, "Scope: Project on environment", "Scope: Environment", 1)
			environment.Choices[0].Label = "Project"
			next, nextReply, _ := intuitivePrompt(t, environment)
			m.openPrompt(next)
			view := ansi.Strip(m.View().Content)
			if m.questionViewport.YOffset() != 0 || !strings.Contains(view, "Scope: Environment") || !strings.Contains(view, "Environment defaults") || m.currentDraft() != "exact suspended source draft" {
				t.Fatalf("new scope reused the previous offset or hid its identity:\n%s", view)
			}
			m.Update(keyPress(tea.KeyEnd))
			if view = ansi.Strip(m.View().Content); !strings.Contains(view, "Environment defaults") || !strings.Contains(view, "[Project]") {
				t.Fatalf("scrolled scope lost its persistent identity/control:\n%s", view)
			}
			assertIntuitiveNoReply(t, nextReply)
		})
	}
}
