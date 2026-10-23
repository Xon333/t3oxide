package ui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/wire"
	"github.com/charmbracelet/x/ansi"
)

func TestIntuitiveDensityFeedbackAppearanceRetirementPreservesReading(t *testing.T) {
	for _, size := range [][2]int{{60, 16}, {80, 24}, {120, 35}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := testModel(t)
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m.Update(tea.PasteMsg{Content: "human 界 draft\nsecond line"})
			m.Update(keyPress(tea.KeyLeft, tea.ModShift))
			line, column, selection := m.composer.Line(), m.composer.Column(), m.composer.SelectedText()
			draft, edits := m.currentDraft(), m.draftEdits
			m.transcript = strings.Repeat("Reading anchor\n", 80)
			m.viewport.SetContent(m.transcript)
			m.viewport.SetYOffset(12)
			m.follow = false
			m.snapshot = &wire.Snapshot{Metadata: wire.Metadata{Historical: true}}
			snapshot := m.snapshot
			height := m.bodyHeight
			check := func(wantHeight int) {
				t.Helper()
				m.View()
				if m.bodyHeight != wantHeight || m.viewport.Height() != wantHeight || m.viewport.YOffset() != 12 || m.follow || m.snapshot != snapshot {
					t.Fatalf("feedback changed reading state: height=%d offset=%d follow=%t", m.bodyHeight, m.viewport.YOffset(), m.follow)
				}
				if m.currentDraft() != draft || m.draftEdits != edits || m.composer.Line() != line || m.composer.Column() != column || m.composer.SelectedText() != selection {
					t.Fatal("feedback changed canonical draft/caret/selection")
				}
			}
			m.Update(noticeMsg{generation: m.generation, text: "Copied to clipboard"})
			check(height - 1)
			if !strings.Contains(ansi.Strip(m.View().Content), "Copied to clipboard") {
				t.Fatal("actual notice is hidden")
			}
			m.Update(noticeMsg{generation: m.generation, text: "A different notice"})
			check(height - 1)
			m.Update(noticeMsg{generation: m.generation})
			check(height)
			m.Update(noticeMsg{generation: m.generation - 1, text: "stale notice"})
			check(height)
		})
	}
}

func TestIntuitiveDensityFeedbackPreservesFollowAndCompletionPosition(t *testing.T) {
	m := testModel(t)
	m.Update(tea.PasteMsg{Content: "/mo"})
	m.transcript = strings.Repeat("Transcript row\n", 80)
	m.viewport.SetContent(m.transcript)
	m.viewport.GotoBottom()
	if !m.completionVisible() {
		t.Fatal("fixture did not open completion")
	}
	panelRow := func() int {
		for row, text := range strings.Split(ansi.Strip(m.View().Content), "\n") {
			if strings.Contains(text, "Commands") {
				return row
			}
		}
		t.Fatal("completion panel is hidden")
		return -1
	}
	before := panelRow()
	height := m.bodyHeight
	completion := m.completion.list.SelectedItem().(choiceItem).command
	m.Update(noticeMsg{generation: m.generation, text: "Feedback"})
	if !m.follow || !m.viewport.AtBottom() || m.bodyHeight != height-1 || panelRow() != before-1 || m.completion.list.SelectedItem().(choiceItem).command != completion {
		t.Fatal("notification displaced completion or lost live follow")
	}
	m.Update(noticeMsg{generation: m.generation})
	if !m.follow || !m.viewport.AtBottom() || panelRow() != before || m.currentDraft() != "/mo" {
		t.Fatal("retirement failed to restore transcript space and completion position")
	}
}

