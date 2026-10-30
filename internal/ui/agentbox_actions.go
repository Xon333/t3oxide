package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
)

func (m *Model) inboxActionTarget() (client.InboxThread, bool) {
	row, ok := m.selectedInbox()
	if !ok || m.inboxSourceLoading(row.Target.ProfileID) {
		m.notice = "Select a loaded task first"
		return row, false
	}
	if m.agentbox.offer != nil {
		target := *m.agentbox.offer
		m.agentbox.offer = nil
		if target.Key() != row.Target.Key() || target.Revision != row.Target.Revision {
			m.notice = "Inbox changed; review this task again"
			return row, false
		}
	}
	if !m.inboxAttached(row) {
		m.notice = "Open this exact task before acting"
		return row, false
	}
	return row, true
}

func (m *Model) inboxLaterMenu() tea.Cmd {
	row, ok := m.selectedInbox()
	if !ok {
		return nil
	}
	if !m.inboxAttached(row) {
		m.notice = "Open this task before choosing Later"
		return nil
	}
	if row.Thread.Inbox == nil {
		m.notice = "Native Later is unavailable on this host"
		return nil
	}
	target := row.Target
	m.agentbox.offer = &target
	items := []list.Item{}
	if row.Thread.Inbox.SnoozedUntil != "" {
		items = append(items, choiceItem{title: "Show now", command: "inbox", args: "unsnooze"})
	}
	for _, item := range []struct {
		label    string
		duration time.Duration
	}{{"In one hour", time.Hour}, {"In four hours", 4 * time.Hour}, {"In one day", 24 * time.Hour}} {
		until := time.Now().Add(item.duration).UTC().Format(time.RFC3339)
		items = append(items, choiceItem{title: item.label, description: until, command: "inbox", args: "until:" + until})
	}
	m.openLocal("Later · "+row.Thread.Title, "inbox-later", items)
	return nil
}

func (m *Model) markInboxReviewed() tea.Cmd {
	row, ok := m.selectedInbox()
	if !ok {
		return nil
	}
	if !m.inboxAttached(row) {
		m.notice = "Open this task before marking it reviewed"
		return nil
	}
	if row.Thread.Inbox == nil {
		m.notice = "Native review disposition is unavailable on this host"
		return nil
	}
	if m.status.Requests > 0 {
		m.notice = "Formal requests need their exact request form"
		return m.execute("requests", "")
	}
	if m.status.ActiveRunID != "" {
		m.notice = "Work is still running; review its changes or use Stop separately"
		return nil
	}
	target := row.Target
	m.agentbox.offer = &target
	items := []list.Item{choiceItem{title: "Inspect actual changes", command: "inbox", args: "diff"}}
	if row.Thread.Inbox.Settled {
		items = append(items, choiceItem{title: "Reopen in Needs you", command: "inbox", args: "unsettle"})
	} else {
		items = append(items, choiceItem{title: "Mark reviewed", description: "Settle this native thread; no commit, push or merge", command: "inbox", args: "settle"})
	}
	m.openLocal("Review · "+row.Thread.Title, "inbox-review", items)
	return nil
}

func (m *Model) mutateInbox(action, until string, undo bool) tea.Cmd {
	row, ok := m.inboxActionTarget()
	if !ok {
		return nil
	}
	if m.actionBusy {
		m.notice = "An action is pending; Stop remains available"
		return nil
	}
	var previous *inboxUndo
	if row.Thread.Inbox != nil {
		switch action {
		case "settle":
			previous = &inboxUndo{target: row.Target, action: "unsettle", label: "Reopen " + row.Thread.Title}
		case "unsettle":
			previous = &inboxUndo{target: row.Target, action: "settle", label: "Mark reviewed " + row.Thread.Title}
		case "snooze", "unsnooze":
			previous = &inboxUndo{target: row.Target, action: "unsnooze", label: "Show " + row.Thread.Title}
			if old := row.Thread.Inbox.SnoozedUntil; old != "" {
				if date, err := time.Parse(time.RFC3339Nano, old); err == nil && date.After(time.Now()) {
					previous.action, previous.until, previous.label = "snooze", old, "Restore Later for "+row.Thread.Title
				}
			}
		}
	}
	m.actionID++
	id, generation, session := m.actionID, m.generation, m.session
	ctx, cancel := context.WithCancel(m.sessionCtx)
	m.cancelAction = cancel
	done := make(chan struct{})
	m.actionDone = done
	m.actionBusy = true
	m.actionName = "inbox-" + action
	order := make([]string, 0, len(m.agentbox.rows))
	for _, candidate := range m.agentbox.rows {
		order = append(order, candidate.Target.Key())
	}
	return func() tea.Msg {
		defer close(done)
		defer cancel()
		receipt, err := session.AgentBoxAction(ctx, row.Target, action, until)
		return inboxMutationMsg{action: actionMsg{generation: generation, id: id, name: "inbox-" + action, err: err}, target: row.Target, receipt: receipt, previous: previous, undo: undo, order: order}
	}
}

