package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
	"github.com/charmbracelet/x/ansi"
)

func TestAgentBoxSessionSwitchRetiresReviewReads(t *testing.T) {
	state, err := config.OpenState(filepath.Join(t.TempDir(), "drafts.json"))
	if err != nil {
		t.Fatal(err)
	}
	manager := client.NewManager([]config.Environment{{ID: "profile", EnvironmentID: "host", Enabled: true}}, state)
	t.Cleanup(func() { _ = manager.Close(); _ = state.Close() })
	a, err := manager.NewSession("profile", "a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := manager.NewSession("profile", "b")
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel(context.Background(), a, nil)
	t.Cleanup(func() { _ = m.Close() })
	oldTarget := client.InboxTarget{ProfileID: "profile", EnvironmentID: "host", ThreadID: "a"}
	if m.loadAttachedReview(oldTarget, "Old review") == nil || !m.agentbox.reviewLoading {
		t.Fatal("initial review did not start")
	}
	old := inboxReviewMsg{generation: m.generation, serial: m.agentbox.reviewSerial, target: oldTarget, review: wire.ThreadReview{Diff: "old changes"}}
	m.setSession(b)
	if m.agentbox.reviewLoading {
		t.Fatal("session switch retained the old review's loading state")
	}
	target := client.InboxTarget{ProfileID: "profile", EnvironmentID: "host", ThreadID: "b"}
	if m.loadAttachedReview(target, "New review") == nil || !m.agentbox.reviewLoading {
		t.Fatal("new session could not start a review")
	}
	m.Update(old)
	if !m.agentbox.reviewLoading || m.agentbox.review != nil || m.detail != "" {
		t.Fatal("stale completion changed the new review")
	}
	m.Update(inboxReviewMsg{generation: m.generation, serial: m.agentbox.reviewSerial, target: target, review: wire.ThreadReview{ThreadID: "b", Diff: "new changes"}, title: "New review"})
	if m.agentbox.reviewLoading || m.agentbox.review == nil || m.agentbox.reviewTarget != target || !strings.Contains(m.detail, "new changes") {
		t.Fatal("current review did not finish on the new session")
	}
}

func seedAgentBox(m *Model) {
	m.mode = "agentbox"
	m.agentbox.loaded = true
	m.agentbox.view = "needs"
	for _, item := range []struct{ id, title, status, env string }{{"alpha", "Review export flow", "completed", "one"}, {"beta", "Add keyboard navigation", "completed", "two"}, {"gamma", "Check error handling", "running", "one"}} {
		target := client.InboxTarget{ProfileID: item.env, EnvironmentID: "env-" + item.env, ProjectID: "project", ThreadID: item.id, Revision: strings.Repeat("a", 64)}
		row := client.InboxThread{Target: target, Environment: "Host " + item.env, Thread: client.Thread{ID: item.id, ProjectID: "project", Project: "Demo", Title: item.title, Status: item.status, UpdatedAt: "2026-10-29T12:00:00Z", Inbox: &wire.InboxMetadata{NativeRevision: target.Revision, UpdatedAt: "2026-10-29T12:00:00Z", QueuedCount: 0, Workspace: "/workspace/demo-task", Branch: "task/demo", LatestMessage: &wire.InboxMessage{Role: "assistant", Text: "The export flow now preserves the selected destination. Review the actual changes before deciding what to do next."}}}}
		m.agentbox.page.Rows = append(m.agentbox.page.Rows, row)
	}
	m.agentbox.page.Sources = []client.InboxSource{{ProfileID: "one", EnvironmentID: "env-one", Label: "Host one", NativeWorkflows: true}, {ProfileID: "two", EnvironmentID: "env-two", Label: "Host two", NativeWorkflows: true}}
	m.filterInbox()
	row, _ := m.selectedInbox()
	m.status = client.Status{ProfileID: row.Target.ProfileID, EnvironmentID: row.Target.EnvironmentID, ThreadID: row.Target.ThreadID, Connected: true, Writable: true, RunState: "completed", State: "completed", Location: client.Location{ProfileID: row.Target.ProfileID, EnvironmentID: row.Target.EnvironmentID, ProjectID: row.Target.ProjectID, ThreadID: row.Target.ThreadID, Title: row.Thread.Title, Environment: row.Environment}}
	m.agentbox.decision = true
}

func TestAgentBoxModeRetainsExactEditorsPromptsAndReadingAnchors(t *testing.T) {
	for _, pending := range []bool{false, true} {
		m := testModel(t)
		m.composer.SetValue("exact draft\n界 é \n")
		m.composer.SelectAll()
		m.composer.SetCursorColumn(3)
		m.follow = false
		m.viewport.SetContent(strings.Repeat("line\n", 90))
		m.viewport.SetYOffset(12)
		m.transcriptFocus = true
		m.readingAnchor = readingAnchor{key: "message-a"}
		if pending {
			m.openPrompt(retiredPrompt(923, client.Question{Kind: client.EnterText, Title: "Exact question", Initial: "exact answer"}))
			m.modalInput.SelectAll()
		}
		draft, line, col, selected := m.currentDraft(), m.composer.Line(), m.composer.Column(), m.composer.SelectedText()
		prompt, answer, answerSelection := m.dialog, m.modalInput.Value(), m.modalInput.SelectedText()
		anchor, offset := m.readingAnchor, m.viewport.YOffset()
		m.setMode("agentbox")
		m.setMode("developer")
		if m.currentDraft() != draft || m.composer.Line() != line || m.composer.Column() != col || m.composer.SelectedText() != selected {
			t.Fatal("mode switch changed exact composer/caret/selection")
		}
		if m.dialog != prompt || m.modalInput.Value() != answer || m.modalInput.SelectedText() != answerSelection {
			t.Fatal("mode switch replaced a pending form")
		}
		if m.readingAnchor != anchor || m.viewport.YOffset() != offset || m.follow || !m.transcriptFocus {
			t.Fatal("mode switch changed reading return state")
		}
	}
}

func TestAgentBoxReplyEditorOwnsLettersNumbersNewlineAndSelection(t *testing.T) {
	m := testModel(t)
	seedAgentBox(m)
	m.agentbox.reply = true
	// Retain deliberately conflicting Developer pane flags across mode return.
	m.transcriptFocus, m.activityFocused, m.contextFocused = true, true, true
	m.restoreFocus()
	for _, r := range "jknrelsz123" {
		m.Update(keyPress(r))
	}
	m.Update(keyPress(tea.KeyEnter))
	if m.currentDraft() != "jknrelsz123\n" {
		t.Fatalf("inbox actions stole composer text: %q", m.currentDraft())
	}
	if m.actionBusy || m.dialog != nil || m.agentbox.undo != nil {
		t.Fatal("typing triggered an inbox action")
	}
	m.Update(keyPress(tea.KeyEscape))
	if m.agentbox.reply || !m.transcriptFocus || !m.activityFocused || !m.contextFocused {
		t.Fatal("return from reply lost the Developer focus state")
	}
}

func TestAgentBoxRefreshPreservesExactSelectionAndRejectsStaleCompletion(t *testing.T) {
	m := testModel(t)
	seedAgentBox(m)
	m.agentbox.serial = 7
	selected := m.agentbox.selected
	page := m.agentbox.page
	page.Rows = []client.InboxThread{page.Rows[1], page.Rows[0], page.Rows[2]}
	m.inboxLoaded(inboxMsg{serial: 7, page: page, prefs: m.agentbox.prefs})
	if m.agentbox.selected != selected {
		t.Fatal("refresh retargeted selected task")
	}
	stale := page
	stale.Rows = nil
	m.inboxLoaded(inboxMsg{serial: 6, page: stale, prefs: m.agentbox.prefs})
	if m.agentbox.selected != selected || len(m.agentbox.rows) == 0 {
		t.Fatal("old read replaced current inbox")
	}
}

func TestAgentBoxReplyKeepsOriginalIdentityWhenSourceDisappearsOnRefresh(t *testing.T) {
	m := testModel(t)
	seedAgentBox(m)
	m.agentbox.reply = true
	m.composer.SetValue("exact source-owned reply")
	original := m.agentbox.selected
	page := m.agentbox.page
	page.Rows = append([]client.InboxThread(nil), page.Rows[1:]...)
	m.agentbox.serial = 4
	m.inboxLoaded(inboxMsg{serial: 4, page: page, prefs: m.agentbox.prefs})
	if m.agentbox.selected != original || m.currentDraft() != "exact source-owned reply" || !m.agentbox.reply {
		t.Fatal("partial refresh retargeted current reply")
	}
	if _, ok := m.selectedInbox(); ok {
		t.Fatal("missing source was replaced by another displayed identity")
	}
}

func TestAgentBoxOffersDoNotAuthorizeFormalRequests(t *testing.T) {
	m := testModel(t)
	seedAgentBox(m)
	m.status.Requests = 1
	m.inboxOfferedChoice(1)
	if m.actionBusy {
		t.Fatal("number silently submitted a request without an exact form")
	}
	m.markInboxReviewed()
	if m.actionBusy {
		t.Fatal("review marker silently resolved a formal request")
	}
	m.agentbox.offer = &client.InboxTarget{ProfileID: "wrong", EnvironmentID: "wrong", ProjectID: "project", ThreadID: "alpha", Revision: "old"}
	if _, ok := m.inboxActionTarget(); ok {
		t.Fatal("stale local menu retargeted an inbox action")
	}
}

func TestAgentBoxRealViewSizesAndSyntheticCaptures(t *testing.T) {
	for _, size := range [][2]int{{60, 16}, {80, 24}, {120, 35}, {180, 45}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m := testModel(t)
			seedAgentBox(m)
			m.width, m.height = size[0], size[1]
			m.resize()
			m.restoreFocus()
			view := m.View().Content
			plain := ansi.Strip(view)
			if !strings.Contains(plain, "AgentBox") || !strings.Contains(plain, "Needs you") || !strings.Contains(plain, "1 Changes") {
				t.Fatalf("missing actionable inbox at %v:\n%s", size, plain)
			}
			lines := strings.Split(view, "\n")
			if len(lines) > size[1] {
				t.Fatalf("view exceeds height %d", len(lines))
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > size[0] {
					t.Fatalf("view exceeds width %d", ansi.StringWidth(line))
				}
			}
			if !strings.Contains(view, "\x1b[") {
				t.Fatal("real view omitted meaningful styles")
			}
			if dir := os.Getenv("T3OXIDE_CAPTURE_DIR"); dir != "" {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				base := filepath.Join(dir, fmt.Sprintf("synthetic-agentbox-%dx%d", size[0], size[1]))
				if err := os.WriteFile(base+".ansi", []byte(view), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(base+".txt", []byte(plain), 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestAgentBoxMissingHostsDoNotClaimInboxZero(t *testing.T) {
	m := testModel(t)
	m.mode = "agentbox"
	m.agentbox.loaded = true
	m.agentbox.page.Sources = []client.InboxSource{{ProfileID: "offline", Error: "connection refused"}}
	view := ansi.Strip(m.View().Content)
	if strings.Contains(view, "Nothing needs you") || !strings.Contains(view, "unavailable") || !strings.Contains(view, "offline") {
		t.Fatal("partial host failure became empty inbox success")
	}
}

func TestAgentBoxBatchInputCheckpointSurvivesModeAndRetiredPrompt(t *testing.T) {
	m := testModel(t)
	var saved string
	q := client.Question{Kind: client.EnterText, Title: "File multiple tasks", StoreText: func(value string) error { saved = value; return nil }}
	m.openPrompt(retiredPrompt(982, q))
	const text = "  task one\n界 second task  "
	m.modalInput.SetValue(text)
	m.captureTextDraft()
	m.setMode("agentbox")
	m.setMode("developer")
	m.dialog = nil // cancellation may retire a prompt before shutdown.
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if saved != text {
		t.Fatal("batch checkpoint was lost or normalized across mode/retirement")
	}
}