func TestIntuitiveDensityFeedbackRetirementClampsLastHistoricalPage(t *testing.T) {
	for _, size := range [][2]int{{60, 16}, {80, 24}, {120, 35}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := testModel(t)
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m.Update(tea.PasteMsg{Content: "exact human draft\nsecond line"})
			m.Update(keyPress(tea.KeyLeft, tea.ModShift))
			draft, selection := m.currentDraft(), m.composer.SelectedText()
			line, column := m.composer.Line(), m.composer.Column()
			m.Update(noticeMsg{generation: m.generation, text: "Copied to clipboard"})
			m.transcript = strings.Repeat("Earlier row\n", 80)
			m.viewport.SetContent(m.transcript)
			m.viewport.GotoBottom()
			m.follow = false
			m.snapshot = &wire.Snapshot{Metadata: wire.Metadata{Historical: true}}
			snapshot := m.snapshot
			offset, height := m.viewport.YOffset(), m.viewport.Height()

			m.Update(noticeMsg{generation: m.generation})
			bottom := m.viewport
			bottom.GotoBottom()
			if m.viewport.Height() != height+1 || m.viewport.YOffset() != offset-1 || m.viewport.YOffset() != bottom.YOffset() {
				t.Fatalf("grown historical viewport retained an invalid final offset: before=%d after=%d bottom=%d", offset, m.viewport.YOffset(), bottom.YOffset())
			}
			if m.follow || m.snapshot != snapshot || m.currentDraft() != draft || m.composer.Line() != line || m.composer.Column() != column || m.composer.SelectedText() != selection {
				t.Fatal("clamping the final historical page changed follow, snapshot or canonical editor state")
			}
		})
	}
}

func TestIntuitiveDensityAuthoritativeRequestsAndRealFeedback(t *testing.T) {
	m := testModel(t)
	m.sidebar = true
	m.status.Requests, m.status.RunState = 3, "running"
	m.status.PendingAction, m.status.PendingStop, m.status.DraftSaveFailed = true, true, true
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 35})
	view := ansi.Strip(m.View().Content)
	if strings.Count(view, "Awaiting input (3)") != 1 || strings.Contains(view, "request(s)") || m.feedbackVisible() || m.feedbackView(80) != "" {
		t.Fatalf("request count acquired duplicate feedback:\n%s", view)
	}
	for _, want := range []string{"running", "Saved action", "Saved Stop", "Draft not saved", "Ctrl+O  Browser"} {
		if !strings.Contains(view, want) {
			t.Fatalf("authoritative state missing %q", want)
		}
	}
	m.actionBusy, m.actionName = true, "send"
	if !m.feedbackVisible() || m.feedbackView(80) == "" {
		t.Fatal("active command lost progress")
	}
	m.stopBusy = true
	if !strings.Contains(ansi.Strip(m.feedbackView(80)), commandProgress("stop")) {
		t.Fatal("independent Stop progress disappeared")
	}
	m.problem = "Durable failure retained"
	m.Update(noticeMsg{generation: m.generation})
	if !strings.Contains(ansi.Strip(m.feedbackView(80)), "Durable failure retained") || !m.durableProblem() {
		t.Fatal("notice retirement hid durable failure")
	}
}

func TestIntuitiveDensityEditorsHaveOneLabel(t *testing.T) {
	m := testModel(t)
	view := ansi.Strip(m.View().Content)
	if strings.Count(view, "Message") != 1 || m.composer.Placeholder != "" || modelStatusLine("saved · high") != "saved · high" || modelStatusLine("") != "Model unreported" {
		t.Fatalf("redundant composer/model labels:\n%s", view)
	}
	m.restoreDraft("normal draft")
	if m.composer.Placeholder != "" {
		t.Fatal("draft restoration revived redundant placeholder")
	}
	m.searching = true
	m.resize()
	view = ansi.Strip(m.View().Content)
	if strings.Count(view, "Find in current page") != 1 || strings.Contains(view, "Find in this page") || m.searchInput.Prompt != "" {
		t.Fatalf("duplicate search scope:\n%s", view)
	}
	m.searching = false
	m.status.NextModel = "next-model"
	if !strings.Contains(ansi.Strip(m.View().Content), "Next send · next-model") {
		t.Fatal("next-send context disappeared")
	}
	m.restoreDraft("exact\tdraft")
	if m.currentDraft() != "exact\tdraft" || !strings.Contains(ansi.Strip(m.View().Content), "Exact draft") {
		t.Fatal("exact canonical draft lost context")
	}
	m.transcriptFocus = true
	if !strings.Contains(ansi.Strip(m.View().Content), "Reading transcript") {
		t.Fatal("reading context disappeared")
	}
}

