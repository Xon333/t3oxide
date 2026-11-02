package ui

import (
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
)

func (m *Model) loadedOutline() tea.Cmd {
	if m.snapshot == nil {
		return nil
	}
	items := []list.Item{}
	for i, row := range m.snapshot.Items {
		items = append(items, choiceItem{title: fmt.Sprintf("%d · %s", i+1, row.Kind), description: row.Text, command: "loaded-jump", args: fmt.Sprintf("%d:%x:%s", m.generation, sha256.Sum256([]byte(row.Text)), row.Key)})
	}
	m.openLocal("Loaded messages", "loaded-outline", items)
	return nil
}
func (m *Model) loadedJump(token string) tea.Cmd {
	if m.snapshot == nil {
		return nil
	}
	for _, row := range m.snapshot.Items {
		if token != fmt.Sprintf("%d:%x:%s", m.generation, sha256.Sum256([]byte(row.Text)), row.Key) {
			continue
		}
		for _, drawn := range m.transcriptRows {
			if drawn.key == row.Key {
				m.dialog = nil
				m.detail = ""
				m.transcriptFocus = true
				m.navigator.focused = false
				m.follow = false
				m.viewport.SetYOffset(drawn.start)
				return m.restoreFocus()
			}
		}
	}
	m.notice = "Loaded message changed; reopen the outline"
	return nil
}

// Retain source byte slices including CRLF and tabs. An unclosed fence is not
// offered as a complete code block. Display wrapping never enters this path.
func loadedCodeBlocks(text string) []string {
	var blocks []string
	marker := ""
	start := 0
	for pos := 0; pos < len(text); {
		end := strings.IndexByte(text[pos:], '\n')
		if end < 0 {
			end = len(text)
		} else {
			end += pos + 1
		}
		line := strings.TrimSuffix(strings.TrimSuffix(text[pos:end], "\n"), "\r")
		// CommonMark permits up to three leading spaces and a closing fence
		// at least as long as its opener. Only delimiters are interpreted;
		// the copied body remains an untouched source-byte slice.
		fence := line
		for i := 0; i < 3 && strings.HasPrefix(fence, " "); i++ {
			fence = fence[1:]
		}
		if marker == "" {
			if strings.HasPrefix(fence, "```") || strings.HasPrefix(fence, "~~~") {
				ch, n := fence[0], 0
				for n < len(fence) && fence[n] == ch {
					n++
				}
				if ch == '`' && strings.ContainsRune(fence[n:], '`') {
					pos = end
					continue
				}
				marker, start = fence[:n], end
			}
		} else if strings.HasPrefix(fence, marker) {
			n := 0
			for n < len(fence) && fence[n] == marker[0] {
				n++
			}
			if strings.Trim(fence[n:], " \t") == "" {
				blocks = append(blocks, text[start:pos])
				marker = ""
			}
		}
		pos = end
	}
	return blocks
}
func (m *Model) copyLoadedCode(args string) tea.Cmd {
	if m.snapshot == nil {
		return nil
	}
	if args == "" {
		items := []list.Item{}
		for _, row := range m.snapshot.Items {
			for i, code := range loadedCodeBlocks(row.Text) {
				items = append(items, choiceItem{title: fmt.Sprintf("%s · code %d", row.Kind, i+1), description: code, command: "copy-code-source", args: fmt.Sprintf("%d:%x:%d:%s", m.generation, sha256.Sum256([]byte(row.Text)), i, row.Key)})
			}
		}
		m.openLocal("Copy loaded code", "copy-code", items)
		return nil
	}
	for _, row := range m.snapshot.Items {
		for i, code := range loadedCodeBlocks(row.Text) {
			token := strconv.FormatUint(m.generation, 10) + fmt.Sprintf(":%x:%d:%s", sha256.Sum256([]byte(row.Text)), i, row.Key)
			if token == args {
				return m.copySourceBytes(redactSourceBytes(code))
			}
		}
	}
	m.notice = "Code source changed; select it again"
	return nil
}
func (m *Model) copySourceBytes(text string) tea.Cmd {
	if text == "" {
		m.notice = "No source text to copy"
		return nil
	}
	if len(text) > 256*1024 {
		m.problem = "Source exceeds clipboard limit"
		return nil
	}
	ctx := m.ctx
	return tea.Batch(tea.SetClipboard(text), func() tea.Msg { return copyResultMsg{nativeClipboard(ctx, text)} })
}

func redactSourceBytes(text string) string {
	for _, secret := range feedbackSecrets {
		text = secret.pattern.ReplaceAllString(text, secret.replace)
	}
	return text
}
