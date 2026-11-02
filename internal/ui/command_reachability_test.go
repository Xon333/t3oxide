package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/wire"
	"testing"
)

func TestAcceptedNavigationCommandsReachable(t *testing.T) {
	m := testModel(t)
	for _, name := range []string{"recent-conversations", "previous-conversation", "loaded-outline", "browse-scope", "batch-receipts"} {
		if _, ok := m.commandSpec(name); !ok || !m.isSlashCommand("/"+name) {
			t.Fatalf("unreachable command %s", name)
		}
	}
}

func TestAcceptedRecentIdentityAndBound(t *testing.T) {
	m := testModel(t)
	seedAgentBox(m)
	m.rememberRecentConversation()
	if len(m.recentConversations) != 1 {
		t.Fatal("attached conversation was not retained")
	}
	first := m.recentConversations[0]
	m.rememberRecentConversation()
	if len(m.recentConversations) != 1 {
		t.Fatal("duplicate recent")
	}
	m.status.EnvironmentID = "different-environment"
	m.rememberRecentConversation()
	if len(m.recentConversations) != 1 || m.recentConversations[0].page.EnvironmentID != first.page.EnvironmentID {
		t.Fatal("unoffered source fabricated a recent")
	}
}

func TestAcceptedDecisionUsesSelectedSource(t *testing.T) {
	m := testModel(t)
	seedAgentBox(m)
	m.status.ThreadID = "other"
	m.status.Requests = 2
	m.status.ActiveRunID = "other-run"
	labels := m.inboxChoiceLabels()
	if len(labels) != 3 || labels[2] != "Reviewed" {
		t.Fatalf("borrowed another thread's authority: %#v", labels)
	}
}

func TestAcceptedReviewRejectsMalformedAction(t *testing.T) {
	m := testModel(t)
	seedAgentBox(m)
	row, _ := m.selectedInbox()
	m.agentbox.reviewTarget = row.Target
	idx := buildReviewIndex(wire.ThreadReview{Diff: "diff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -1 +1 @@\n-old\n+new\n"})
	m.reviewIndex = &idx
	m.composer.SetValue("human note")
	m.reviewNavigate(idx.Digest + ":0:0:unknown")
	if m.currentDraft() != "human note" {
		t.Fatal("malformed action staged text")
	}
}

func TestAcceptedExactRequestControlStagesWithoutSubmit(t *testing.T) {
	m := testModel(t)
	p, reply, _ := intuitivePrompt(t, client.Question{Kind: client.EnterText, RequestQuestions: []client.RequestQuestion{{ID: "answer", InitialAnswer: "a"}}})
	m.openPrompt(p)
	m.restoreFocus()
	if !m.retainRequestExactInput("\t\r\n") {
		t.Fatal("attempt not retained")
	}
	surface := m.requestFormLayout(60, 16)
	found := false
	for _, hit := range surface.hits {
		if hit.args == "use-exact" {
			found = true
			hit.serial = m.dialog.serial
			m.activateModalHit(hit)
		}
	}
	if !found || m.dialog.preservedInitial == "" {
		t.Fatal("missing actionable exact answer")
	}
	select {
	case <-reply:
		t.Fatal("local recovery submitted")
	default:
	}
}

func TestAcceptedPasteCaretEndsOnJoinedGrapheme(t *testing.T) {
	e := newTextarea("")
	e.Focus()
	e.SetValue("👩")
	e.CursorStart()
	updateGraphemeEditor(&e, tea.PasteMsg{Content: "👨‍"})
	if e.Value() != "👨‍👩" || e.Column() != len([]rune(e.Value())) {
		t.Fatalf("partial joined cluster after paste: %q column %d", e.Value(), e.Column())
	}
	updateGraphemeEditor(&e, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if e.Value() != "" {
		t.Fatal("paste split by deletion")
	}
}