func TestIntuitiveDensityCompactFooterUsesContext(t *testing.T) {
	m := testModel(t)
	footer := ansi.Strip(m.footerView(39))
	if !strings.Contains(footer, "Ctrl+P commands") || strings.Contains(footer, "^Space") || strings.Index(footer, "Ctrl+S") > strings.Index(footer, "Ctrl+P") {
		t.Fatalf("compact footer lost contextual command/Stop packing: %q", footer)
	}
	m.status.Requests = 2
	footer = ansi.Strip(m.footerView(39))
	if !strings.Contains(footer, "Ctrl+R requests") || !strings.HasPrefix(footer, "Ctrl+S") {
		t.Fatalf("compact footer bypassed pending requests or Stop: %q", footer)
	}
	m.status.Requests = 0
	m.restoreDraft("nonempty human draft")
	if footer = ansi.Strip(m.footerView(56)); !strings.Contains(footer, "Tab→Ctrl+P commands") {
		t.Fatalf("nonempty editor advertised wrong context: %q", footer)
	}
}

func TestIntuitiveDensityFullPageGeometryPaysForActualChrome(t *testing.T) {
	for _, surface := range []string{"browser", "launch"} {
		t.Run(surface, func(t *testing.T) {
			m := testModel(t)
			m.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
			if surface == "browser" {
				q := browserQuestion("a")
				for i := range 20 {
					q.Choices = append(q.Choices, client.Choice{ID: fmt.Sprint(i), Label: fmt.Sprint("Conversation ", i)})
				}
				m.openPrompt(retiredPrompt(7107, q))
			} else {
				m.openPrompt(retiredPrompt(7108, launchQuestion(client.LaunchInput{Message: "first message\nsecond line"})))
			}
			m.restoreFocus()
			line, column, selection := m.modalInput.Line(), m.modalInput.Column(), m.modalInput.SelectedText()
			input := m.modalInput.Value()
			height := m.modalHeight
			inputHeight := m.modalInput.Height()
			m.Update(noticeMsg{generation: m.generation, text: "Visible notice"})
			if m.modalHeight != height-1 {
				t.Fatalf("full-page modal ignored feedback height: before=%d after=%d", height, m.modalHeight)
			}
			if surface == "launch" && m.modalInput.Height() != inputHeight-1 {
				t.Fatal("launch textarea ignored feedback allocation")
			}
			if m.modalInput.Value() != input || m.modalInput.Line() != line || m.modalInput.Column() != column || m.modalInput.SelectedText() != selection {
				t.Fatal("surface resize changed canonical editor state")
			}
			available := m.height - 2 - lipgloss.Height(m.dialogHelp(m.workspaceWidth(), true)) - lipgloss.Height(m.feedbackView(m.workspaceWidth()))
			if lipgloss.Height(m.modalView()) > available {
				t.Fatal("modal exceeded measured full-page body allocation")
			}
			if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Visible notice") || !strings.Contains(view, "Ctrl+Q") {
				t.Fatalf("feedback hid full-page bottom controls:\n%s", view)
			}
			m.Update(noticeMsg{generation: m.generation})
			if m.modalHeight != height || surface == "launch" && m.modalInput.Height() != inputHeight {
				t.Fatal("retirement failed to return full-page editor space")
			}
		})
	}
}

func TestIntuitiveDensityConditionalPickerFooter(t *testing.T) {
	for _, tc := range []struct {
		name  string
		items []list.Item
		want  string
	}{
		{"complete", []list.Item{choiceItem{id: "a", title: "Complete title"}}, ""},
		{"description", []list.Item{choiceItem{id: "a", title: "Complete title", description: "Useful consequence"}}, "Useful consequence"},
		{"arguments", []list.Item{choiceItem{title: "Command", command: "theme", args: "terminal"}}, "terminal"},
		{"disabled", []list.Item{choiceItem{title: "Command", disabled: true, description: "Unavailable: no target"}}, "Unavailable: no target"},
		{"truncated", []list.Item{choiceItem{id: "a", title: strings.Repeat("Long title ", 10)}}, "→ Long title"},
		{"duplicate", []list.Item{choiceItem{id: "opaque-a", title: "Same title"}, choiceItem{id: "opaque-b", title: "Same title"}}, "opaque-a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := testModel(t)
			m.dialog = &dialog{kind: client.ChooseOne, title: "Picker"}
			m.dialog.list = m.newList("Picker", tc.items)
			m.refreshDialogDetails()
			m.resize()
			before, detail := m.dialog.list.SelectedItem(), m.dialog.detailText
			footer := ansi.Strip(m.pickerSelectionView(m.modalWidth))
			if tc.want == "" && footer != "" || tc.want != "" && !strings.Contains(footer, tc.want) {
				t.Fatalf("footer=%q, want %q", footer, tc.want)
			}
			m.View()
			if m.dialog.list.SelectedItem() != before || m.dialog.detailText != detail {
				t.Fatal("rendering changed selected identity or complete Details")
			}
			m.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
			if tc.want != "" && !strings.Contains(ansi.Strip(m.pickerSelectionView(m.modalWidth)), tc.want) {
				t.Fatal("compact layout lost useful selected context")
			}
		})
	}
}

