package ui

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/viewport"
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
	kind, text, role     string
	streaming, truncated bool
	attachmentCount      int64
}

// The body and its shell have different invalidation domains. Attachment,
// role and truncation changes must not invoke the Markdown parser again.
// An overflow entry keeps only its bounded source signature, never its output.
type cachedRow struct {
	signature rowSignature
	bodyText  string
	plain     bool
	body      string
	rendered  string
	lines     int
	overflow  bool
	reading   *rowReadingMap
}
type transcriptInputRow struct {
	key       string
	signature rowSignature
}
type renderCounters struct {
	body, rich, plain, assembly uint64
}
type renderedRow struct {
	key          string
	start, lines int
	plain        bool
	reading      *rowReadingMap
}

// A reading position belongs to an authoritative item, rather than a line in
// the concatenated page. Keep the desired offset even when a reflow temporarily
// makes that item shorter; restoring the earlier layout can then restore it.
type readingAnchor struct {
	key                 string
	offset              int
	plain, sourceMapped bool
	sourceOffset        int
	sourceHash          [32]byte
}

type renderPageIdentity struct {
	thread, cursor string
	historical     bool
}

func pageIdentity(snapshot *wire.Snapshot) renderPageIdentity {
	meta := snapshot.Metadata
	id := renderPageIdentity{thread: meta.ThreadID, historical: meta.Historical}
	if meta.Historical && meta.HistoryCursor != nil {
		id.cursor = *meta.HistoryCursor
	}
	return id
}

type transcriptRenderer struct {
	width    int
	theme    string
	markdown *glamour.TermRenderer
	cache    map[string]cachedRow
	rows     []renderedRow
	layout   transcriptLayout
	inputs   []transcriptInputRow
	content  string
	complete bool
	counters renderCounters
}
type renderRequest struct {
	generation, revision, serial uint64
	page                         uint64
	snapshot                     *wire.Snapshot
	width                        int
	theme                        string
	reset, follow                bool
	searchTerm                   string
}
type renderedMsg struct {
	request  renderRequest
	content  string
	err      error
	rows     []renderedRow
	prepared *preparedTranscript
}

