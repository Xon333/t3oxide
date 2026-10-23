package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
)

func draftSessionModel(t *testing.T) (*Model, *config.State) {
	t.Helper()
	state, err := config.OpenState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	manager := client.NewManager([]config.Environment{{ID: "p", EnvironmentID: "env", Enabled: true}}, state)
	session, err := manager.NewSession("p", "thread")
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel(context.Background(), session, nil)
	t.Cleanup(func() { m.Close(); _ = manager.Close(); _ = state.Close() })
	return m, state
}

func TestUnsupportedPasteIsDurablyRecoverableWithoutChangingPriorDraft(t *testing.T) {
	m, state := draftSessionModel(t)
	original := "keep my draft"
	if err := m.storeDraft(original); err != nil {
		t.Fatal(err)
	}
	attempt := "\tfunc main() { � }\r\n\n "
	m.composer.SelectAll()
	m.Update(tea.PasteMsg{Content: attempt})
	if m.currentDraft() != original || m.session.Draft() != original {
		t.Fatal("paste replaced prior draft")
	}
	entries := state.PromptHistory("env", "thread")
	if len(entries) != 1 || entries[0].Text != attempt {
		t.Fatalf("paste cannot be recovered: %#v", entries)
	}
	m.openRecall()
	if m.dialog == nil || m.dialog.local != "recall" || m.currentDraft() != original {
		t.Fatal("opening recall changed draft")
	}
	m.cancelDialog()
	if m.currentDraft() != original {
		t.Fatal("cancel recall lost live draft")
	}
	m.recallDraft(entries[0].ID)
	if m.currentDraft() != attempt || m.session.Draft() != attempt {
		t.Fatal("recall changed exact bytes")
	}
	if m.composer.Focused() {
		t.Fatal("normalizing textarea can edit exact buffer")
	}
	m.clearDraft()
	if m.currentDraft() != "" || state.Draft("env", "thread") != "" {
		t.Fatal("clear did not persist")
	}
	m.recallDraft(entries[0].ID)
	if m.currentDraft() != attempt {
		t.Fatal("clear was not recoverable")
	}
}

func TestInputProblemClearsOnlyOnRelevantAcceptedChange(t *testing.T) {
	m := testModel(t)
	m.Update(tea.PasteMsg{Content: "a\tb"})
	problem := m.problem
	m.Update(keyPress(tea.KeyLeft))
	if m.problem != problem {
		t.Fatal("cursor move dismissed input rejection")
	}
	m.openPrompt(retiredPrompt(99, client.Question{Kind: client.EnterText}))
	m.restoreFocus()
	m.Update(tea.PasteMsg{Content: "answer"})
	if m.problem != problem {
		t.Fatal("unrelated modal edit dismissed composer rejection")
	}
	m.cancelDialog()
	m.Update(tea.PasteMsg{Content: "valid"})
	if m.problem != "" {
		t.Fatal("accepted composer edit retained old rejection")
	}
	m.Update(tea.PasteMsg{Content: "\t"})
	m.problem = "network failure"
	m.Update(tea.PasteMsg{Content: "next"})
	if m.problem != "network failure" {
		t.Fatal("edit dismissed operational error")
	}
	m.openPrompt(retiredPrompt(100, client.Question{Kind: client.EnterText}))
	m.restoreFocus()
	m.Update(tea.PasteMsg{Content: "\t"})
	if !strings.Contains(m.problem, "not added") {
		t.Fatal("modal input error missing")
	}
	m.Update(keyPress(tea.KeyLeft))
	if m.problem == "" {
		t.Fatal("modal cursor dismissed input error")
	}
	m.Update(tea.PasteMsg{Content: "accepted"})
	if m.problem != "" {
		t.Fatal("modal accepted edit retained rejection")
	}
}

func TestEditorReturnPreservesBytesAndRejectsStaleDrafts(t *testing.T) {
	for _, stale := range []string{"none", "edit", "generation"} {
		t.Run(stale, func(t *testing.T) {
			m, state := draftSessionModel(t)
			original := "original draft"
			m.storeDraft(original)
			result := editorFinishedMsg{generation: m.generation, edits: m.draftEdits, session: m.session, original: original}
			edited := " \tEdited �\r\n\n "
			path, err := writeEditorFile(edited)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Remove(path) })
			result.path = path
			if stale == "edit" {
				m.storeDraft("newer typing")
			}
			if stale == "generation" {
				m.setSession(nil)
			}
			result = readEditorResult(result, nil)
			m.Update(result)
			if stale == "none" {
				if m.currentDraft() != edited || state.Draft("env", "thread") != edited {
					t.Fatal("editor normalized bytes")
				}
			} else {
				if m.currentDraft() == edited {
					t.Fatal("stale editor overwrote current draft")
				}
				history := state.PromptHistory("env", "thread")
				if len(history) == 0 || history[0].Text != edited {
					t.Fatal("stale editor attempt was discarded")
				}
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("verified preserved editor staging was not removed")
			}
		})
	}
}

