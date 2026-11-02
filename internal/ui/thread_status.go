package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/Xon333/t3oxide/internal/client"
)

type statusBadge struct{ label, role string }

// Run state and formal requests are distinct from InboxBucket's disposition:
// settled/Reviewed and snoozed metadata never imply a completed run.
func threadStatusBadge(row client.Thread) statusBadge {
	status := strings.ToLower(strings.TrimSpace(row.Status))
	switch status {
	case "failed", "error":
		return statusBadge{"! Failed", "error"}
	case "blocked":
		return statusBadge{"# Blocked", "warning"}
	}
	if row.Inbox != nil && row.Inbox.PendingRequestCount > 0 {
		return statusBadge{"? Waiting", "warning"}
	}
	switch status {
	case "preparing", "starting", "running", "active", "responding":
		return statusBadge{"> Active", "operation"}
	case "waiting", "waiting_for_input", "awaiting_input", "waiting_for_approval":
		return statusBadge{"? Waiting", "warning"}
	case "queued", "pending":
		return statusBadge{"= Queued", "operation"}
	}
	if row.Inbox != nil && row.Inbox.QueuedCount > 0 {
		return statusBadge{"= Queued", "operation"}
	}
	switch status {
	case "completed":
		return statusBadge{"+ Completed", "completed"}
	case "interrupted", "stopped", "cancelled", "canceled":
		return statusBadge{"x Stopped", "warning"}
	}
	if status == "idle" {
		return statusBadge{"- Idle", "plain"}
	}
	return statusBadge{"? Unknown", "plain"}
}

// Compact keeps the full textual status available in Info and decision details.
func (b statusBadge) compact() statusBadge {
	b.label = strings.Fields(b.label)[0]
	return b
}

// Critical status colors are fixed, independently of theme and row selection.
// A dark symbol cell preserves their contrast in light terminals too.
const statusBackground = "#111318"

func statusColor(role string) string {
	switch role {
	case "operation":
		return "#5DE8FF"
	case "completed":
		return "#5CFF85"
	case "warning":
		return "#FFE45E"
	case "error":
		return "#FF7575"
	}
	return ""
}

func (b statusBadge) style(p palette, base lipgloss.Style) lipgloss.Style {
	if foreground := statusColor(b.role); foreground != "" {
		return base.Foreground(lipgloss.Color(foreground)).Background(lipgloss.Color(statusBackground)).Reverse(false).Bold(true)
	}
	return base.Foreground(lipgloss.Color(p.fg)).Bold(true)
}

func renderStatusRow(line, identity string, badge statusBadge, p palette, base, marker lipgloss.Style) string {
	body := strings.TrimSuffix(line, badge.label)
	rendered := base.Render(body)
	if before, after, found := strings.Cut(body, identity); found {
		rendered = base.Render(before) + marker.Render(identity) + base.Render(after)
	}
	if body != line {
		rendered += badge.style(p, base).Render(badge.label)
	}
	return rendered
}