func (m *Model) inboxMutationFinished(msg inboxMutationMsg) (tea.Model, tea.Cmd) {
	if msg.action.generation != m.generation || msg.action.id != m.actionID {
		return m, nil
	}
	if msg.action.err == nil {
		if (msg.receipt.Action == "settle" || msg.receipt.Action == "snooze") && !msg.undo {
			if row, ok := m.selectedInbox(); ok && row.Target.Key() == msg.target.Key() {
				label := "Reviewed"
				if msg.receipt.Action == "snooze" {
					label = "Later"
				}
				m.agentbox.ack = &inboxAcknowledgement{row: row, label: label, order: msg.order}
				m.agentbox.selected = row.Target.Key()
				m.agentbox.decision = true
			}
		}
		if msg.undo {
			m.agentbox.undo = nil
		} else if msg.previous != nil && msg.receipt.NativeRevision != "" {
			msg.previous.target.Revision = msg.receipt.NativeRevision
			m.agentbox.undo = msg.previous
		}
		// Update only the exact row that received this authentic acknowledgement.
		// The following metadata refresh owns all run/request/lifecycle truth.
		for i := range m.agentbox.page.Rows {
			row := &m.agentbox.page.Rows[i]
			if row.Target.Key() != msg.target.Key() || row.Thread.Inbox == nil {
				continue
			}
			row.Target.Revision = msg.receipt.NativeRevision
			row.Thread.Inbox.NativeRevision = msg.receipt.NativeRevision
			switch msg.receipt.Action {
			case "settle":
				row.Thread.Inbox.Settled = true
			case "unsettle":
				row.Thread.Inbox.Settled = false
			case "unsnooze":
				row.Thread.Inbox.SnoozedUntil = ""
			}
		}
		m.agentbox.reply = false
		m.filterInbox()
		msg.action.result.Text = "Native " + msg.receipt.Action + " acknowledged"
		if m.agentbox.ack != nil {
			msg.action.result.Text = m.agentbox.ack.label + " acknowledged · Next / Back to inbox"
		}
	}
	_, cmd := m.Update(msg.action)
	if msg.action.err == nil {
		return m, tea.Batch(cmd, m.loadInbox(true))
	}
	return m, cmd
}

func (m *Model) loadInboxReview() tea.Cmd {
	row, ok := m.inboxActionTarget()
	if !ok {
		return nil
	}
	return m.loadAttachedReview(row.Target, row.Thread.Title)
}

func (m *Model) loadAttachedReview(target client.InboxTarget, title string) tea.Cmd {
	if m.session == nil || target.ThreadID == "" {
		m.notice = "Open a conversation before reviewing its changes"
		return nil
	}
	if m.agentbox.reviewLoading {
		m.notice = "Review is loading"
		return nil
	}
	m.agentbox.reviewSerial++
	serial, generation, session := m.agentbox.reviewSerial, m.generation, m.session
	m.agentbox.reviewLoading = true
	ctx := m.sessionCtx
	return func() tea.Msg {
		review, err := session.ThreadReview(ctx, target)
		return inboxReviewMsg{generation: generation, serial: serial, target: target, review: review, err: err, title: title}
	}
}

