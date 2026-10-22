package ui

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"charm.land/glamour/v2"
	gansi "charm.land/glamour/v2/ansi"
	gstyles "charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"
	"github.com/Xon333/t3oxide/internal/wire"
	"github.com/charmbracelet/x/ansi"
)

const (
	maxRenderedBytes       = 2 * 1024 * 1024
	transcriptReadingWidth = 96
)

type rowSignature struct {
	kind, text           string
	streaming, truncated bool
	attachmentCount      int64
}
type cachedRow struct {
	signature rowSignature
	rendered  string
}
type transcriptRenderer struct {
	width    int
	theme    string
	markdown *glamour.TermRenderer
	cache    map[string]cachedRow
}
type renderRequest struct {
	generation, revision, serial uint64
	snapshot                     *wire.Snapshot
	width                        int
	theme                        string
	reset, follow                bool
}
type renderedMsg struct {
	request renderRequest
	content string
	err     error
}

// render is used by one asynchronous Tea command at a time. Its cache is
// bounded by the currently displayed authoritative page, not conversation age.
func (r *transcriptRenderer) render(ctx context.Context, req renderRequest) (string, error) {
	if req.snapshot == nil {
		return "", nil
	}
	width := max(1, req.width)
	layout := newTranscriptLayout(width)
	p := colors(req.theme)
	if r.width != width || r.theme != req.theme || r.markdown == nil {
		formatter := "terminal16m"
		if req.theme == "terminal" {
			formatter = "terminal16"
		}
		renderer, err := glamour.NewTermRenderer(
			glamour.WithStyles(transcriptMarkdownStyle(p)),
			// Bound native table layout before it allocates padded cells. The
			// final ANSI hardwrap also contains tabs and wide code/diff tokens.
			glamour.WithWordWrap(layout.bodyWidth),
			glamour.WithChromaFormatter(formatter),
		)
		if err != nil {
			return "", err
		}
		r.markdown = renderer
		r.width, r.theme = width, req.theme
		r.cache = make(map[string]cachedRow)
	}
	next := make(map[string]cachedRow, min(len(req.snapshot.Items), wire.MaxRows))
	limitNotice := layout.wrap(p.subtle().Render("Rendered page limit reached · open full T3 for remaining content"))
	var out strings.Builder
	for index, row := range req.snapshot.Items {
		if index >= wire.MaxRows {
			break
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		signature := rowSignature{row.Kind, row.Text, row.Streaming, row.Truncated, row.AttachmentCount}
		cached, ok := r.cache[row.Key]
		if !ok || cached.signature != signature {
			source := safeText(row.Text)
			body, err := r.markdown.Render(source)
			if err != nil {
				body = p.plain().Render(source)
			}
			body = trimBlankLines(safeRenderedMarkdown(body))
			if strings.TrimSpace(ansi.Strip(body)) == "" {
				body = p.subtle().Render("(no text)")
			}
			if row.Truncated {
				body += "\n\n" + p.subtle().Render("Shortened by T3oxide · open full T3 for the rest")
			}
			cached = cachedRow{signature, layout.message(row, body, p)}
		}
		separator := ""
		if out.Len() > 0 {
			separator = "\n\n"
		}
		// Reserve the notice before accepting a row so the byte cap is strict,
		// including row spacing, ANSI styles, and the final notice itself.
		if out.Len()+len(separator)+len(cached.rendered)+len("\n\n")+len(limitNotice) > maxRenderedBytes {
			if out.Len() > 0 {
				out.WriteString("\n\n")
			}
			out.WriteString(limitNotice)
			break
		}
		next[row.Key] = cached
		out.WriteString(separator)
		out.WriteString(cached.rendered)
	}
	r.cache = next
	if len(req.snapshot.Items) == 0 {
		out.WriteString(layout.wrap(p.subtle().Render("No messages on this page.\nUse the composer when Latest is synchronized.")))
	}
	return out.String(), nil
}

// Base styles and rendering contracts are reused from released Glamour v2.0.1,
// commit 95c93db04489cebf252ec856584445c7e85ee276: styles/styles.go,
// ansi/codeblock.go, and ansi/blockelement.go. No Markdown is parsed here.
func transcriptMarkdownStyle(p palette) gansi.StyleConfig {
	style := gstyles.DarkStyleConfig
	zero, bold := uint(0), true
	style.Document.Margin = &zero
	style.Document.BlockPrefix, style.Document.BlockSuffix = "", ""
	style.Document.Color, style.Document.BackgroundColor = &p.fg, &p.bg
	style.Heading.Color = &p.accent
	style.H1.Color, style.H1.BackgroundColor = &p.accent, &p.bg
	style.H1.Prefix, style.H1.Suffix = "", ""
	style.H2.Prefix, style.H3.Prefix = "", ""
	style.H4.Prefix, style.H5.Prefix, style.H6.Prefix = "", "", ""
	style.H3.Color, style.H4.Color = &p.secondary, &p.secondary
	style.H5.Color, style.H6.Color, style.H6.Bold = &p.muted, &p.muted, &bold
	style.BlockQuote.Color = &p.muted
	style.HorizontalRule.Color = &p.muted
	style.Link.Color, style.LinkText.Color = &p.secondary, &p.secondary
	style.Image.Color, style.ImageText.Color = &p.secondary, &p.muted
	style.Code.Color, style.Code.BackgroundColor = &p.secondary, &p.elevated
	style.CodeBlock.Margin = &zero
	// A non-nil Chroma overrides Theme and registers a global "charm" style.
	// Clearing it selects the actual released Dracula syntax/diff palette.
	style.CodeBlock.Chroma, style.CodeBlock.Theme = nil, "dracula"
	if p.name == "terminal" {
		style.Document.Color, style.Document.BackgroundColor = nil, nil
		style.H5.Color, style.H6.Color = nil, nil
		style.BlockQuote.Color, style.HorizontalRule.Color = nil, nil
		style.ImageText.Color = nil
		style.H1.BackgroundColor, style.Code.BackgroundColor = nil, nil
		style.CodeBlock.Color, style.CodeBlock.BackgroundColor = nil, nil
		style.CodeBlock.Theme = terminalSyntax.Name
	}
	return style
}

type transcriptLayout struct {
	bodyWidth, margin int
	rail              bool
}

func newTranscriptLayout(width int) transcriptLayout {
	l := transcriptLayout{bodyWidth: min(transcriptReadingWidth, max(1, width))}
	if width >= 12 {
		l.margin, l.rail = 1, true
		l.bodyWidth = min(transcriptReadingWidth, width-4)
	} else if width >= 4 {
		l.rail = true
		l.bodyWidth = width - 2
	}
	return l
}

func (l transcriptLayout) message(row wire.Row, body string, p palette) string {
	title := strings.NewReplacer("\n", " ", "\t", " ").Replace(safeText(row.Kind))
	marker, rail, tint := "·", "┊", p.muted
	switch row.Kind {
	case "user", "user_message":
		title, marker, rail, tint = "you", "›", "┃", p.secondary
	case "assistant", "assistant_message":
		title, marker, rail, tint = "assistant", "●", "│", p.accent
	case "proposed_plan":
		title, marker, rail, tint = "proposed plan", "◇", "│", p.accent
	case "system_notice":
		title = "T3 notice"
	case "notification":
		title = "notification"
	case "error":
		title, marker, rail, tint = "T3 error", "!", "┃", p.error
	}
	roleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(tint))
	heading := roleStyle.Bold(true).Render(title)
	if row.Streaming {
		heading += p.subtle().Render(" · streaming")
	}
	if row.AttachmentCount > 0 {
		heading += p.subtle().Render(fmt.Sprintf(" · %d attachment(s)", row.AttachmentCount))
	}
	lines := strings.Split(l.wrap(heading+"\n\n"+body), "\n")
	if !l.rail {
		return strings.Join(lines, "\n")
	}
	prefix := strings.Repeat(" ", l.margin)
	for i, line := range lines {
		glyph := rail
		if i == 0 {
			glyph = marker
		}
		lines[i] = prefix + roleStyle.Render(glyph) + " " + line
	}
	return strings.Join(lines, "\n")
}