func TestIntuitiveDensityDialogHelpTracksDetailsAndLaunchFocus(t *testing.T) {
	m := testModel(t)
	m.openPrompt(retiredPrompt(7104, client.Question{Kind: client.ChooseMany, Title: "Answers", Choices: []client.Choice{{ID: "a", Label: "Alpha"}}}))
	if !strings.Contains(ansi.Strip(m.modalView()), "Enter toggle") {
		t.Fatal("ordinary modal lost local activation help")
	}
	m.dialog.finishFocused = true
	if !strings.Contains(ansi.Strip(m.dialogHelp(76, false)), "Enter continue") {
		t.Fatal("multiple-choice completion help is stale")
	}
	m.openPrompt(retiredPrompt(7105, launchQuestion(client.LaunchInput{})))
	for _, focus := range []int{0, 1, len(m.launchActions()) + 1} {
		m.dialog.launch.focus = focus
		help := ansi.Strip(m.dialogHelp(56, true))
		want := "Enter newline"
		if focus == 1 {
			want = "Enter continue"
		} else if focus > 1 {
			want = "Enter cancel"
		}
		if !strings.Contains(help, want) || strings.Count(help, "Ctrl+S") != 1 || !strings.Contains(help, "Ctrl+Q") {
			t.Fatalf("launch focus help=%q, want %q", help, want)
		}
	}
	m.dialog.details = true
	m.questionViewport.SetContent("Complete Details")
	m.resize()
	if strings.Contains(ansi.Strip(m.modalView()), "Ctrl+L/Esc") || !strings.Contains(ansi.Strip(m.dialogHelp(56, true)), "Ctrl+L/Esc back") {
		t.Fatal("full-page Details retained duplicate local help")
	}
}

func TestIntuitiveDensityRetirementKeepsUnrelatedErrorDetail(t *testing.T) {
	m := testModel(t)
	m.openPrompt(retiredPrompt(7106, browserQuestion("a")))
	m.openBrowserMore()
	m.problem, m.streamProblem = "saved failure", "connection failure"
	m.openFeedbackDetail()
	detail, title := m.detail, m.detailTitle
	m.Update(promptClosedMsg{generation: m.generation, id: 7106})
	if m.dialog != nil || m.browserDialog != nil || m.detail != detail || m.detailTitle != title || m.problem != "saved failure" || m.streamProblem != "connection failure" {
		t.Fatal("browser retirement discarded unrelated durable error detail")
	}
}

func TestIntuitiveDensityPickerFooterChangesPreserveNativeSelection(t *testing.T) {
	m := testModel(t)
	choices := []client.Choice{{ID: "a", Label: "Alpha"}, {ID: "b", Label: "Beta", Description: "Consequence"}}
	for i := range 30 {
		choices = append(choices, client.Choice{ID: fmt.Sprint(i), Label: fmt.Sprint("Other ", i)})
	}
	m.openPrompt(retiredPrompt(7101, client.Question{Kind: client.ChooseOne, Choices: choices}))
	m.restoreFocus()
	height := m.dialog.list.Height()
	m.Update(keyPress(tea.KeyDown))
	if m.dialog.list.SelectedItem().(choiceItem).id != "b" || m.pickerSelectionView(m.modalWidth) == "" || m.dialog.list.Height() >= height {
		t.Fatal("context footer changed native selection or failed to reserve a row")
	}
	m.Update(keyPress(tea.KeyUp))
	if m.dialog.list.SelectedItem().(choiceItem).id != "a" || m.pickerSelectionView(m.modalWidth) != "" || m.dialog.list.Height() != height {
		t.Fatal("retiring redundant footer changed selection or failed to reclaim row")
	}
}

