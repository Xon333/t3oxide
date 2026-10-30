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
		return statusBadge{"+ Completed", "operation"}
	case "interrupted", "stopped", "cancelled", "canceled":
		return statusBadge{"x Stopped", "warning"}
	}
	if status == "idle" {
		return statusBadge{"- Idle", "plain"}
	}
	return statusBadge{"? Unknown", "plain"}
}

// Start from the row style so selection/focus stays visible behind the badge.
// Terminal uses the existing ANSI roles, never fixed RGB colors.
func (b statusBadge) style(p palette, base lipgloss.Style) lipgloss.Style {
	color := p.fg
	switch b.role {
	case "operation":
		color = p.operation
	case "warning":
		color = p.warning
	case "error":
		color = p.error
	}
	return base.Foreground(lipgloss.Color(color)).Bold(true)
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
