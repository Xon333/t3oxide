package ui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
)

type textDraftCheckpoint struct {
	value string
	store func(string) error
}

func (m *Model) captureTextDraft() {
	if m.dialog == nil || m.dialog.prompt == nil || m.dialog.kind != client.EnterText || m.dialog.prompt.Question.StoreText == nil {
		return
	}
	value := m.modalInput.Value()
	if m.dialog.preservedInitial != "" {
		value = m.dialog.preservedInitial
	}
	checkpoint := &textDraftCheckpoint{value: value, store: m.dialog.prompt.Question.StoreText}
	m.launchDraftMu.Lock()
	m.textDraft = checkpoint
	m.launchDraftMu.Unlock()
}

func (m *Model) persistTextCheckpoint() error {
	m.launchDraftMu.Lock()
	defer m.launchDraftMu.Unlock()
	if m.textDraft == nil {
		return nil
	}
	if err := m.textDraft.store(m.textDraft.value); err != nil {
		return err
	}
	m.textDraft = nil
	return nil
}

func (m *Model) storeTextDraft() error {
	m.captureTextDraft()
	return m.persistTextCheckpoint()
}

func (m *Model) fileInboxMany() tea.Cmd {
	if m.session == nil {
		return nil
	}
	if m.actionBusy {
		m.notice = "An action is pending; Stop remains available"
		return nil
	}
	session := m.session
	return m.navigatorRun("file-many", func(ctx context.Context) (client.Result, error) { return session.FileMany(ctx) })
}
