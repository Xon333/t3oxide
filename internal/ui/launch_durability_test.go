package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
)

// Use the real atomic State writer/reopen contract. Client tests separately
// exercise the production callback's exact source binding; this callback is
// the UI boundary and deliberately differs from the active composer identity.
func launchStateModel(t *testing.T) (*Model, *config.State, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.json")
	state, err := config.OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.Close() })
	if err = state.SetDraft("source-env", "source-thread", "source composer"); err != nil {
		t.Fatal(err)
	}
	m := testModel(t)
	m.composer.SetValue("active composer")
	q := launchQuestion(client.LaunchInput{Title: "retained title", Message: "first"})
	q.LaunchReview.StoreDraft = func(input client.LaunchInput) error {
		return state.SetDraft("source-env", "\x00new/source-thread", input.Message)
	}
	m.openPrompt(retiredPrompt(6200, q))
	m.restoreFocus()
	return m, state, path
}

func reopenLaunchDraft(t *testing.T, m *Model, state *config.State, path, want string) {
	t.Helper()
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if err := state.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := config.OpenState(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := reopened.Draft("source-env", "\x00new/source-thread"); got != want {
		t.Fatalf("reopened launch draft=%q, want exact %q", got, want)
	}
	if reopened.Draft("source-env", "source-thread") != "source composer" || m.currentDraft() != "active composer" {
		t.Fatal("launch shutdown wrote to a source/active composer slot")
	}
}

func TestLaunchTypingQuitPersistsBeforeCancellationAndStateReopen(t *testing.T) {
	m, state, path := launchStateModel(t)
	m.Update(tea.PasteMsg{Content: " message\n界 "})
	want := m.launchInput().Message
	_, cmd := m.Update(keyPress('q', tea.ModCtrl))
	if cmd == nil || !m.closed || m.ctx.Err() == nil {
		t.Fatal("explicit quit did not complete")
	}
	if state.Draft("source-env", "\x00new/source-thread") != want {
		t.Fatal("quit canceled before saving the live first message")
	}
	reopenLaunchDraft(t, m, state, path, want)
}

func TestLaunchContextShutdownPreservesRetiredPromptCheckpoint(t *testing.T) {
	m, state, path := launchStateModel(t)
	m.Update(tea.PasteMsg{Content: " context shutdown\nexact"})
	want := m.launchInput().Message
	m.cancel() // Program's parent context retires the prompt before deferred Close.
	m.Update(promptClosedMsg{generation: m.generation, id: m.dialog.prompt.ID})
	if m.dialog != nil {
		t.Fatal("test did not retire the live review")
	}
	reopenLaunchDraft(t, m, state, path, want)
}

func TestLaunchSuccessfulEditorQuitPersistsBeforeRemovingOnlyFile(t *testing.T) {
	m, state, path := launchStateModel(t)
	canonical := "\texternal first message\r\n�\n "
	editorPath, err := writeEditorFile("first")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(editorPath) })
	if err = os.WriteFile(editorPath, []byte(canonical), 0600); err != nil {
		t.Fatal(err)
	}
	result := readEditorResult(editorFinishedMsg{generation: m.generation, launchSerial: m.dialog.serial, path: editorPath, original: "first"}, nil)
	m.Update(result)
	if _, err = os.Stat(editorPath); !os.IsNotExist(err) {
		t.Fatal("successfully persisted editor file was not consumed")
	}
	if state.Draft("source-env", "\x00new/source-thread") != canonical {
		t.Fatal("editor file removed before exact first-message bytes were durable")
	}
	if m.launchInput().Title != "retained title" {
		t.Fatal("editor return lost title")
	}
	m.Update(keyPress('q', tea.ModCtrl))
	reopenLaunchDraft(t, m, state, path, canonical)
}

func TestLaunchSaveFailureKeepsQuitFormAndUniqueEditorFile(t *testing.T) {
	m, state, _ := launchStateModel(t)
	if err := state.Close(); err != nil {
		t.Fatal(err)
	}
	m.Update(tea.PasteMsg{Content: " unsaved"})
	input := m.launchInput()
	_, cmd := m.Update(keyPress('q', tea.ModCtrl))
	if cmd != nil || m.closed || m.ctx.Err() != nil || m.launchInput() != input || !strings.Contains(m.problem, "Launch draft save") {
		t.Fatal("failed save quit or discarded the live form")
	}
	editorPath, err := writeEditorFile("unique edit")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(editorPath) })
	m.Update(readEditorResult(editorFinishedMsg{generation: m.generation, launchSerial: m.dialog.serial, path: editorPath, original: input.Message}, nil))
	if _, err = os.Stat(editorPath); err != nil {
		t.Fatal("failed durable save discarded editor's unique copy")
	}
	if m.launchInput().Message != "unique edit" || !strings.Contains(m.problem, "editor file retained") {
		t.Fatal("failed save did not retain edited form and recovery path")
	}
	if err = m.Close(); err == nil {
		t.Fatal("Close hid durable launch save failure")
	}
}

func TestLaunchSuccessfulCreateCannotResurrectCompletedDraftOnClose(t *testing.T) {
	m, state, path := launchStateModel(t)
	m.Update(tea.PasteMsg{Content: " created"})
	m.launchAction("create")
	if err := state.SetDraft("source-env", "\x00new/source-thread", ""); err != nil {
		t.Fatal(err)
	}
	reopenLaunchDraft(t, m, state, path, "")
}

func TestNavigationTransitionWheelAndFallbackCannotMoveHiddenConversation(t *testing.T) {
	for _, surface := range []string{"browser", "launch"} {
		t.Run(surface, func(t *testing.T) {
			m := testModel(t)
			m.transcript = strings.Repeat("line\n", 60)
			m.viewport.SetContent(m.transcript)
			m.follow = false
			m.viewport.SetYOffset(9)
			m.composer.SetValue("exact hidden composer")
			m.composer.CursorStart()
			m.composer.Focus()
			m.browserPage = surface == "browser"
			m.launchPage = surface == "launch"
			m.dialog = nil
			m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
			m.Update(tea.PasteMsg{Content: "hidden paste"})
			if m.viewport.YOffset() != 9 || m.follow || m.currentDraft() != "exact hidden composer" || m.composer.Column() != 0 {
				t.Fatal("transition event reached hidden viewport/editor")
			}
		})
	}
}
