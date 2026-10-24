package ui

import (
	"context"
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/wire"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func semanticSlotThemes() []string {
	themes := []string{"violet", "cyan", "lime", "terminal"}
	ids := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	for _, base := range themes[:3] {
		s := &Settings{Theme: base}
		for _, id := range ids {
			themes = append(themes, s.ThemeFor(id, ids))
		}
	}
	return themes
}

// Decode actual renderer output through Bubble Tea's existing cell engine;
// checking a palette alone cannot detect a consumer taking the wrong slot.
func semanticSlotCells(t *testing.T, content, text string) []uv.Cell {
	t.Helper()
	for _, line := range uv.NewStyledString(content).Lines(ansi.GraphemeWidth) {
		var plain strings.Builder
		for _, cell := range line {
			plain.WriteString(cell.Content)
		}
		start := strings.Index(plain.String(), text)
		if start < 0 {
			continue
		}
		var cells []uv.Cell
		pos := 0
		for _, cell := range line {
			if pos >= start && pos < start+len(text) {
				cells = append(cells, cell)
			}
			pos += len(cell.Content)
		}
		return cells
	}
	t.Fatalf("rendered text missing %q in %q", text, ansi.Strip(content))
	return nil
}

func assertSemanticSlotColor(t *testing.T, got color.Color, want string) {
	t.Helper()
	if want == "" {
		if got != nil {
			t.Fatalf("expected terminal default foreground, got %v", got)
		}
		return
	}
	if got == nil {
		t.Fatalf("expected foreground %s, got terminal default", want)
	}
	r, g, b, a := got.RGBA()
	wr, wg, wb, wa := lipgloss.Color(want).RGBA()
	if r != wr || g != wg || b != wb || a != wa {
		t.Fatalf("foreground = %v, want %s", got, want)
	}
}

func assertSemanticSlotText(t *testing.T, content, text, want string) {
	t.Helper()
	for _, cell := range semanticSlotCells(t, content, text) {
		assertSemanticSlotColor(t, cell.Style.Fg, want)
	}
}

func TestSemanticSlotsRendererRoleBoundaries(t *testing.T) {
	for _, theme := range semanticSlotThemes() {
		t.Run(theme, func(t *testing.T) {
			p := colors(theme)
			var r transcriptRenderer
			rows := []wire.Row{
				{Key: "u", Kind: "user_message", Text: "Question 界"},
				{Key: "a", Kind: "assistant_message", Text: "# Context\n\n[Reference](https://example.com) and `inline_token`", Streaming: true},
				{Key: "n", Kind: "system_notice", Text: "Notice"},
				{Key: "e", Kind: "error", Text: "Failure"},
			}
			out, err := r.render(context.Background(), renderRequest{snapshot: &wire.Snapshot{Items: rows}, theme: theme, width: 100})
			if err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct{ text, color string }{
				{"Context", p.accent}, {"Reference", p.secondary}, {"inline_token", p.secondary},
				{"streaming", p.operation}, {"you", p.muted}, {"Agent", p.muted},
				{"T3 notice", p.muted}, {"T3 error", p.error},
			} {
				assertSemanticSlotText(t, out, tc.text, tc.color)
			}
			if theme == "terminal" {
				assertTerminalPalette(t, out)
			}
			// Source-owned row kinds/text and their authoritative streaming flag
			// survive styling; completion must remove only the streaming suffix.
			rows[1].Streaming = false
			out, err = r.render(context.Background(), renderRequest{snapshot: &wire.Snapshot{Items: rows}, theme: theme, width: 100})
			if err != nil || strings.Contains(ansi.Strip(out), "streaming") || rows[1].Kind != "assistant_message" || !strings.Contains(ansi.Strip(out), "inline_token") {
				t.Fatalf("styling changed source authority/text or retained stale streaming: %v", err)
			}
		})
	}
}