// render is used by one asynchronous Tea command at a time. Its cache is
// bounded by the currently displayed authoritative page, not conversation age.
func (r *transcriptRenderer) render(ctx context.Context, req renderRequest) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if req.snapshot == nil {
		return "", nil
	}
	width := max(1, req.width)
	layout := newTranscriptLayout(width)
	p := colors(req.theme)
	if r.layout != layout || r.theme != req.theme || r.markdown == nil {
		formatter := "terminal16m"
		if req.theme == "terminal" {
			formatter = "terminal16"
		}
		renderer, err := glamour.NewTermRenderer(
			glamour.WithStyles(transcriptMarkdownStyle(p)),
			// Keep the native table bound before padded cells are allocated.
			glamour.WithWordWrap(layout.bodyWidth),
			glamour.WithChromaFormatter(formatter),
		)
		if err != nil {
			return "", err
		}
		r.markdown = renderer
		r.layout, r.theme = layout, req.theme
		r.cache = make(map[string]cachedRow)
		r.complete = false
	}
	r.width = width
	// Control revisions and an equivalent outer width do not reassemble the
	// page. Exact structured signatures deliberately avoid delimiter/hash aliases.
	if r.complete && sameTranscriptRows(r.inputs, req.snapshot) {
		return r.content, nil
	}
	r.complete = false
	r.rows = nil
	next := make(map[string]cachedRow, min(len(req.snapshot.Items), wire.MaxRows))
	limitNotice := layout.wrap(p.subtle().Render("Display limit · Details in full T3"))
	var out strings.Builder
	var line int
	for index, row := range req.snapshot.Items {
		if index >= wire.MaxRows {
			break
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		signature := transcriptSignature(req.snapshot, row)
		cached, ok := r.cache[row.Key]
		if !ok || cached.signature != signature {
			plain := row.Streaming && (row.Kind == "assistant" || row.Kind == "assistant_message")
			body := cached.body
			if !ok || cached.overflow || cached.bodyText != row.Text || cached.plain != plain {
				source := safeText(row.Text)
				r.counters.body++
				if plain {
					r.counters.plain++
					body = p.plain().Render(source)
				} else {
					r.counters.rich++
					var err error
					body, err = r.markdown.Render(source)
					if err != nil {
						body = p.plain().Render(source)
					}
				}
				// Glamour itself is not cancellable. Check immediately after its bounded
				// conversion, before sanitation/wrapping, and before publication.
				if err := ctx.Err(); err != nil {
					return "", err
				}
				body = trimBlankLines(safeRenderedMarkdown(body))
				if strings.TrimSpace(ansi.Strip(body)) == "" {
					body = p.subtle().Render("(no text)")
				}
			}
			decoratedBody := body
			if row.Truncated {
				decoratedBody += "\n\n" + p.subtle().Render("Message shortened · Details in full T3")
			}
			if err := ctx.Err(); err != nil {
				return "", err
			}
			rendered := layout.message(row, decoratedBody, p, signature.role)
			cached = cachedRow{signature: signature, bodyText: row.Text, plain: plain, body: body, rendered: rendered, lines: strings.Count(rendered, "\n") + 1}
			// Intrinsic overflow is independent of prefix budget. Retain a bounded
			// negative entry so a changing badge or neighbouring row cannot repeat it.
			if len(rendered)+len("\n\n")+len(limitNotice) > maxRenderedBytes {
				cached = cachedRow{signature: signature, bodyText: row.Text, plain: plain, overflow: true}
			} else {
				cached.reading = prepareRowReading(row.Text, rendered)
			}
		}
		separator := ""
		if out.Len() > 0 {
			separator = "\n\n"
		}
		if cached.overflow || out.Len()+len(separator)+len(cached.rendered)+len("\n\n")+len(limitNotice) > maxRenderedBytes {
			if cached.overflow {
				next[row.Key] = cached
			}
			// A remaining-budget rejection is not intrinsic: dropping earlier rows
			// makes the same row eligible again on a subsequent authoritative page.
			if out.Len() > 0 {
				out.WriteString("\n\n")
			}
			out.WriteString(limitNotice)
			break
		}
		next[row.Key] = cached
		r.rows = append(r.rows, renderedRow{key: row.Key, start: line, lines: cached.lines, plain: cached.plain, reading: cached.reading})
		line += cached.lines + 1
		out.WriteString(separator)
		out.WriteString(cached.rendered)
	}
	if len(req.snapshot.Items) == 0 {
		out.WriteString(layout.wrap(p.subtle().Render("No messages on this page.\nUse the composer when Latest is synchronized.")))
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	r.cache = next
	r.inputs = captureTranscriptRows(req.snapshot)
	r.content = out.String()
	r.counters.assembly++
	r.complete = true
	return r.content, nil
}

func transcriptSignature(snapshot *wire.Snapshot, row wire.Row) rowSignature {
	return rowSignature{kind: row.Kind, text: row.Text, role: agentRole(snapshot, row), streaming: row.Streaming, truncated: row.Truncated, attachmentCount: row.AttachmentCount}
}

func sameTranscriptRows(inputs []transcriptInputRow, snapshot *wire.Snapshot) bool {
	if snapshot == nil || len(inputs) != min(len(snapshot.Items), wire.MaxRows) {
		return false
	}
	for i, input := range inputs {
		row := snapshot.Items[i]
		if input.key != row.Key || input.signature != transcriptSignature(snapshot, row) {
			return false
		}
	}
	return true
}

func captureTranscriptRows(snapshot *wire.Snapshot) []transcriptInputRow {
	rows := make([]transcriptInputRow, min(len(snapshot.Items), wire.MaxRows))
	for i := range rows {
		rows[i] = transcriptInputRow{key: snapshot.Items[i].Key, signature: transcriptSignature(snapshot, snapshot.Items[i])}
	}
	return rows
}

// Prepared content is single-owner: the render command constructs it, then
// transfers it to Update. No mutable viewport or line slice is shared with the
// live UI while work runs. This retains Bubbles' maintained viewport and moves
// its full-page width scan plus search sanitation off the event loop.
type preparedTranscript struct {
	content    string
	viewport   viewport.Model
	lowerLines []string
	term       string
	matches    []int
}

func prepareTranscript(content string, width int, term string) *preparedTranscript {
	v := viewport.New(viewport.WithWidth(width), viewport.WithHeight(1))
	v.SoftWrap = false
	v.FillHeight = true
	v.SetContent(content)
	lines := strings.Split(strings.ToLower(safeText(content)), "\n")
	return &preparedTranscript{content: content, viewport: v, lowerLines: lines, term: term, matches: findTranscriptLines(lines, term)}
}

func anchorAt(rows []renderedRow, line int) readingAnchor {
	for _, row := range rows {
		if line < row.start+row.lines {
			return rowAnchor(row, max(0, line-row.start))
		}
	}
	if len(rows) > 0 {
		row := rows[len(rows)-1]
		return rowAnchor(row, row.lines-1)
	}
	return readingAnchor{}
}

func remapAnchor(anchor readingAnchor, old, next []renderedRow) (readingAnchor, int) {
	index := make(map[string]renderedRow, len(next))
	for _, row := range next {
		index[row.key] = row
	}
	if row, ok := index[anchor.key]; ok {
		// Plain streaming to rich final layout can change line heights. If
		// exact source bytes survived, follow its prepared source position.
		// Replacements/shrink use the established row-offset fallback.
		if anchor.plain && !row.plain && anchor.sourceMapped && row.reading != nil && anchor.sourceHash == row.reading.hash {
			if mapped := lineAtSource(row.reading.offsets, anchor.sourceOffset); mapped >= 0 {
				anchor = rowAnchor(row, mapped)
			}
		}
		return anchor, row.start + min(anchor.offset, row.lines-1)
	}
	// Prefer the closest surviving neighbour, breaking ties toward the next
	// item. A fully replaced rolling window falls back to its first item.
	for i, row := range old {
		if row.key != anchor.key {
			continue
		}
		for distance := 1; distance < len(old); distance++ {
			for _, j := range []int{i + distance, i - distance} {
				if j >= 0 && j < len(old) {
					if retained, ok := index[old[j].key]; ok {
						return readingAnchor{key: retained.key}, retained.start
					}
				}
			}
		}
		break
	}
	if len(next) > 0 {
		return readingAnchor{key: next[0].key}, next[0].start
	}
	return readingAnchor{}, 0
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

// Snapshot lineage describes only its own thread. A borrowed/source row cannot
// acquire that relationship from its label, key, or the containing page.
func agentRole(snapshot *wire.Snapshot, row wire.Row) string {
	if row.SourceThreadID != "" && row.SourceThreadID == snapshot.Metadata.ThreadID {
		switch snapshot.Metadata.Lineage.Type() {
		case "subagent":
			return "Subagent"
		case "fork":
			return "Agent · fork"
		}
	}
	return "Agent"
}

func (l transcriptLayout) message(row wire.Row, body string, p palette, agentRole string) string {
	title := strings.NewReplacer("\n", " ", "\t", " ").Replace(safeText(row.Kind))
	marker, rail, tint := "·", "┊", p.muted
	switch row.Kind {
	case "user", "user_message":
		title, marker, rail, tint = "you", "›", "┃", p.secondary
	case "assistant", "assistant_message":
		title, marker, rail = agentRole, "●", "│"
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
	heading := p.subtle().Render(title)
	if row.Kind == "error" {
		heading = roleStyle.Bold(true).Render(title)
	}
	if row.Streaming {
		heading += p.operational().Render(" · streaming")
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
