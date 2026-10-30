package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/wire"
	"github.com/charmbracelet/x/ansi"
)

func durableInboxModel(t *testing.T) (*Model, client.InboxThread) {
	t.Helper()
	m, _ := requestAcceptanceModel(t)
	m.mode = "agentbox"
	m.status = client.Status{ProfileID: "profile", EnvironmentID: "host", ThreadID: "thread", Connected: true, Writable: true, State: "completed", RunState: "completed", Location: client.Location{ProfileID: "profile", EnvironmentID: "host", ProjectID: "project", ThreadID: "thread", Title: "Reviewed source task"}}
	row := client.InboxThread{Target: client.InboxTarget{ProfileID: "profile", EnvironmentID: "host", ProjectID: "project", ThreadID: "thread", Revision: strings.Repeat("a", 64)}, Environment: "Fixture", Thread: client.Thread{ID: "thread", ProjectID: "project", Project: "Fixture project", Title: "Reviewed source task", Status: "completed", Inbox: &wire.InboxMetadata{NativeRevision: strings.Repeat("a", 64), QueuedCount: 0}}}
	next := row
	next.Target.ThreadID, next.Thread.ID, next.Thread.Title = "next-thread", "next-thread", "Next exact task"
	native := *row.Thread.Inbox
	next.Thread.Inbox = &native
	m.agentbox.page = client.InboxPage{Rows: []client.InboxThread{row, next}, Sources: []client.InboxSource{{ProfileID: "profile", EnvironmentID: "host", NativeWorkflows: true}}}
	m.agentbox.loaded, m.agentbox.loading, m.agentbox.view = true, false, "needs"
	m.agentbox.pendingSources = nil
	m.agentbox.selected, m.agentbox.decision = row.Target.Key(), true
	m.filterInbox()
	m.restoreDraft("exact source draft\n  preserve this  ")
	m.composer.CursorStart()
	m.composer.SetCursorColumn(4)
	m.follow = false
	m.viewport.SetContent(strings.Repeat("retained reading row\n", 80))
	m.viewport.SetYOffset(15)
	m.readingAnchor = readingAnchor{key: "original-message", offset: 2}
	return m, row
}

func syntheticInboxAck(m *Model, row client.InboxThread, action string, err error) {
	m.actionID++
	m.actionBusy = true
	m.actionName = "inbox-" + action
	order := make([]string, 0, len(m.agentbox.rows))
	for _, item := range m.agentbox.rows {
		order = append(order, item.Target.Key())
	}
	m.Update(inboxMutationMsg{action: actionMsg{generation: m.generation, id: m.actionID, name: m.actionName, err: err}, target: row.Target, receipt: client.InboxActionReceipt{ThreadID: row.Target.ThreadID, Action: action, NativeRevision: strings.Repeat("b", 64), Sequence: 12}, order: order})
}

func TestAcceptanceReviewedAndLaterAcknowledgementStayOnExactAttachment(t *testing.T) {
	for _, action := range []string{"settle", "snooze"} {
		for _, size := range flowSizes {
			t.Run(fmt.Sprintf("%s/%dx%d", action, size[0], size[1]), func(t *testing.T) {
				m, row := durableInboxModel(t)
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				session, generation := m.session, m.generation
				draft, line, column := m.currentDraft(), m.composer.Line(), m.composer.Column()
				anchor, offset := m.readingAnchor, m.viewport.YOffset()
				syntheticInboxAck(m, row, action, nil)
				if m.session != session || m.generation != generation || m.agentbox.selected != row.Target.Key() || m.agentbox.ack == nil || !m.agentbox.decision {
					t.Fatal("successful native disposition advanced/hid the attached source")
				}
				if m.currentDraft() != draft || m.composer.Line() != line || m.composer.Column() != column || m.follow || m.readingAnchor != anchor || m.viewport.YOffset() != offset {
					t.Fatal("acknowledgement damaged source draft/caret/reading state")
				}
				view := ansi.Strip(m.View().Content)
				for _, want := range []string{m.agentbox.ack.label, "Next", "Back"} {
					if !strings.Contains(view, want) {
						t.Fatalf("acknowledgement lacks explicit %q at %v:\n%s", want, size, view)
					}
				}
				m.agentbox.loading, m.agentbox.pendingSources = false, nil
				m.status.Requests = 1
				if cmd := m.agentboxAction("inbox", "next-acknowledged"); cmd != nil || m.agentbox.ack == nil || m.agentbox.selected != row.Target.Key() {
					t.Fatal("new request allowed acknowledgement Next to hide the source")
				}
				m.status.Requests = 0
				m.setMode("developer")
				m.setMode("agentbox")
				if m.session != session || m.currentDraft() != draft || m.agentbox.selected != row.Target.Key() || m.agentbox.ack == nil {
					t.Fatal("mode switch lost acknowledged exact source")
				}
			})
		}
	}
}