func (l transcriptLayout) wrap(text string) string {
	// Lip Gloss v2.0.6, commit 733ce53541bb688fd735524b665e0d96e35433bb,
	// wrap.go: WrapWriter resets/restores styles across the newlines we insert,
	// keeping syntax colors off the independently styled message rails.
	// The unconfigured native style expands tabs to four spaces without
	// aligning every line to the longest line before we wrap it.
	text = lipgloss.NewStyle().Render(text)
	// x/ansi v0.11.9, commit faa4adf95555331dfb439b716cc83767ecdadeb4,
	// ansi/wrap.go: Hardwrap retains graphemes and leading code whitespace.
	text = ansi.Hardwrap(text, l.bodyWidth, true)
	var out strings.Builder
	w := lipgloss.NewWrapWriter(&out)
	_, _ = w.Write([]byte(text))
	_ = w.Close()
	lines := strings.Split(out.String(), "\n")
	for i, line := range lines {
		// A wide grapheme cannot fit a one-cell terminal. Only this degenerate
		// case is clipped; normal wrapping keeps all displayed code/text.
		if ansi.StringWidth(line) > l.bodyWidth {
			lines[i] = ansi.Truncate(line, l.bodyWidth, "")
		}
	}
	return strings.Join(lines, "\n")
}

func trimBlankLines(text string) string {
	lines := strings.Split(text, "\n")
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[0])) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// Markdown entities can decode into control bytes after safeText. Keep only
// printable text, line breaks, tabs and SGR styles from the native renderer.
func safeRenderedMarkdown(text string) string {
	var out strings.Builder
	var state byte
	for len(text) > 0 {
		seq, _, n, next := ansi.DecodeSequence(text, state, nil)
		text, state = text[n:], next
		if strings.HasPrefix(seq, "\x1b[") && strings.HasSuffix(seq, "m") &&
			strings.Trim(seq[2:len(seq)-1], "0123456789;:") == "" {
			out.WriteString(seq)
		} else if len(seq) > 0 && seq[0] != '\x1b' {
			for _, ch := range seq {
				if ch == '\n' || ch == '\t' || !unicode.IsControl(ch) {
					out.WriteRune(ch)
				}
			}
		}
	}
	return out.String()
}

func safeText(text string) string {
	text = ansi.Strip(strings.ToValidUTF8(text, "�"))
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
}
func fitLine(text string, width int) string {
	text = strings.NewReplacer("\n", " ", "\t", " ").Replace(safeText(text))
	return ansi.Truncate(text, max(1, width), "…")
}
