package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"testing"
)

func TestSourceCodeFenceBoundariesRetainBodyBytes(t *testing.T) {
	for _, source := range []string{
		"   ```go\r\n\tbytes\r\n  `````  \r\n",
		" ~~~ lang\r\n\tbytes\r\n ~~~~~\r\n",
	} {
		blocks := loadedCodeBlocks(source)
		if len(blocks) != 1 || blocks[0] != "\tbytes\r\n" {
			t.Fatalf("lost source bytes: %#v", blocks)
		}
	}
	for _, source := range []string{"    ```\nindented\n    ```", "```bad`info\nbody\n```", "````\nbody\n```"} {
		if len(loadedCodeBlocks(source)) != 0 {
			t.Fatalf("invalid or incomplete fence offered: %q", source)
		}
	}
}

func TestLaunchMessagePointerAndModalTerminalCaret(t *testing.T) {
	for _, size := range [][2]int{{60, 16}, {140, 40}} {
		for _, stage := range []string{"", "message"} {
			m := testModel(t)
			m.mouse = true
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			q := launchQuestion(client.LaunchInput{Title: "Draft", Message: "abcdef"})
			q.LaunchReview.Stage = stage
			p, _, cancel := intuitivePrompt(t, q)
			m.openPrompt(p)
			m.restoreFocus()
			m.modalInput.CursorEnd()
			view := m.View()
			editor, z, ok := m.drawnEditor()
			if !ok || editor != &m.modalInput {
				t.Fatalf("launch editor missing %v %q", size, stage)
			}
			if view.Cursor == nil {
				t.Fatalf("no terminal caret %v %q", size, stage)
			}
			handled, _ := m.editorPointer(tea.MouseClickMsg{X: z.x + 2, Y: z.y, Button: tea.MouseLeft})
			if !handled || m.modalInput.Column() >= 6 {
				t.Fatalf("launch click did not place caret %v %q", size, stage)
			}
			m.editorPointer(tea.MouseMotionMsg{X: z.x + 5, Y: z.y, Button: tea.MouseLeft})
			if !m.modalInput.HasSelection() || m.modalInput.Value() != "abcdef" {
				t.Fatalf("launch drag failed %v %q", size, stage)
			}
			m.editorPointer(tea.MouseReleaseMsg{X: z.x + 5, Y: z.y, Button: tea.MouseLeft})
			if m.editorGesture != nil {
				t.Fatal("released drag retained gesture")
			}
			cancel()
		}
	}
}
