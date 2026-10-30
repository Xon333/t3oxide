package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
)

type inboxAcknowledgement struct {
	row   client.InboxThread
	label string
	order []string
}

func freezeInboxRows(old, fresh []client.InboxThread) []client.InboxThread {
	byID := make(map[string]client.InboxThread, len(fresh))
	for _, row := range fresh {
		byID[row.Target.Key()] = row
	}
	out := make([]client.InboxThread, 0, len(fresh))
	for _, row := range old {
		if current, ok := byID[row.Target.Key()]; ok {
			out = append(out, current)
			delete(byID, row.Target.Key())
		}
	}
	for _, row := range fresh {
		if _, ok := byID[row.Target.Key()]; ok {
			out = append(out, row)
			delete(byID, row.Target.Key())
		}
	}
	return out
}

func (m *Model) nextAcknowledgedInbox() tea.Cmd {
	a := &m.agentbox
	if a.ack == nil || m.status.Requests > 0 || m.actionBusy {
		return nil
	}
	order := append([]string(nil), a.ack.order...)
	seen := map[string]bool{}
	for _, id := range order {
		seen[id] = true
	}
	for _, row := range a.rows {
		if !seen[row.Target.Key()] {
			order = append(order, row.Target.Key())
		}
	}
	after := false
	for _, id := range order {
		if id == a.ack.row.Target.Key() {
			after = true
			continue
		}
		if !after {
			continue
		}
		for _, row := range a.rows {
			if row.Target.Key() != id || m.inboxSourceLoading(row.Target.ProfileID) {
				continue
			}
			if a.view != "all" && client.InboxBucket(row, time.Now()) != a.view {
				continue
			}
			a.ack = nil
			a.selected = id
			return m.openInboxThread(false)
		}
	}
	m.notice = "No next item on this loaded page · Back to inbox to change view or load more"
	return nil
}
