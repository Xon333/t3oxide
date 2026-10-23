package ui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
)

type localPrompt struct {
	session *client.Session
	entry   config.PromptEntry
}

func (m *Model) promptHistory() []config.PromptEntry {
	var entries []config.PromptEntry
	for _, p := range m.localPrompts {
		if p.session == m.session {
			entries = append(entries, p.entry)
		}
	}
	if m.session != nil {
		entries = append(entries, m.session.PromptHistory()...)
	}
	return entries
}

func (m *Model) rememberPrompt(kind, text string) error {
	if text == "" {
		return nil
	}
	if m.session != nil {
		return m.session.RememberPrompt(kind, text)
	}
	m.rememberLocalPrompt(kind, text)
	return nil
}

func (m *Model) rememberLocalPrompt(kind, text string) {
	sum := sha256.Sum256([]byte(kind + "\x00" + text))
	entry := config.PromptEntry{ID: hex.EncodeToString(sum[:]), Kind: kind, Text: text}
	entries := []localPrompt{{session: m.session, entry: entry}}
	for _, e := range m.localPrompts {
		if (e.entry.ID != entry.ID || e.session != m.session) && len(entries) < 24 {
			entries = append(entries, e)
		}
	}
	m.localPrompts = entries
}

// retainAttempt never replaces the live draft. Unrepresentable/oversized text
// stays in a private file when bounded JSON recovery cannot preserve its bytes.
func (m *Model) retainAttempt(text string) string {
	if text == "" {
		return ""
	}
	if len(text) <= maxInputBytes && utf8.ValidString(text) {
		if err := m.rememberPrompt("attempt", text); err == nil {
			return " · /recall"
		}
	}
	path, err := writeEditorFile(text)
	if err != nil {
		// Disk failure must not discard the only copy of a rejected paste.
		m.rememberLocalPrompt("attempt", text)
		return " · /recall retains this attempt in memory; recovery save failed: " + safeText(err.Error())
	}
	return " · exact attempt saved at " + path
}

func (m *Model) storeDraft(text string) error {
	if m.session != nil {
		if err := m.session.StoreDraft(text); err != nil {
			return err
		}
	}
	m.clearInputProblem("composer", 0)
	m.restoreDraft(text)
	m.draftEdits++
	m.resize()
	m.refreshCompletion()
	return nil
}

func (m *Model) clearDraft() tea.Cmd {
	text := m.currentDraft()
	if text == "" {
		m.notice = "Draft is empty"
		return nil
	}
	if err := m.rememberPrompt("cleared", text); err != nil {
		m.problem = "Draft retained: " + safeText(err.Error())
		return nil
	}
	if err := m.storeDraft(""); err != nil {
		m.problem = "Draft retained: " + safeText(err.Error())
		return nil
	}
	m.notice = "Draft cleared · /recall"
	return m.restoreFocus()
}

func (m *Model) openRecall() tea.Cmd {
	if m.dialog != nil && m.dialog.prompt != nil {
		m.notice = "Finish or dismiss the request first"
		return nil
	}
	entries := m.promptHistory()
	if len(entries) == 0 {
		m.notice = "No prompts to recall"
		return nil
	}
	items := make([]list.Item, 0, len(entries))
	for _, e := range entries {
		title := strings.Join(strings.Fields(e.Text), " ")
		if len([]rune(title)) > 100 {
			title = string([]rune(title)[:100]) + "…"
		}
		items = append(items, choiceItem{id: e.ID, title: title, description: e.Text, command: "recall", args: e.ID})
	}
	m.dialogSerial++
	m.dialog = &dialog{serial: m.dialogSerial, title: "Recall prompt", local: "recall", list: m.newList("Recall prompt", items)}
	m.detail = ""
	m.searching = false
	m.refreshDialogDetails()
	m.resize()
	return m.restoreFocus()
}

func (m *Model) recallDraft(args string) tea.Cmd {
	if args == "" {
		return m.openRecall()
	}
	// IDs are data, never interpreted as commands, indexes or prompt text.
	for _, entry := range m.promptHistory() {
		if entry.ID != args {
			continue
		}
		old := m.currentDraft()
		if old != "" && old != entry.Text {
			if err := m.rememberPrompt("cleared", old); err != nil {
				m.problem = "Draft retained: " + safeText(err.Error())
				return nil
			}
		}
		if err := m.storeDraft(entry.Text); err != nil {
			m.problem = "Recall: " + safeText(err.Error())
			return nil
		}
		m.notice = "Prompt recalled"
		return m.restoreFocus()
	}
	m.problem = "That prompt is no longer in recall"
	return nil
}

func (m *Model) setInputProblem(kind string, id uint64, text string) {
	m.inputProblem, m.inputProblemKind, m.inputProblemID = text, kind, id
	m.problem = text
}
func (m *Model) clearInputProblem(kind string, id uint64) {
	if m.inputProblemKind != kind || m.inputProblemID != id {
		return
	}
	if m.problem == m.inputProblem {
		m.problem = ""
	}
	m.inputProblem, m.inputProblemKind = "", ""
}

func writeEditorFile(text string) (string, error) {
	f, err := os.CreateTemp("", "t3oxide-draft-*.txt")
	if err != nil {
		return "", err
	}
	path := f.Name()
	_, err = f.WriteString(text)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("save exact text: %w", err)
	}
	return path, nil
}
