package config

import (
	"errors"
	"strings"
)

// WorkspaceStrategy is only a reviewed native launch input. It never creates
// or deletes a checkout locally and never carries a source thread's worktree.
type WorkspaceStrategy struct {
	Type    string `json:"type"`
	BaseRef string `json:"baseRef,omitempty"`
}

func (w WorkspaceStrategy) Validate() error {
	if w.Type == "root" && w.BaseRef == "" {
		return nil
	}
	if w.Type == "worktree" && w.BaseRef != "" && len(w.BaseRef) <= 4096 && strings.TrimSpace(w.BaseRef) == w.BaseRef && !strings.ContainsAny(w.BaseRef, "\x00\n\r") {
		return nil
	}
	return errors.New("invalid native workspace strategy")
}
