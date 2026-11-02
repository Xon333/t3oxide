package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/wire"
	"strings"
	"testing"
)

func TestAcceptedSourceCodeRetainsBytes(t *testing.T) {
	source := "before\r\n```go\r\n\tfoo()\r\n```\r\nafter\n~~~\nsecond\n~~~\n"
	blocks := loadedCodeBlocks(source)
	if len(blocks) != 2 || blocks[0] != "\tfoo()\r\n" || blocks[1] != "second\n" {
		t.Fatalf("source bytes changed: %#v", blocks)
	}
	if len(loadedCodeBlocks("```\nunclosed")) != 0 {
		t.Fatal("offered incomplete code")
	}
	if redactSourceBytes("\tclean\r\n") != "\tclean\r\n" {
		t.Fatal("redaction normalized source")
	}
}
func TestAcceptedComposerPointerCancelsOnResize(t *testing.T) {
	m := testModel(t)
	m.mouse = true
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.composer.SetValue("alpha beta\nsecond")
	m.View()
	_, z, ok := m.drawnEditor()
	if !ok {
		t.Fatal("missing drawn editor")
	}
	handled, _ := m.editorPointer(tea.MouseClickMsg{X: z.x + 3, Y: z.y, Button: tea.MouseLeft})
	if !handled || m.editorGesture == nil {
		t.Fatal("pointer did not begin")
	}
	m.editorPointer(tea.MouseMotionMsg{X: z.x + 7, Y: z.y, Button: tea.MouseLeft})
	if !m.composer.HasSelection() {
		t.Fatal("drag did not select source")
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if m.editorGesture != nil {
		t.Fatal("resize retained gesture")
	}
	if m.composer.Value() != "alpha beta\nsecond" {
		t.Fatal("pointer changed draft")
	}
}
func TestAcceptedReviewNoteStagesOnlyAndRejectsOldSource(t *testing.T) {
	m := testModel(t)
	seedAgentBox(m)
	row, _ := m.selectedInbox()
	m.agentbox.reviewTarget = row.Target
	idx := buildReviewIndex(wire.ThreadReview{Diff: "diff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -1 +1 @@\n-old\n+new\n"})
	m.reviewIndex = &idx
	m.composer.SetValue("existing human text")
	m.reviewNavigate(idx.Digest + ":0:note")
	if !strings.Contains(m.currentDraft(), "existing human text") || !strings.Contains(m.currentDraft(), "Review note") || m.actionBusy {
		t.Fatal("note did not stage safely")
	}
	before := m.currentDraft()
	m.reviewNavigate("stale:0:note")
	if m.currentDraft() != before {
		t.Fatal("stale anchor changed text")
	}
}
func TestAcceptedServerEditsKeepModeWithoutRead(t *testing.T) {
	m := testModel(t)
	seedAgentBox(m)
	m.agentbox.serverSearch = true
	m.agentbox.query.SetValue("new query")
	m.applyInboxQuery("old query")
	if !m.agentbox.serverSearch || m.agentbox.loading || len(m.agentbox.rows) != 0 {
		t.Fatal("server edit changed mode or retained results")
	}
	m.navigator.serverSearch = true
	m.navigator.list.FilterInput.SetValue("new query")
	m.applyNavigatorFilter("old query")
	if !m.navigator.serverSearch || m.navigator.loading || len(m.navigator.list.Items()) != 0 {
		t.Fatal("navigator edit changed meaning")
	}
}

func TestAcceptedGraphemeMotionAndDelete(t *testing.T) {
	for _, s := range []string{"e\u0301", "👩‍💻", "🇦🇹"} {
		editor := newTextarea("")
		editor.Focus()
		editor.SetValue("a" + s + "b")
		editor.CursorEnd()
		updateGraphemeEditor(&editor, keyPress(tea.KeyLeft))
		updateGraphemeEditor(&editor, tea.KeyPressMsg{Code: tea.KeyBackspace})
		if editor.Value() != "ab" || editor.Column() != 1 {
			t.Fatalf("split grapheme %q: %q", s, editor.Value())
		}
	}
}
func TestAcceptedContextSurfaceInvalidatesSourceAndGeometry(t *testing.T) {
	m := testModel(t)
	m.contextPanel(28, 20)
	before := m.contextSurface.content
	m.contextSelection++
	m.contextPanel(28, 20)
	if m.contextSurface.key.selection != m.contextSelection {
		t.Fatal("stale selection key")
	}
	m.theme = "terminal"
	m.contextPanel(30, 18)
	if m.contextSurface.key.width != 30 || m.contextSurface.key.height != 18 || m.contextSurface.key.theme != "terminal" {
		t.Fatal("stale pane geometry")
	}
	_ = before
}