func TestEditorFailureAndOversizeKeepUniqueAttempt(t *testing.T) {
	m, state := draftSessionModel(t)
	m.storeDraft("original")
	for _, edited := range []string{"\tedited\r\n�", strings.Repeat("x", maxInputBytes+1), string([]byte{255, 254})} {
		path, err := writeEditorFile(edited)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Remove(path) })
		msg := readEditorResult(editorFinishedMsg{generation: m.generation, edits: m.draftEdits, session: m.session, original: "original", path: path}, errors.New("canceled"))
		m.Update(msg)
		if m.currentDraft() != "original" || state.Draft("env", "thread") != "original" {
			t.Fatal("failed editor changed original")
		}
		if len(edited) <= maxInputBytes && edited[0] != 255 {
			entries := state.PromptHistory("env", "thread")
			if len(entries) == 0 || entries[0].Text != edited {
				t.Fatal("failed editor discarded unique attempt")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("recoverable attempt staging retained")
			}
		} else {
			got, err := os.ReadFile(path)
			if err != nil || string(got) != edited || !strings.Contains(m.problem, path) {
				t.Fatal("unrepresentable editor file not retained with exact bytes")
			}
		}
	}
}

func TestEditorCommandHonorsShellQuotingAndPassesFilenameAsData(t *testing.T) {
	t.Setenv("VISUAL", `printf ' \t�\r\n\n' > "$1"; :`)
	dir := t.TempDir()
	path := filepath.Join(dir, "draft $(touch injected).txt")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := editorCommand(context.Background(), path)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != " \t�\r\n\n" {
		t.Fatalf("quoted editor/filename changed bytes: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "injected")); !os.IsNotExist(err) {
		t.Fatal("editor filename was interpreted as shell code")
	}
}

func TestEmptyEditorSaveIsRecoverableClear(t *testing.T) {
	m, state := draftSessionModel(t)
	original := " \tprior �\r\n"
	if err := m.storeDraft(original); err != nil {
		t.Fatal(err)
	}
	path, err := writeEditorFile("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })
	msg := readEditorResult(editorFinishedMsg{generation: m.generation, edits: m.draftEdits, session: m.session, original: original, path: path}, nil)
	m.Update(msg)
	entries := state.PromptHistory("env", "thread")
	if m.currentDraft() != "" || len(entries) != 1 || entries[0].Kind != "cleared" || entries[0].Text != original {
		t.Fatal("empty editor save discarded the original draft")
	}
}

func TestClearDoesNotDestroyDraftWhenRecoverySaveFails(t *testing.T) {
	m, state := draftSessionModel(t)
	m.storeDraft("unique draft")
	state.Close()
	m.clearDraft()
	if m.currentDraft() != "unique draft" || m.session.Draft() != "unique draft" {
		t.Fatal("failed recovery save destroyed live draft")
	}
}

func TestRejectedAttemptSurvivesPersistenceFailureInMemory(t *testing.T) {
	m, state := draftSessionModel(t)
	if err := m.storeDraft("original"); err != nil {
		t.Fatal(err)
	}
	state.Close()
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	attempt := "unique\tattempt\r\n�"
	m.Update(tea.PasteMsg{Content: attempt})
	entries := m.promptHistory()
	if m.currentDraft() != "original" || len(entries) != 1 || entries[0].Text != attempt || !strings.Contains(m.problem, "in memory") {
		t.Fatal("failed persistence discarded a rejected paste")
	}
}

func TestSendTextRejectsOversizeCanonicalDraftWithoutStartingAction(t *testing.T) {
	m := testModel(t)
	for _, text := range []string{strings.Repeat("x", maxInputBytes+1), strings.Repeat("\n", maxInputLines) + "x"} {
		if m.sendText(text) != nil || m.actionBusy || !strings.Contains(m.problem, "edit before sending") {
			t.Fatal("oversize canonical draft submitted")
		}
	}
}

func TestCombinedPasteLimitRetainsRawAttemptBeforeTextareaTruncation(t *testing.T) {
	for _, modal := range []bool{false, true} {
		t.Run(map[bool]string{false: "composer", true: "modal"}[modal], func(t *testing.T) {
			m, state := draftSessionModel(t)
			original := strings.Repeat("old\n", 5000)
			attempt := strings.Repeat("new\n", 6000) + "unique tail"
			if err := m.storeDraft(original); err != nil {
				t.Fatal(err)
			}
			if modal {
				m.openPrompt(retiredPrompt(123, client.Question{Kind: client.EnterText, Initial: original}))
				m.restoreFocus()
			}
			m.Update(tea.PasteMsg{Content: attempt})
			if m.currentDraft() != original || modal && m.modalInput.Value() != original {
				t.Fatal("combined paste changed prior input")
			}
			entries := state.PromptHistory("env", "thread")
			if len(entries) != 1 || entries[0].Text != attempt {
				t.Fatal("combined paste lost its raw tail")
			}
			if modal {
				m.modalInput.SelectAll()
			} else {
				m.composer.SelectAll()
			}
			m.Update(tea.PasteMsg{Content: "replacement"})
			if modal && m.modalInput.Value() != "replacement" || !modal && m.currentDraft() != "replacement" {
				t.Fatal("selection was not included in combined budget")
			}
		})
	}
}
