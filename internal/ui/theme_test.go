package ui

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/Xon333/t3oxide/internal/wire"
	"github.com/charmbracelet/x/ansi"
)

// Default text and basic ANSI accents work with either light or dark terminal
// palettes. Reject fixed RGB, 256-color grays and painted backgrounds in every
// rendered surface, including nested Markdown styles and upstream defaults.
func assertTerminalPalette(t *testing.T, content string) {
	t.Helper()
	allowed := map[string]bool{"": true, "0": true, "1": true, "2": true, "3": true, "4": true, "7": true, "9": true, "22": true, "23": true, "24": true, "27": true, "29": true, "31": true, "32": true, "33": true, "34": true, "35": true, "36": true, "39": true, "49": true, "59": true}
	for _, match := range regexp.MustCompile("\x1b\\[([0-9;:]*)m").FindAllStringSubmatch(content, -1) {
		for _, parameter := range strings.Split(match[1], ";") {
			if !allowed[parameter] {
				t.Fatalf("terminal theme painted a fixed color: %q", match[0])
			}
		}
	}
}

func TestTerminalThemeMarkdownAndSyntaxUseTerminalPalette(t *testing.T) {
	var r transcriptRenderer
	text := "Body text 界\n\n# Heading\n\n##### Small heading\n\n> Quote\n\n[Link](https://example.com) ![Image](image.png) and `inline`\n\n---\n\n```go\n// comment\nfunc main() { println(42, \"hello\") }\n```\n\n```diff\n-old\n+new\n```\n\n| One | Two |\n| --- | --- |\n| A | B |"
	req := renderRequest{snapshot: &wire.Snapshot{Items: []wire.Row{{Key: "a", Kind: "assistant_message", Text: text}}}, width: 60}
	for _, theme := range []string{"violet", "terminal", "cyan", "terminal", "lime", "terminal"} {
		req.theme = theme
		content, err := r.render(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if theme != "terminal" {
			if !strings.Contains(content, "38;2;") {
				t.Fatal("fixed theme lost its RGB styling")
			}
			continue
		}
		assertTerminalPalette(t, content)
		for _, want := range []string{"Body text 界", "Heading", "Quote", "inline", "func main()", "-old", "+new"} {
			if !strings.Contains(ansi.Strip(content), want) {
				t.Fatalf("terminal Markdown lost %q", want)
			}
		}
		for _, color := range []string{"\x1b[34m", "\x1b[31m", "\x1b[32m"} {
			if !strings.Contains(content, color) {
				t.Fatalf("terminal syntax did not use palette slot: %q", color)
			}
		}
	}
}

func TestTerminalThemeRoundTripPreservesDraftAndStylesDialogs(t *testing.T) {
	m := testModel(t)
	m.composer.SetValue("exact human draft 界")
	for _, theme := range []string{"terminal", "cyan", "terminal", "lime", "terminal", "violet"} {
		finishCommand(t, m, m.dispatch("theme", theme))
		v := m.View()
		if m.theme != theme || m.composer.Value() != "exact human draft 界" {
			t.Fatal("theme change lost selection or draft")
		}
		if theme != "terminal" {
			if v.ForegroundColor == nil || v.BackgroundColor == nil || m.composer.Styles().Focused.Selection.GetReverse() {
				t.Fatal("fixed theme did not regain its original styles")
			}
			continue
		}
		if v.ForegroundColor != nil || v.BackgroundColor != nil {
			t.Fatal("terminal theme owns default colors")
		}
		if !m.composer.Styles().Focused.Selection.GetReverse() || !m.composer.Styles().Blurred.Selection.GetReverse() {
			t.Fatal("textarea selection does not use the default color pair")
		}
		assertTerminalPalette(t, v.Content)
		m.composer.SelectAll()
		assertTerminalPalette(t, m.View().Content)
		m.composer.ClearSelection()
		m.openPalette()
		assertTerminalPalette(t, m.View().Content)
		assertTerminalPalette(t, m.dialog.list.Paginator.ActiveDot+m.dialog.list.Paginator.InactiveDot)
		m.dialog.list.SetFilterText("theme")
		assertTerminalPalette(t, m.View().Content)
		m.cancelDialog()
	}
	m.openThemes()
	if items := m.dialog.list.Items(); len(items) != 5 || items[0].(choiceItem).args != "default" || items[1].(choiceItem).args != "terminal" {
		t.Fatal("environment default or terminal theme missing from chooser")
	}
}