func TestSemanticSlotsNativeSelectionAndAuthoritativeHeader(t *testing.T) {
	for _, theme := range semanticSlotThemes() {
		t.Run(theme, func(t *testing.T) {
			m := testModel(t)
			m.theme = theme
			m.applyTheme()
			m.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
			p := colors(theme)
			for _, state := range []string{"running", "responding", "completed"} {
				m.status = client.Status{ThreadID: "thread", RunState: state, ConnectionState: "synchronized", State: "local receipt", Requests: 2}
				out := m.View().Content
				assertSemanticSlotText(t, out, "t3oxide", p.accent)
				assertSemanticSlotText(t, out, state, p.operation)
				assertSemanticSlotText(t, out, "Awaiting input (2)", p.operation)
				assertSemanticSlotText(t, out, "Actions", p.fg)
				if strings.Contains(ansi.Strip(out), "Connection: synchronized") {
					t.Fatal("quiet connection acquired a redundant header row")
				}
				if strings.Contains(ansi.Strip(out), "local receipt") {
					t.Fatal("local state replaced authoritative run status")
				}
			}
			m.openPrompt(retiredPrompt(999, client.Question{Kind: client.ChooseOne, Choices: []client.Choice{{ID: "a", Label: "Unselected"}, {ID: "b", Label: "Selected"}}}))
			m.dialog.list.SetSize(60, 8)
			m.dialog.list.Select(1)
			m.applyTheme()
			out := m.dialog.list.View()
			assertSemanticSlotText(t, out, "Unselected", p.fg)
			for _, cell := range semanticSlotCells(t, out, "Selected") {
				if theme == "terminal" {
					assertSemanticSlotColor(t, cell.Style.Fg, "")
					if cell.Style.Bg != nil || cell.Style.Attrs&uv.AttrReverse == 0 {
						t.Fatal("terminal native selection lost default reversed pair")
					}
				} else {
					assertSemanticSlotColor(t, cell.Style.Fg, p.accent)
				}
			}
			if m.dialog.list.SelectedItem().(choiceItem).id != "b" {
				t.Fatal("restyling moved native list selection")
			}
			if theme == "terminal" {
				assertTerminalPalette(t, out)
			}
		})
	}
}

func TestSemanticSlotsThemeChangesPreserveNativeEditors(t *testing.T) {
	m := testModel(t)
	m.composer.SetValue("exact draft 界\nsecond line")
	m.composer.SetCursorColumn(3)
	m.composer.SelectAll()
	m.modalInput.SetValue("exact answer 界")
	m.modalInput.SetCursorColumn(4)
	m.searchInput.SetValue("exact query 界")
	m.searchInput.SetCursor(3)
	line, column := m.composer.Line(), m.composer.Column()
	selected := m.composer.SelectedText()
	for _, theme := range semanticSlotThemes() {
		m.theme = theme
		m.applyTheme()
		p := colors(theme)
		if m.composer.Value() != "exact draft 界\nsecond line" || m.composer.Line() != line || m.composer.Column() != column || m.composer.SelectedText() != selected || !m.composer.HasSelection() {
			t.Fatalf("%s changed native draft/caret/selection", theme)
		}
		if m.modalInput.Value() != "exact answer 界" || m.modalInput.Column() != 4 || m.searchInput.Value() != "exact query 界" || m.searchInput.Position() != 3 {
			t.Fatalf("%s changed native answer/query caret", theme)
		}
		for _, styles := range []struct{ prompt, cursor color.Color }{
			{m.composer.Styles().Focused.Prompt.GetForeground(), m.composer.Styles().Cursor.Color},
			{m.modalInput.Styles().Focused.Prompt.GetForeground(), m.modalInput.Styles().Cursor.Color},
			{m.searchInput.Styles().Focused.Prompt.GetForeground(), m.searchInput.Styles().Cursor.Color},
		} {
			assertSemanticSlotColor(t, styles.prompt, p.accent)
			assertSemanticSlotColor(t, styles.cursor, p.accent)
		}
	}
}
