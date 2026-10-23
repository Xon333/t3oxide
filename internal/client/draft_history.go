package client

import (
	"errors"
	"unicode/utf8"

	"github.com/Xon333/t3oxide/internal/config"
)

func (a *Session) PromptHistory() []config.PromptEntry {
	a.mu.Lock()
	env, thread := a.environment, a.thread
	a.mu.Unlock()
	if env == nil || thread == "" {
		return nil
	}
	return a.manager.State.PromptHistory(env.EnvironmentID, thread)
}

func (a *Session) RememberPrompt(kind, text string) error {
	a.mu.Lock()
	env, thread := a.environment, a.thread
	a.mu.Unlock()
	if env == nil || thread == "" {
		return errors.New("choose a conversation before saving recovery text")
	}
	return a.manager.State.RememberPrompt(env.EnvironmentID, thread, kind, text)
}

// StoreDraft synchronously preserves an explicit editor/recall/clear operation.
// It shares the acknowledgment/flush lock, and publishes only after persistence.
func (a *Session) StoreDraft(text string) error {
	if len(text) > 64*1024 || !utf8.ValidString(text) {
		return errors.New("draft must be valid UTF-8 and at most 64 KiB")
	}
	a.manager.draftPersistence.Lock()
	defer a.manager.draftPersistence.Unlock()
	a.manager.mu.Lock()
	defer a.manager.mu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.environment == nil || a.thread == "" {
		return errors.New("choose a conversation before saving the draft")
	}
	if err := a.manager.State.SetDraft(a.environment.EnvironmentID, a.thread, text); err != nil {
		return err
	}
	a.draft = text
	a.draftDirty = false
	a.draftError = nil
	a.draftVersion++
	a.manager.drafts[draftIdentity{a.environment.EnvironmentID, a.thread}] = text
	return nil
}
