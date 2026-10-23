package ui

import (
	"context"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/wire"
	"github.com/charmbracelet/x/ansi"
)

func TestSearchColorPreservesStyledUnicodeThroughRouter(t *testing.T) {
	t.Setenv("TERM", "xterm-ghostty")
	t.Setenv("COLORTERM", "truecolor")
	// Restore the inherited environment after this test, including whether
	// NO_COLOR was present. Do not change a package-global renderer.
	t.Setenv("NO_COLOR", "")
	if err := os.Unsetenv("NO_COLOR"); err != nil {
		t.Fatal(err)
	}
	for _, theme := range []string{"violet", "terminal"} {
		t.Run(theme, func(t *testing.T) {
			m := NewModel(context.Background(), nil, &Settings{Theme: theme})
			t.Cleanup(func() { _ = m.Close() })
			m.Update(tea.WindowSizeMsg{Width: 100, Height: 45})
			var renderer transcriptRenderer
			req := renderRequest{generation: m.generation, width: m.contentWidth, theme: theme, reset: true}
			req.snapshot = &wire.Snapshot{Items: []wire.Row{{
				Key: "synthetic", Kind: "assistant_message",
				Text: "Initial **Markdown** for Alpha — Café 界 👩🏽‍💻\n\nSecond **Markdown** result.",
			}}}
			content, err := renderer.render(m.ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(content, "\x1b[") || theme == "violet" && !strings.Contains(content, "\x1b[38;2;") {
				t.Fatal("regression requires genuine ANSI color from Glamour")
			}
			m.Update(renderedMsg{request: req, content: content})
			m.viewport.GotoTop()
			before := m.viewport.View()
			original := m.viewport.GetContent()

			// Route the command through the real composer and Enter handler.
			m.Update(tea.PasteMsg{Content: "/search mARKDOWN"})
			m.Update(keyPress(tea.KeyEnter))
			if m.searchTerm != "mARKDOWN" || len(m.searchLines) != 2 {
				t.Fatalf("search not routed: term=%q lines=%v", m.searchTerm, m.searchLines)
			}
			after := m.viewport.View()
			if after == before {
				t.Error("search did not visibly style the selected matching line")
			}
			if ansi.Strip(after) != ansi.Strip(before) {
				t.Error("search changed visible transcript text or row widths")
			}
			first := m.View().Content
			for i := 0; i < 8; i++ {
				if got := m.View().Content; got != first {
					t.Fatalf("View changed at iteration %d without an update", i)
				}
			}
			if m.viewport.GetContent() != original || m.transcript != content {
				t.Error("search or View mutated retained transcript content")
			}
			if !utf8.ValidString(first) {
				t.Error("search produced invalid UTF-8")
			}

			m.Update(keyPress(tea.KeyTab))
			m.Update(keyPress('n'))
			if m.searchIndex != 1 {
				t.Errorf("next match index=%d", m.searchIndex)
			}
			m.Update(keyPress('N'))
			if m.searchIndex != 0 {
				t.Errorf("previous match index=%d", m.searchIndex)
			}
			m.Update(renderedMsg{request: req, content: content + "\nTail 界"})
			if got := ansi.Strip(m.viewport.View()); strings.Contains(got, "[38;") || !strings.Contains(got, "Café 界 👩🏽‍💻") {
				t.Error("render refresh corrupted styled Unicode search content")
			}
			m.Update(keyPress(tea.KeyTab))
			m.Update(tea.PasteMsg{Content: "/search absent"})
			m.Update(keyPress(tea.KeyEnter))
			if m.searchIndex != -1 || m.viewport.StyleLineFunc != nil {
				t.Error("no-match search did not clear the selected line style")
			}
		})
	}
}