func (m *Model) inboxReviewFinished(msg inboxReviewMsg) tea.Cmd {
	if msg.generation != m.generation || msg.serial != m.agentbox.reviewSerial {
		return nil
	}
	m.agentbox.reviewLoading = false
	if msg.err != nil {
		m.problem = safeText(msg.err.Error())
		return nil
	}
	if m.status.EnvironmentID != msg.target.EnvironmentID || m.status.ProfileID != msg.target.ProfileID || m.status.ThreadID != msg.target.ThreadID || m.status.Location.ProjectID != msg.target.ProjectID {
		return nil
	}
	m.agentbox.review = &msg.review
	m.agentbox.reviewTarget = msg.target
	lines := []string{msg.title, "Environment: " + m.status.Location.Environment, "Project: " + msg.review.ProjectID, "Thread: " + msg.review.ThreadID, "Revision: " + msg.review.Revision, "Workspace: " + msg.review.Workspace}
	if msg.review.Branch != "" {
		lines = append(lines, "Branch: "+msg.review.Branch)
	}
	for _, pr := range msg.review.PullRequests {
		lines = append(lines, fmt.Sprintf("PR #%d · %s · %s", pr.Number, pr.State, pr.URL))
		sync := pr.SyncedAt
		if sync == "" {
			sync = "unreported"
		}
		lines = append(lines, "PR synced: "+sync)
	}
	if len(msg.review.PullRequests) == 0 {
		lines = append(lines, "PR sync: unreported")
	}
	observed := msg.review.GeneratedAt
	if observed == "" {
		observed = "unreported"
	}
	lines = append(lines, "Diff observed: "+observed, fmt.Sprintf("Sources: %d · files: %d · clipped: %t", len(msg.review.Sources), len(msg.review.Files), msg.review.DiffTruncated))
	lines = append(lines, "Diff/run completion does not establish tests, merge, or human review.")
	if msg.review.Unavailable != "" {
		lines = append(lines, "\n"+msg.review.Unavailable)
	}
	for _, source := range msg.review.Sources {
		lines = append(lines, source.Title+" · "+source.Kind+" · "+source.Hash+fmt.Sprintf(" · clipped: %t", source.Truncated))
	}
	for _, file := range msg.review.Files {
		lines = append(lines, fmt.Sprintf("%s · +%d −%d", file.Path, file.Additions, file.Deletions))
	}
	if msg.review.Diff != "" {
		lines = append(lines, "\n"+msg.review.Diff)
	} else if msg.review.Unavailable == "" {
		lines = append(lines, "\nNative review reports no changes.")
	}
	if msg.review.DiffTruncated {
		lines = append(lines, "\nDiff is bounded; inspect the remaining changes in full T3.")
	}
	m.openDetail("Changes", safeText(strings.Join(lines, "\n")))
	if m.session != nil {
		offset, notice := m.session.ReviewPosition(msg.target, msg.review)
		m.detailViewport.SetYOffset(offset)
		if notice != "" {
			m.notice = notice
		}
	}
	return nil
}

func (m *Model) inboxPriorityMenu() tea.Cmd {
	row, ok := m.selectedInbox()
	if !ok {
		return nil
	}
	if row.Target.ProjectID == "" {
		m.notice = "This native thread has no reported project identity"
		return nil
	}
	target := row.Target
	m.agentbox.offer = &target
	items := []list.Item{}
	for i, label := range []string{"Low", "Medium", "High", "Urgent"} {
		items = append(items, choiceItem{title: label, command: "inbox", args: fmt.Sprintf("priority:%d", i)})
	}
	items = append(items, choiceItem{title: "Project · First", command: "inbox", args: "project-rank:1"}, choiceItem{title: "Project · Second", command: "inbox", args: "project-rank:2"}, choiceItem{title: "Project · Third", command: "inbox", args: "project-rank:3"}, choiceItem{title: "Project · Unranked", command: "inbox", args: "project-rank:0"})
	m.openLocal("Presentation priority", "inbox-priority", items)
	return nil
}

func (m *Model) setInboxRank(value int, project bool) tea.Cmd {
	row, ok := m.selectedInbox()
	if !ok {
		return nil
	}
	if m.agentbox.offer != nil {
		target := *m.agentbox.offer
		m.agentbox.offer = nil
		if target.Key() != row.Target.Key() {
			m.notice = "Inbox changed; choose priority again"
			return nil
		}
	}
	if m.agentbox.rankBusy {
		return nil
	}
	if value < 0 || value > 3 {
		return nil
	}
	previous := m.agentbox.prefs.Clone()
	next := previous.Clone()
	if project {
		key := config.InboxIdentity(row.Target.EnvironmentID, row.Target.ProjectID, "")
		if value == 0 {
			delete(next.Projects, key)
		} else {
			next.Projects[key] = value
		}
	} else {
		next.Tasks[row.Target.Key()] = value
	}
	m.agentbox.rankBusy = true
	session := m.session
	return func() tea.Msg {
		saved, err := session.SaveInboxPreferences(previous.Revision, next)
		return inboxRankMsg{previous: previous, next: saved, err: err}
	}
}

func (m *Model) inboxRankFinished(msg inboxRankMsg) tea.Cmd {
	m.agentbox.rankBusy = false
	if msg.err != nil {
		m.problem = safeText(msg.err.Error())
		return nil
	}
	m.agentbox.prefs = msg.next
	if msg.undo {
		m.agentbox.undo = nil
	} else {
		previous := msg.previous.Clone()
		m.agentbox.undo = &inboxUndo{preferences: &previous, expected: msg.next.Revision, label: "Restore presentation priority"}
	}
	m.filterInbox()
	m.notice = "Presentation priority saved"
	return nil
}