func TestIntuitiveDensityFullPageHelpHasOneOwner(t *testing.T) {
	for _, size := range [][2]int{{60, 16}, {80, 24}, {120, 35}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := testModel(t)
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m.openPrompt(retiredPrompt(7102, browserQuestion("a")))
			local := ansi.Strip(m.modalView())
			if strings.Contains(local, "Ctrl+L") || strings.Contains(local, "Enter open") {
				t.Fatal("full page modal retained local help")
			}
			help := ansi.Strip(m.dialogHelp(m.workspaceWidth(), true))
			first, _, _ := strings.Cut(help, "\n")
			if !strings.Contains(first, "Enter open") || !strings.Contains(first, "Ctrl+S") {
				t.Fatalf("compact primary/Stop hidden: %q", first)
			}
			view := ansi.Strip(m.View().Content)
			for _, want := range []string{"Enter open", "Tab controls", "Ctrl+L details", "Esc resume", "Ctrl+S", "Ctrl+Q"} {
				if strings.Count(view, want) != 1 {
					t.Fatalf("expected one %q:\n%s", want, view)
				}
			}
			m.openDetail("Help", "Local help content")
			if strings.Contains(ansi.Strip(m.modalView()), "Ctrl+Y copy") || !strings.Contains(ansi.Strip(m.dialogHelp(m.workspaceWidth(), true)), "Ctrl+Y copy") {
				t.Fatal("detail overlay did not yield local controls to full page")
			}
			if lipgloss.Height(m.View().Content) > size[1] || lipgloss.Width(m.View().Content) > size[0] {
				t.Fatal("full page help exceeded terminal")
			}
			m.openPrompt(retiredPrompt(7105, launchQuestion(client.LaunchInput{Title: "Kept title", Message: "Kept first message"})))
			input := m.launchInput()
			m.openDetail("Help", "Full-page launch Help content")
			view = ansi.Strip(m.View().Content)
			if strings.Count(view, "╭") != 1 || !strings.Contains(view, "Full-page launch Help content") || strings.Contains(view, "╭─ First message") {
				t.Fatalf("launch Help was hidden or composited over a second form:\n%s", view)
			}
			if m.launchInput() != input {
				t.Fatal("rendering launch Help changed canonical form input")
			}
		})
	}
}

func TestIntuitiveDensityRetiredSuspendedBrowserClearsLocalHelp(t *testing.T) {
	for _, local := range []string{"browser-more", "browser-settings"} {
		t.Run(local, func(t *testing.T) {
			m := testModel(t)
			m.Update(tea.PasteMsg{Content: "source draft"})
			m.problem = "durable error"
			m.status.PendingAction = true
			done := make(chan struct{})
			p := &client.Prompt{ID: 7103, Question: browserQuestion("a"), Done: done}
			m.openPrompt(p)
			if local == "browser-more" {
				m.openBrowserMore()
			} else {
				m.browserAction("settings")
			}
			m.openDetail("Help", "Stale local Help")
			m.Update(promptClosedMsg{generation: m.generation - 1, id: p.ID})
			m.Update(promptClosedMsg{generation: m.generation, id: p.ID + 1})
			if m.detail == "" || m.browserDialog == nil {
				t.Fatal("unrelated retirement closed live local Help")
			}
			close(done)
			m.Update(promptClosedMsg{generation: m.generation, id: p.ID})
			if m.dialog != nil || m.browserDialog != nil || m.detail != "" || m.browserPage || !m.composer.Focused() {
				t.Fatal("retired exact browser left local Help or focus behind")
			}
			if m.problem != "durable error" || !m.status.PendingAction || m.currentDraft() != "source draft" {
				t.Fatal("local retirement changed unrelated durable state/draft")
			}
		})
	}
}
