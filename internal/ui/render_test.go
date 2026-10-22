package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Xon333/t3oxide/internal/wire"
	"github.com/charmbracelet/x/ansi"
)

func TestTerminalTextCannotInjectControlSequences(t *testing.T) {
	input := "A\x1b[2J\x1b]52;c;c2VjcmV0\aB\x1b]0;title\x1b\\C\x00\r\x7f\u0085D\n\t👩🏽‍💻 café 界"
	got := safeText(input)
	if got != "ABCD\n\t👩🏽‍💻 café 界" {
		t.Fatalf("unsafe or lost readable content: %q", got)
	}
	if got = safeText(string([]byte{'a', 0xff, 'b'})); got != "a�b" {
		t.Fatalf("invalid UTF-8 handling: %q", got)
	}
	for width := 1; width < 24; width++ {
		got = fitLine("👩🏽‍💻 café 界\nlong title", width)
		if !utf8.ValidString(got) || ansi.StringWidth(got) > width || strings.Contains(got, "\n") {
			t.Fatalf("invalid truncated title at width %d: %q", width, got)
		}
	}
}

func TestMarkdownUsesRealWireKindsAndKeyedReplacement(t *testing.T) {
	var r transcriptRenderer
	snapshot := &wire.Snapshot{Revision: 1, Items: []wire.Row{
		{Key: "u", Kind: "user_message", Text: "Human input @not-a-file\nUnicode: 你好 👩🏽‍💻"},
		{Key: "a", Kind: "assistant_message", Text: "## Result\n\n**First** response\n\n```diff\n-old\n+new\n```", Streaming: true},
		{Key: "n", Kind: "system_notice", Text: "Notice", Truncated: true, AttachmentCount: 2},
	}}
	req := renderRequest{snapshot: snapshot, width: 42, theme: "violet"}
	content, err := r.render(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	plain := ansi.Strip(content)
	for _, want := range []string{"you", "assistant · streaming", "@not-a-file", "你好", "First", "-old", "+new", "T3 notice", "2 attachment(s)", "Shortened by T3oxide"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("render does not contain %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "assistant_message") || strings.Contains(plain, "user_message") {
		t.Fatal("raw protocol message kind shown instead of participant")
	}
	first := r.cache["a"]
	again, err := r.render(context.Background(), req)
	if err != nil || again != content || r.cache["a"] != first {
		t.Fatal("identical authoritative page changed rendering")
	}
	next := &wire.Snapshot{Revision: 2, Items: []wire.Row{{Key: "a", Kind: "assistant_message", Text: "Replacement, no old suffix", Streaming: false}}}
	req.snapshot = next
	content, err = r.render(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	plain = ansi.Strip(content)
	if strings.Contains(plain, "First") || strings.Contains(plain, "streaming") || !strings.Contains(plain, "Replacement") {
		t.Fatalf("keyed row was appended or stale: %q", plain)
	}
	if len(r.cache) != 1 {
		t.Fatalf("cache retained an earlier page: %d rows", len(r.cache))
	}
	old := content
	req.theme = "lime"
	content, _ = r.render(context.Background(), req)
	if content == old || r.theme != "lime" {
		t.Fatal("theme change did not invalidate styled Markdown")
	}
	req.width = 20
	content, _ = r.render(context.Background(), req)
	if r.width != 20 || !utf8.ValidString(content) {
		t.Fatal("width change did not produce valid reflow")
	}
}

func TestRendererBoundsRowsAndRejectsCanceledWork(t *testing.T) {
	var r transcriptRenderer
	rows := make([]wire.Row, wire.MaxRows+1)
	for i := range rows {
		rows[i] = wire.Row{Key: string(rune(0x1000 + i)), Kind: "assistant_message", Text: "a"}
	}
	rows[len(rows)-1].Text = "beyond-page-bound"
	req := renderRequest{snapshot: &wire.Snapshot{Items: rows}, width: 60, theme: "cyan"}
	text, err := r.render(context.Background(), req)
	if err != nil || strings.Contains(text, "beyond-page-bound") || len(r.cache) > wire.MaxRows {
		t.Fatalf("page bound failed: rows=%d err=%v", len(r.cache), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.render(ctx, req); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled render returned %v", err)
	}
}

func TestRemoteKindsAndMarkdownControlBytesAreSanitized(t *testing.T) {
	var r transcriptRenderer
	s := &wire.Snapshot{Items: []wire.Row{{Key: "unsafe", Kind: "future\x1b]52;c;YWJj\a_kind", Text: "before\x1b[2J\x1b]52;c;YWJj\aafter"}}}
	out, err := r.render(context.Background(), renderRequest{snapshot: s, width: 40, theme: "violet"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "\x1b]52") || strings.Contains(out, "\x1b[2J") || !strings.Contains(ansi.Strip(out), "beforeafter") {
		t.Fatalf("remote control sequence escaped: %q", out)
	}
}

func TestTranscriptRolesHaveSeparateHeadersAndReadableSpacing(t *testing.T) {
	var r transcriptRenderer
	rows := []wire.Row{
		{Key: "u", Kind: "user_message", Text: "A question"},
		{Key: "a", Kind: "assistant_message", Text: "An answer", Streaming: true},
		{Key: "n", Kind: "system_notice", Text: "A notice"},
		{Key: "e", Kind: "error", Text: "An error"},
	}
	content, err := r.render(context.Background(), renderRequest{snapshot: &wire.Snapshot{Items: rows}, width: 60, theme: "violet"})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(ansi.Strip(content), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	plain := strings.Join(lines, "\n")
	for _, want := range []string{
		" › you\n ┃\n ┃ A question",
		" ● assistant · streaming\n │\n │ An answer",
		" · T3 notice\n ┊\n ┊ A notice",
		" ! T3 error\n ┃\n ┃ An error",
		"A question\n\n ● assistant",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing role hierarchy or spacing %q:\n%s", want, plain)
		}
	}
}

func TestTranscriptWrapsMarkdownCodeAndDiffsWithinAvailableWidth(t *testing.T) {
	source := "# Result\n\nA paragraph with **emphasis**, `inline code`, and [a link](https://example.com/a/long/path).\n\n" +
		"```go\nfunc main() {\n\tif ok {\n\t\tprintln(\"你好 👩🏽‍💻 café\")\n\t}\n}\n```\n\n" +
		"```diff\n-old value\n+new value with a very long continuation that must remain visible\n```\n\n" +
		"> A quotation\n\n- First item\n- Second item\n\n| Name | Value |\n| --- | --- |\n| alpha | beta |"
	for _, width := range []int{1, 2, 3, 4, 8, 12, 20, 42, 100, 180} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			var r transcriptRenderer
			content, err := r.render(context.Background(), renderRequest{
				snapshot: &wire.Snapshot{Items: []wire.Row{{Key: "a", Kind: "assistant_message", Text: source, Truncated: true, AttachmentCount: 2}}},
				width:    width, theme: "violet",
			})
			if err != nil {
				t.Fatal(err)
			}
			if !utf8.ValidString(content) {
				t.Fatal("wrapping broke a Unicode grapheme")
			}
			for _, line := range strings.Split(content, "\n") {
				if ansi.StringWidth(line) > width || strings.Contains(line, "\t") {
					t.Fatalf("line exceeds width %d or has an unmeasured tab: %q", width, line)
				}
				if width > 100 && ansi.StringWidth(line) > transcriptReadingWidth+3 {
					t.Fatalf("line exceeds comfortable reading measure: %q", line)
				}
			}
			plainLines := strings.Split(ansi.Strip(content), "\n")
			for i, line := range plainLines {
				plainLines[i] = strings.TrimRight(line, " ")
			}
			plain := strings.Join(plainLines, "\n")
			if width >= 42 {
				for _, want := range []string{"Result", "emphasis", "inline", "code", "-old value", "+new value", "你好 👩🏽‍💻 café", " │ func main() {\n │     if ok {", "alpha", "beta"} {
					if !strings.Contains(plain, want) {
						t.Fatalf("lost rendered Markdown/code content %q:\n%s", want, plain)
					}
				}
				if strings.Contains(plain, "```go") || strings.Contains(plain, "# Result") {
					t.Fatalf("Markdown syntax appeared instead of styled content:\n%s", plain)
				}
			}
		})
	}
}

func TestTranscriptUsesSelectedUpstreamSyntaxAndDiffTheme(t *testing.T) {
	var r transcriptRenderer
	content, err := r.render(context.Background(), renderRequest{
		snapshot: &wire.Snapshot{Items: []wire.Row{{Key: "a", Kind: "assistant_message", Text: "```go\nfunc main() {}\n```\n\n```diff\n-old\n+new\n```"}}},
		width:    60, theme: "violet",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Chroma's released Dracula palette, not DarkStyleConfig's global "charm"
	// palette: declarations are cyan; deleted/inserted diff tokens are red/green.
	for _, color := range []string{"\x1b[38;2;139;233;253m", "\x1b[38;2;255;85;85m", "\x1b[38;2;80;250;123m"} {
		if !strings.Contains(content, color) {
			t.Fatalf("selected upstream syntax theme not applied: missing %q", color)
		}
	}
}

func TestMarkdownEntitiesCannotReintroduceTerminalCommands(t *testing.T) {
	var r transcriptRenderer
	content, err := r.render(context.Background(), renderRequest{
		snapshot: &wire.Snapshot{Items: []wire.Row{{Key: "a", Kind: "assistant_message", Text: "before&#27;[2Jafter &#27;]52;c;YWJj&#7;safe &#13;end"}}},
		width:    80, theme: "violet",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(content, "\x1b[2J") || strings.Contains(content, "\x1b]52") || strings.ContainsAny(content, "\a\r") {
		t.Fatalf("Markdown decoded an unsafe terminal command: %q", content)
	}
	if plain := ansi.Strip(content); !strings.Contains(plain, "before") || !strings.Contains(plain, "after") || !strings.Contains(plain, "safe") {
		t.Fatalf("sanitizing entity commands lost surrounding text: %q", plain)
	}
}

func TestTranscriptByteCapIncludesSpacingAndLimitNotice(t *testing.T) {
	rows := make([]wire.Row, wire.MaxRows)
	for i := range rows {
		rows[i] = wire.Row{Key: fmt.Sprint(i), Kind: "assistant_message", Text: strings.Repeat("**x** ", wire.MaxTextBytes/6)}
	}
	var r transcriptRenderer
	content, err := r.render(context.Background(), renderRequest{snapshot: &wire.Snapshot{Items: rows}, width: 60, theme: "violet"})
	if err != nil {
		t.Fatal(err)
	}
	if len(content) > maxRenderedBytes || !strings.Contains(ansi.Strip(content), "Rendered page limit reached") {
		t.Fatalf("render cap or limit notice failed: %d bytes", len(content))
	}
	if len(r.cache) >= len(rows) {
		t.Fatal("cache retained rows that were not displayed")
	}
}

func TestTranscriptBoundsNativeTableExpansionBeforeWrapping(t *testing.T) {
	var r transcriptRenderer
	_, err := r.render(context.Background(), renderRequest{
		snapshot: &wire.Snapshot{Items: []wire.Row{{Key: "a", Kind: "assistant_message", Text: "Initialize the native renderer"}}},
		width:    60, theme: "violet",
	})
	if err != nil {
		t.Fatal(err)
	}
	// This valid-size source becomes a 36 MB padded table when Glamour's
	// word-wrap width is zero. Check its native output before the final cap
	// could hide excessive allocation by replacing the row with a notice.
	source := "|" + strings.Repeat("x", 12000) + "|\n|---|\n" + strings.Repeat("|x|\n", 3000)
	if len(source) > wire.MaxTextBytes {
		t.Fatal("table fixture exceeds the actual wire text bound")
	}
	native, err := r.markdown.Render(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(native) > maxRenderedBytes {
		t.Fatalf("native table layout expanded before the display cap: %d bytes", len(native))
	}
}

func TestTranscriptCacheDistinguishesNullsInKindAndText(t *testing.T) {
	var r transcriptRenderer
	req := renderRequest{
		snapshot: &wire.Snapshot{Items: []wire.Row{{Key: "same", Kind: "foo", Text: "bar\x00baz"}}},
		width:    60, theme: "violet",
	}
	if _, err := r.render(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	// These rows collided under the old NUL-delimited signature although
	// their sanitized headings and bodies are different.
	req.snapshot = &wire.Snapshot{Items: []wire.Row{{Key: "same", Kind: "foo\x00bar", Text: "baz"}}}
	content, err := r.render(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	plain := ansi.Strip(content)
	if !strings.Contains(plain, "foobar") || strings.Contains(plain, "barbaz") {
		t.Fatalf("cache reused a different row's header or body: %q", plain)
	}
}
