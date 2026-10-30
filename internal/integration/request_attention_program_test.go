package integration_test

import (
	"strings"
	"testing"
)

// Manual review tests begin by explicitly closing both automatic forms. This
// exercises the accepted presentation-only close through the actual program,
// client and adapter while leaving each native request pending and reopenable.
func dismissInitialAutomaticRequestAttention(t *testing.T, r *tuiProgram, f *fixture) {
	t.Helper()
	for _, title := range []string{"T3 approval", "T3 questions"} {
		r.modalReady(title)
		r.send("\x1b")
		waitWorkspaceFrame(t, r, func(view string) bool { return !strings.Contains(view, "╭─ "+title) }, "closed automatic "+title)
		if effects, dispatches := f.counts(); effects != 0 || dispatches != 0 {
			t.Fatal("closing automatic request presentation sent a native response/dismiss command")
		}
	}
	if view := r.views.text(); !strings.Contains(strings.ToLower(view), "request") {
		t.Fatal("closed requests lost their persistent cue")
	}
}

func hasProgramReply(view, title string) bool {
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "╭─ Reply") && strings.Contains(line, title) {
			return true
		}
	}
	return false
}

func waitProgramReply(t *testing.T, r *tuiProgram, title string) {
	t.Helper()
	waitWorkspaceFrame(t, r, func(view string) bool { return hasProgramReply(view, title) }, "exact source reply composer")
}