func TestAcceptanceDispositionFailureAndStaleReceiptNeverAdvance(t *testing.T) {
	for _, failure := range []string{"rejected", "uncertain"} {
		t.Run(failure, func(t *testing.T) {
			m, row := durableInboxModel(t)
			session, draft := m.session, m.currentDraft()
			syntheticInboxAck(m, row, "settle", errors.New(failure+": inspect original operation"))
			if m.session != session || m.agentbox.selected != row.Target.Key() || m.agentbox.ack != nil || !m.agentbox.decision || m.currentDraft() != draft || m.problem == "" {
				t.Fatal("failure/uncertainty became success or changed selected source")
			}
			m.Update(inboxMutationMsg{action: actionMsg{generation: m.generation, id: m.actionID - 1, name: "inbox-settle"}, target: row.Target, receipt: client.InboxActionReceipt{ThreadID: row.Target.ThreadID, Action: "settle"}})
			if m.agentbox.ack != nil || row.Thread.Inbox.Settled {
				t.Fatal("late old result acknowledged another operation")
			}
		})
	}
}

func TestAcceptanceAcknowledgementNextUsesCurrentFilteredSourceIdentities(t *testing.T) {
	m, row := durableInboxModel(t)
	other := m.agentbox.page.Rows[1]
	other.Target.EnvironmentID, other.Target.ProfileID, other.Environment = "other-host", "other-profile", "Other host"
	other.Target.ThreadID, other.Thread.ID = row.Target.ThreadID, row.Target.ThreadID // same local thread ID, different native source
	last := m.agentbox.page.Rows[1]
	last.Target.ThreadID, last.Thread.ID = "last-thread", "last-thread"
	m.agentbox.page.Rows = []client.InboxThread{row, other, last}
	m.agentbox.reorderNow = false
	m.agentbox.rows = append([]client.InboxThread(nil), m.agentbox.page.Rows...)
	syntheticInboxAck(m, row, "settle", nil)
	m.agentbox.loading = false
	m.agentbox.pendingSources = map[string]bool{"other-profile": true}
	// Background order changes cannot turn a transient row number into a new
	// target. Pending sources are skipped; no hidden page is fetched by Next.
	m.agentbox.rows = []client.InboxThread{last, row, other}
	if cmd := m.agentboxAction("inbox", "next-acknowledged"); cmd == nil || m.agentbox.selected != last.Target.Key() || m.status.ThreadID != row.Target.ThreadID || m.agentbox.ack != nil {
		t.Fatal("Next ignored frozen source identities, pending source or explicit admission")
	}
	if m.actionName != "inbox-open" {
		t.Fatal("Next did not use the ordinary exact-source open path")
	}
}

func TestAcceptanceBatchErrorReceiptIsVisibleAndSuccessfulHandoffRetainsReceipt(t *testing.T) {
	const receipt = "Batch source: source-profile / original-thread\nA · Acknowledged · original-A\nB · Needs recovery · original-B\nC · Remaining draft · no operation\nOriginal human input:\n  A\n\tB\n界 C\n"
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprintf("failed=%t", failed), func(t *testing.T) {
			m, _ := durableInboxModel(t)
			session, draft := m.session, m.currentDraft()
			m.actionID, m.actionBusy, m.actionName = 31, true, "file-many"
			result := client.Result{DetailTitle: "Batch receipt", Detail: receipt}
			var err error
			if failed {
				err = errors.New("B may already be sent; reconcile original-B")
			} else {
				result.Next = session // lazy successful handoff; no event reader run
			}
			m.Update(actionMsg{generation: m.generation, id: 31, name: "file-many", result: result, err: err})
			if m.detailTitle != "Batch receipt" || !strings.Contains(m.detail, "Acknowledged") || !strings.Contains(m.detail, "Needs recovery") || !strings.Contains(m.detail, "Remaining draft") || strings.Contains(m.detail, "Queued") {
				t.Fatal("partial receipt was hidden, lost on handoff, or recast as native queue state")
			}
			if failed && (m.session != session || m.currentDraft() != draft || !strings.Contains(m.problem, "original-B")) {
				t.Fatal("failed batch hid its exact error or changed the source draft")
			}
			if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Batch receipt") || !strings.Contains(view, "Remaining draft") {
				t.Fatal("actual composed View did not expose the durable receipt", view)
			}
		})
	}
}
