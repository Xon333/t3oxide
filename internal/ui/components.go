package ui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/atotto/clipboard"
)

const (
	maxInputBytes = 64 * 1024
	// Bubbles itself has a hard 10,000-line ceiling. Staying below it lets us
	// detect and reject an entire oversized insertion instead of retaining a
	// silently truncated prefix.
	maxInputLines = 9999
)

type componentMsg struct {
	generation, id uint64
	kind           string
	message        tea.Msg
}

type clipboardPasteMsg struct {
	text string
	err  error
}

func readClipboard() tea.Msg {
	text, err := clipboard.ReadAll()
	return clipboardPasteMsg{text, err}
}

// Bubbles may return asynchronous filter/clipboard/cursor messages. Carry the
// originating attachment and modal identity with those messages so a late
// result cannot paste into another conversation or change another request.
func (m *Model) componentCmd(kind string, id uint64, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	generation := m.generation
	var wrap func(tea.Cmd) tea.Cmd
	wrap = func(inner tea.Cmd) tea.Cmd {
		if inner == nil {
			return nil
		}
		return func() tea.Msg {
			message := inner()
			if batch, ok := message.(tea.BatchMsg); ok {
				commands := make(tea.BatchMsg, len(batch))
				for i, next := range batch {
					commands[i] = wrap(next)
				}
				return commands
			}
			return componentMsg{generation, id, kind, message}
		}
	}
	return wrap(cmd)
}

func (m *Model) updateComponent(kind string, id uint64, message tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch kind {
	case "composer":
		if m.preservedDraft != "" || !m.composer.Focused() {
			return nil
		}
		if msg, ok := message.(tea.KeyPressMsg); ok && key.Matches(msg, m.composer.KeyMap.Paste) {
			return m.componentCmd(kind, id, readClipboard)
		}
		message = m.clipboardInput(message)
		if m.rejectInput(message, "Draft") {
			return nil
		}
		old := m.composer.Value()
		m.composer, cmd = m.composer.Update(message)
		m.composerChanged(old)
		// Native DynamicHeight also recalculates on cursor-only updates. Keep
		// the display cap after those updates without limiting stored text.
		if m.composer.Value() == old {
			m.resize()
		}
	case "modal":
		if m.dialog == nil || m.dialog.serial != id || m.dialog.kind != client.EnterText || m.detail != "" {
			return nil
		}
		if m.dialog.preservedInitial != "" || !m.modalInput.Focused() {
			return nil
		}
		if msg, ok := message.(tea.KeyPressMsg); ok && key.Matches(msg, m.modalInput.KeyMap.Paste) {
			return m.componentCmd(kind, id, readClipboard)
		}
		message = m.clipboardInput(message)
		old := m.modalInput.Value()
		if m.rejectInput(message, "Answer") {
			return nil
		}
		m.modalInput, cmd = m.modalInput.Update(message)
		if len(m.modalInput.Value()) > maxInputBytes || strings.Count(m.modalInput.Value(), "\n") >= maxInputLines {
			m.modalInput.SetValue(old)
			m.setInputProblem("modal", id, "Answer limit is 64 KiB / 9,999 lines; this input was not added"+m.retainAttempt(inputText(message)))
		} else if m.modalInput.Value() != old {
			m.clearInputProblem("modal", id)
		}
	case "list":
		if m.dialog == nil || m.dialog.serial != id || m.dialog.kind == client.EnterText || m.detail != "" {
			return nil
		}
		return m.updateDialogList(message)
	case "search":
		if !m.searching {
			return nil
		}
		m.searchInput, cmd = m.searchInput.Update(message)
	}
	return m.componentCmd(kind, id, cmd)
}

func (m *Model) clipboardInput(message tea.Msg) tea.Msg {
	if msg, ok := message.(clipboardPasteMsg); ok {
		if msg.err != nil {
			m.problem = "Clipboard: " + safeText(msg.err.Error())
			return nil
		}
		return tea.PasteMsg{Content: msg.text}
	}
	return message
}

func inputText(message tea.Msg) string {
	switch msg := message.(type) {
	case tea.PasteMsg:
		return msg.Content
	case tea.KeyPressMsg:
		return msg.Text
	}
	return ""
}

func (m *Model) rejectInput(message tea.Msg, label string) bool {
	text := inputText(message)
	kind, id := "composer", uint64(0)
	if label == "Answer" && m.dialog != nil {
		kind, id = "modal", m.dialog.serial
	}
	reason := ""
	current, selection := m.composer.Value(), m.composer.SelectedText()
	if kind == "modal" {
		current, selection = m.modalInput.Value(), m.modalInput.SelectedText()
	}
	combinedBytes := len(current) - len(selection) + len(text)
	combinedNewlines := strings.Count(current, "\n") - strings.Count(selection, "\n") + strings.Count(text, "\n")
	if len(text) > maxInputBytes || strings.Count(text, "\n") >= maxInputLines || text != "" && (combinedBytes > maxInputBytes || combinedNewlines >= maxInputLines) {
		reason = label + " limit is 64 KiB / 9,999 lines; this input was not added"
	} else {
		for _, r := range text {
			if r == utf8.RuneError || r == '\t' || r == '\r' || unicode.IsControl(r) && r != '\n' {
				reason = label + " contains text this editor would change; this input was not added"
				break
			}
		}
	}
	if reason == "" {
		return false
	}
	m.setInputProblem(kind, id, reason+m.retainAttempt(text))
	return true
}
