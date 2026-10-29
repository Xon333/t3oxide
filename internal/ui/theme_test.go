package ui

import (
	"charm.land/bubbles/v2/textinput"
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/Xon333/t3oxide/internal/wire"
	"github.com/charmbracelet/x/ansi"
)

func TestThemeSemanticSlotPalettes(t *testing.T) {
	for _, tc := range []struct {
		name, accent, secondary, operation string
	}{
		{"violet", "#BBA5FA", "#84D7E4", "#C1D99B"},
		{"cyan", "#84D7E4", "#C1D99B", "#BBA5FA"},
		{"lime", "#C1D99B", "#BBA5FA", "#84D7E4"},
		{"terminal", "4", "6", "5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := colors(tc.name)
			if p.accent != tc.accent || p.secondary != tc.secondary || p.operation != tc.operation {
				t.Fatalf("highlight slots = %q / %q / %q, want %q / %q / %q", p.accent, p.secondary, p.operation, tc.accent, tc.secondary, tc.operation)
			}
			for _, highlight := range []string{p.accent, p.secondary, p.operation} {
				if highlight == p.error || highlight == p.warning {
					t.Fatal("highlight reused a warning/error state")
				}
			}
			if p.error == p.warning {
				t.Fatal("warning/error states share a color")
			}
			if tc.name == "terminal" && (p.fg != "" || p.bg != "" || p.muted != "" || p.elevated != "" || p.error != "1" || p.warning != "3") {
				t.Fatal("terminal default base or ANSI warning/error ownership changed")
			}
		})
	}
}

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

func TestTerminalThemeNestedHelpAndEditorsRoundTrip(t *testing.T) {
	m := testModel(t)
	m.launchTitle.SetValue("Exact title 界")
	m.launchTitle.SetCursor(3)
	m.agentbox.query.SetValue("Exact query")
	m.agentbox.query.SetCursor(2)
	m.openPalette()
	for _, theme := range []string{"terminal", "cyan", "terminal", "violet"} {
		m.theme = theme
		if err := m.settings.OverrideTheme(theme); err != nil {
			t.Fatal(err)
		}
		m.applyTheme()
		m.resizeNavigator()
		if m.launchTitle.Value() != "Exact title 界" || m.launchTitle.Position() != 3 || m.agentbox.query.Value() != "Exact query" || m.agentbox.query.Position() != 2 {
			t.Fatal("theme application changed editor contents or caret")
		}
		if theme != "terminal" {
			if m.dialog.list.Help.Styles.FullDesc.Render("description") != colors(theme).helpStyles().FullDesc.Render("description") {
				t.Fatal("nested help retained Terminal styles")
			}
			for _, input := range []textinput.Model{m.launchTitle, m.agentbox.query} {
				if input.Styles().Focused.Placeholder.Render("placeholder") != textinput.DefaultStyles(true).Focused.Placeholder.Render("placeholder") {
					t.Fatal("auxiliary editor did not regain its original style")
				}
			}
			continue
		}
		assertTerminalPalette(t, m.helpText())
		for _, full := range []bool{false, true} {
			m.dialog.list.Help.ShowAll = full
			assertTerminalPalette(t, m.dialog.list.Help.View(m.dialog.list))
		}
		for _, input := range []textinput.Model{m.launchTitle, m.agentbox.query, m.navigator.list.FilterInput} {
			s := input.Styles()
			for _, state := range []textinput.StyleState{s.Focused, s.Blurred} {
				assertTerminalPalette(t, state.Placeholder.Render("placeholder")+state.Suggestion.Render("suggestion")+state.Text.Render("text"))
			}
		}
	}
}
