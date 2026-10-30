package ui

import (
	"crypto/sha256"
	"strings"
	"unicode"
)

// This is a private, bounded value projection of the current content inputs.
// Control/request revisions remain authoritative in Model.snapshot and status.
type transcriptRenderInput struct {
	valid            bool
	generation, page uint64
	theme            string
	layout           transcriptLayout
	rows             []transcriptInputRow
}

func (input transcriptRenderInput) matchesContent(m *Model) bool {
	return input.valid && input.generation == m.generation && input.theme == m.theme && input.layout == newTranscriptLayout(m.contentWidth) && sameTranscriptRows(input.rows, m.snapshot)
}

func (input transcriptRenderInput) matches(m *Model) bool {
	return input.page == m.renderPage && input.matchesContent(m)
}

func captureTranscriptInput(m *Model) transcriptRenderInput {
	return transcriptRenderInput{valid: true, generation: m.generation, page: m.renderPage, theme: m.theme, layout: newTranscriptLayout(m.contentWidth), rows: captureTranscriptRows(m.snapshot)}
}

func findTranscriptLines(lines []string, term string) []int {
	if term == "" {
		return nil
	}
	term = strings.ToLower(term)
	var matches []int
	for index, line := range lines {
		if strings.Contains(line, term) {
			matches = append(matches, index)
		}
	}
	return matches
}

func (m *Model) installTranscript(msg renderedMsg) {
	prepared := msg.prepared
	if prepared == nil {
		// Direct component tests and compatibility callers can construct a
		// renderedMsg; production rendering always prepares off the UI lane.
		prepared = prepareTranscript(msg.content, m.contentWidth, m.searchTerm)
	}
	old := m.viewport
	v := prepared.viewport
	v.SetWidth(m.contentWidth)
	v.SetHeight(m.bodyHeight)
	v.KeyMap = old.KeyMap
	v.SoftWrap, v.FillHeight = old.SoftWrap, old.FillHeight
	v.MouseWheelEnabled, v.MouseWheelDelta = old.MouseWheelEnabled, old.MouseWheelDelta
	v.YPosition, v.Style, v.LeftGutterFunc = old.YPosition, old.Style, old.LeftGutterFunc
	v.HighlightStyle, v.SelectedHighlightStyle = old.HighlightStyle, old.SelectedHighlightStyle
	v.SetYOffset(old.YOffset())
	v.SetXOffset(old.XOffset())
	m.viewport = v
	m.transcript = msg.content
	m.transcriptSearch = prepared
	m.contentInstalls++
}

// A local reading map is prepared with each accepted row; it stores positions,
// not a second transcript or durable review state. Markdown punctuation can
// reflow away, so bounded word matching supplies a best-effort source anchor.
// It is used only for an exact-source plain -> rich transition. All other
// reflows retain the established row/line anchor and its clamping behavior.
type rowReadingMap struct {
	hash    [32]byte
	offsets []int
}
type sourceWord struct {
	text   string
	offset int
}

func readingWords(source string) []sourceWord {
	var words []sourceWord
	start := -1
	for index, r := range source {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if start < 0 {
				start = index
			}
		} else if start >= 0 {
			words = append(words, sourceWord{strings.ToLower(source[start:index]), start})
			start = -1
		}
	}
	if start >= 0 {
		words = append(words, sourceWord{strings.ToLower(source[start:]), start})
	}
	return words
}

func prepareRowReading(source, rendered string) *rowReadingMap {
	words := readingWords(safeText(source))
	lines := strings.Split(safeText(rendered), "\n")
	result := &rowReadingMap{hash: sha256.Sum256([]byte(source)), offsets: make([]int, len(lines))}
	cursor := 0
	for line, text := range lines {
		// An unmatched wrapped token is not evidence of source offset zero
		// (or of the preceding word). Keep row/line fallback for such lines.
		offset := -1
		if line >= 2 {
			found := false
			for _, word := range readingWords(text) {
				for index := cursor; index < min(cursor+128, len(words)); index++ {
					if word.text == words[index].text {
						offset, cursor, found = words[index].offset, index+1, true
						break
					}
				}
				if found {
					break
				}
			}
		}
		result.offsets[line] = offset
	}
	return result
}

func rowAnchor(row renderedRow, offset int) readingAnchor {
	anchor := readingAnchor{key: row.key, offset: offset, plain: row.plain}
	if row.reading != nil && offset >= 2 && offset < len(row.reading.offsets) && row.reading.offsets[offset] >= 0 {
		anchor.sourceMapped = true
		anchor.sourceOffset = row.reading.offsets[offset]
		anchor.sourceHash = row.reading.hash
	}
	return anchor
}

func lineAtSource(offsets []int, source int) int {
	line, best := -1, -1
	for index, offset := range offsets {
		if index < 2 || offset < 0 {
			continue
		}
		if offset > source {
			break
		}
		// Select the first matching text line rather than a later duplicate;
		// unmatched/blank lines were excluded above.
		if offset > best {
			line, best = index, offset
		}
	}
	return line
}
