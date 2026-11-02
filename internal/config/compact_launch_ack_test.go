package config

import (
	"path/filepath"
	"testing"
)

func TestRecoveredCompactLaunchAcknowledgesOnlyItsUnchangedMessage(t *testing.T) {
	for _, newer := range []bool{false, true} {
		name := "submitted"
		if newer {
			name = "newer"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			state, err := OpenState(path)
			if err != nil {
				t.Fatal(err)
			}
			submitted := LaunchDraft{Title: "Original", Message: " exact message\n"}
			if err = state.SetLaunchDraft("t3oxide-client", "new-message", submitted); err != nil {
				t.Fatal(err)
			}
			fields := map[string]any{"id": "op", "op": "launch-project", "environmentId": "env", "threadId": "created", "newThreadId": "created", "title": submitted.Title, "message": submitted.Message, "compactMessageDraft": true}
			if err = state.SavePending("env", false, fields); err != nil {
				t.Fatal(err)
			}
			expected := submitted
			if newer {
				expected = LaunchDraft{Title: "Newer", Message: "do not erase this"}
				if err = state.SetLaunchDraft("t3oxide-client", "new-message", expected); err != nil {
					t.Fatal(err)
				}
			}
			if err = state.Close(); err != nil {
				t.Fatal(err)
			}
			state, err = OpenState(path)
			if err != nil {
				t.Fatal(err)
			}
			defer state.Close()
			if err = state.Acknowledge("env", false, "op", "created", ""); err != nil {
				t.Fatal(err)
			}
			draft, exists := state.LaunchDraft("t3oxide-client", "new-message")
			if newer {
				if !exists || !launchDraftEqual(draft, expected) {
					t.Fatal("recovery erased newer message")
				}
			} else if exists || state.Draft("t3oxide-client", "\x00new/new-message") != "" {
				t.Fatal("recovery retained submitted message")
			}
			if state.Pending("env", false) != nil {
				t.Fatal("acknowledgment retained operation")
			}
		})
	}
}