func (m *Model) undoInbox() tea.Cmd {
	u := m.agentbox.undo
	if u == nil {
		m.notice = "No reversible inbox action"
		return nil
	}
	if u.preferences != nil {
		if m.agentbox.rankBusy {
			return nil
		}
		m.agentbox.rankBusy = true
		session := m.session
		previous := m.agentbox.prefs.Clone()
		next := u.preferences.Clone()
		expected := u.expected
		return func() tea.Msg {
			saved, err := session.SaveInboxPreferences(expected, next)
			return inboxRankMsg{previous: previous, next: saved, err: err, undo: true}
		}
	}
	// Native undo is an inverse with the exact post-action revision. It cannot
	// override a newer run, request, another client, or a changed disposition.
	for _, row := range m.agentbox.page.Rows {
		if row.Target.Key() != u.target.Key() {
			continue
		}
		if row.Target.Revision != u.target.Revision {
			m.notice = "Task changed since that action; Undo is unavailable"
			return nil
		}
		if !m.inboxAttached(row) {
			m.notice = "Open the original task before Undo"
			return nil
		}
		m.agentbox.view = "all"
		m.filterInbox()
		m.agentbox.selected = row.Target.Key()
		target := u.target
		m.agentbox.offer = &target
		return m.mutateInbox(u.action, u.until, true)
	}
	m.notice = "Refresh the original task before Undo"
	return nil
}

// Numerals select only these visible client actions. A formal request choice
// is never fabricated from a model's prose and is never auto-approved here.
func (m *Model) inboxOfferedChoice(index int) tea.Cmd {
	row, ok := m.selectedInbox()
	if !ok || !m.inboxAttached(row) {
		return nil
	}
	if m.agentbox.ack != nil && m.agentbox.ack.row.Target.Key() == row.Target.Key() && m.status.Requests == 0 {
		if index == 1 {
			return m.nextAcknowledgedInbox()
		}
		if index == 2 {
			return m.agentboxAction("inbox", "back")
		}
		return nil
	}
	if m.status.Requests > 0 {
		switch index {
		case 1:
			return m.execute("requests", "")
		case 2:
			return m.setMode("developer")
		case 3:
			return m.inboxLaterMenu()
		}
	} else {
		switch index {
		case 1:
			return m.loadInboxReview()
		case 2:
			return m.openInboxThread(true)
		case 3:
			return m.markInboxReviewed()
		}
	}
	return nil
}

func (m *Model) inboxInfo() tea.Cmd {
	lines := []string{"AgentBox · native T3 threads", "Priority changes only this client's presentation order. Native T3 owns execution and queueing.", "The loaded page is bounded. Future queued launch scheduling is not implemented by this client.", "Existing native work continues when this client exits."}
	if m.agentbox.query.Value() != "" {
		lines = append(lines, "Current query: "+m.agentbox.query.Value(), "Last native read query: "+m.agentbox.readQuery, "Sources: "+m.inboxSourceHealth())
	}
	for _, source := range m.agentbox.page.Sources {
		lines = append(lines, fmt.Sprintf("\n%s · profile=%s environment=%s · nativeWorkflows=%t · cursor=%d more=%t", source.Label, source.ProfileID, source.EnvironmentID, source.NativeWorkflows, source.Page.Cursor, source.Page.More))
		if source.Error != "" {
			lines = append(lines, source.Error)
		}
		if source.Page.ObservedAt != "" {
			lines = append(lines, "Observed: "+source.Page.ObservedAt)
		}
		if source.Page.SearchScope != "" {
			lines = append(lines, fmt.Sprintf("Search scope: %s · incomplete: %t", source.Page.SearchScope, source.Page.SearchIncomplete))
		}
	}
	if row, ok := m.selectedInbox(); ok {
		lines = append(lines, "\nConversation: "+row.Thread.Title, "Thread: "+row.Target.ThreadID, "Project: "+row.Thread.Project, "Project ID: "+row.Target.ProjectID, "Revision: "+row.Target.Revision, "Status: "+row.Thread.Status)
		lines = append(lines, "Profile: "+row.Target.ProfileID, "Environment: "+row.Environment, "Environment ID: "+row.Target.EnvironmentID, "Machine kind: "+m.inboxMachineKind(row.Target.ProfileID))
		identity := row.Thread.ProviderIdentity
		if identity == nil && row.Thread.ProviderInstanceID != "" {
			identity = &wire.ProviderIdentity{InstanceID: row.Thread.ProviderInstanceID}
		}
		lines = append(lines, providerIdentityLines(identity)...)
		if row.Thread.Inbox != nil {
			v := row.Thread.Inbox
			queued := "unreported"
			if v.QueuedCount >= 0 {
				queued = fmt.Sprint(v.QueuedCount)
			}
			request := "none reported"
			if v.PendingRequestCount > 0 {
				request = "pending summary · " + v.PendingRequestID + " · " + v.PendingRequestType
			}
			lines = append(lines, "Run: "+v.RunID, "Workspace: "+v.Workspace, "Branch: "+v.Branch, fmt.Sprintf("Queued: %s · settled: %t", queued, v.Settled), "Formal request: "+request, "Later: "+v.SnoozedUntil)
		}
	}
	m.openDetail("Info", safeText(strings.Join(lines, "\n")))
	return nil
}
