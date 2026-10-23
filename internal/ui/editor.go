package ui

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
)

type editorFinishedMsg struct {
	generation, edits    uint64
	session              *client.Session
	original, path, text string
	err                  error
	read                 bool
}

// Editor configuration is a trusted shell command, like Git's core.editor.
// The generated filename is passed as a positional argument, never interpolated.
func editorCommand(ctx context.Context, path string) *exec.Cmd {
	editor := os.Getenv("VISUAL")
	if strings.TrimSpace(editor) == "" {
		editor = os.Getenv("EDITOR")
	}
	if strings.TrimSpace(editor) == "" {
		editor = "vi"
	}
	return exec.CommandContext(ctx, "/bin/sh", "-c", editor+` "$@"`, "t3oxide-editor", path)
}

func (m *Model) editDraft() tea.Cmd {
	if m.editorBusy {
		m.notice = "Editor is already open"
		return nil
	}
	if m.dialog != nil && m.dialog.prompt != nil {
		m.notice = "Finish or dismiss the request first"
		return nil
	}
	original := m.currentDraft()
	// Preserve the pre-edit buffer before handing the terminal to another process.
	if m.session != nil {
		if err := m.session.StoreDraft(original); err != nil {
			m.problem = "Editor: " + safeText(err.Error())
			return nil
		}
	}
	path, err := writeEditorFile(original)
	if err != nil {
		m.problem = "Editor: " + safeText(err.Error())
		return nil
	}
	result := editorFinishedMsg{generation: m.generation, edits: m.draftEdits, session: m.session, original: original, path: path}
	m.editorBusy = true
	return tea.ExecProcess(editorCommand(m.ctx, path), func(runErr error) tea.Msg {
		return readEditorResult(result, runErr)
	})
}

func readEditorResult(result editorFinishedMsg, runErr error) editorFinishedMsg {
	result.err = runErr
	f, err := os.Open(result.path)
	if err == nil {
		// Large results remain intact on disk, without an unbounded UI read.
		var content []byte
		content, err = io.ReadAll(io.LimitReader(f, maxInputBytes+1))
		_ = f.Close()
		if err == nil && len(content) <= maxInputBytes {
			result.text, result.read = string(content), true
		}
		if err == nil && len(content) > maxInputBytes {
			err = fmt.Errorf("editor result exceeds 64 KiB")
		}
	}
	if err != nil {
		if runErr != nil {
			result.err = fmt.Errorf("%w; read edited draft: %v", runErr, err)
		} else {
			result.err = fmt.Errorf("read edited draft: %w", err)
		}
	}
	return result
}

func (m *Model) finishEditor(msg editorFinishedMsg) tea.Cmd {
	m.editorBusy = false
	stale := msg.generation != m.generation || msg.edits != m.draftEdits || msg.original != m.currentDraft()
	valid := msg.read && utf8.ValidString(msg.text) && len(msg.text) <= maxInputBytes && strings.Count(msg.text, "\n") < maxInputLines
	if msg.read && !valid && msg.err == nil {
		if !utf8.ValidString(msg.text) {
			msg.err = fmt.Errorf("editor result is not valid UTF-8")
		} else {
			msg.err = fmt.Errorf("editor result exceeds 64 KiB / 9,999 lines")
		}
	}
	if msg.err == nil && !stale && valid {
		// Saving an empty editor buffer is an explicit clear too.
		if msg.text == "" && msg.original != "" {
			msg.err = m.rememberPrompt("cleared", msg.original)
		}
		if msg.err == nil {
			msg.err = m.storeDraft(msg.text)
		}
		if msg.err == nil {
			var err error
			if err = os.Remove(msg.path); err != nil {
				m.notice = "Draft saved; editor file retained at " + msg.path
			} else {
				m.notice = "Draft saved from editor"
			}
			return m.restoreFocus()
		}
	}
	// On cancellation/failure/stale return, retain both the original live draft
	// and any unique edited attempt. A different attachment never receives it.
	preserved := msg.read && msg.text == msg.original
	if msg.read && !preserved && utf8.ValidString(msg.text) && len(msg.text) <= maxInputBytes {
		var err error
		if msg.session != nil {
			err = msg.session.RememberPrompt("attempt", msg.text)
		} else if !stale {
			err = m.rememberPrompt("attempt", msg.text)
		} else {
			err = fmt.Errorf("original attachment is unavailable")
		}
		preserved = err == nil
	}
	if preserved {
		if err := os.Remove(msg.path); err != nil {
			preserved = false
		}
	}
	detail := "Editor returned; original draft retained"
	if msg.err != nil {
		detail += ": " + safeText(msg.err.Error())
	}
	if stale {
		detail = "Editor result belongs to an older draft; current draft retained"
	}
	if preserved {
		detail += " · F5 recalls any edited attempt"
	} else {
		detail += " · edited file retained at " + msg.path
	}
	m.problem = detail
	return m.restoreFocus()
}
