package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Layout and mouse routing share this collection. Operational actions get the
// first available cells; less common controls remain in the command catalog.
func (m *Model) visibleToolbarActions(width int) []struct{ label, action string } {
	budget := width
	if position := m.workspacePosition(width); position != "" {
		budget -= ansi.StringWidth(position) + 2
	}
	actions := m.toolbarActions()
	used, count := 0, 0
	for _, action := range actions {
		gap := 0
		if count > 0 {
			gap = 2
		}
		if used+gap+ansi.StringWidth(action.label) > budget {
			break
		}
		used += gap + ansi.StringWidth(action.label)
		count++
	}
	return actions[:count]
}

func (m *Model) workspacePosition(width int) string {
	if m.snapshot != nil && m.snapshot.Metadata.Historical {
		if width < 70 {
			return "Live paused"
		}
		return "Earlier · live paused"
	}
	if !m.follow {
		if width < 60 {
			return fmt.Sprintf("Read %d%%", int(m.viewport.ScrollPercent()*100))
		}
		return fmt.Sprintf("Reading %d%% · End latest", int(m.viewport.ScrollPercent()*100))
	}
	if m.transcriptFocus {
		return "> Reading"
	}
	return ""
}

// Durable lanes never compete with a long transient receipt for the last
// visible cells. Full operation identities and causal receipt state are kept
// in Details and in the existing source-owned Saved operations review.
func (m *Model) operationView(width int) string {
	text := m.operationLine()
	if ansi.StringWidth(text) > width && m.durableProblem() {
		var parts []string
		if m.hasSavedActions() {
			parts = append(parts, "Saved action")
		}
		if m.status.PendingStop {
			parts = append(parts, "Saved Stop")
		}
		if m.status.DraftSaveFailed {
			parts = append(parts, "Draft not saved")
		}
		text = strings.Join(parts, " · ")
		if ansi.StringWidth(text) > width {
			text = strings.ReplaceAll(text, " · ", "; ")
			text = strings.ReplaceAll(text, "Draft not saved", "Draft unsaved")
		}
		if ansi.StringWidth(text) > width && m.status.PendingAction && m.status.PendingStop {
			text = "Saved: action + Stop"
			if m.status.DraftSaveFailed {
				text += " · Draft unsaved"
			}
		}
	}
	return colors(m.theme).operational().Render(fitLine(text, width))
}

func (m *Model) conversationDetailText() string {
	loc := m.status.Location
	parts := []string{m.attachedTargetStatus()}
	if m.session != nil {
		destination, policy := m.session.CreationDestinationHint()
		parts = append(parts, "New destination policy: "+policy, "New profile: "+reported(destination.ProfileID), "New environment ID: "+reported(destination.EnvironmentID), "New project ID: "+reported(destination.ProjectID))
	}
	if m.status.Context != "" {
		parts = append(parts, m.status.Context)
	}
	for _, field := range []struct{ name, value string }{
		{"Profile", m.status.ProfileID}, {"Environment ID", m.status.EnvironmentID},
		{"Conversation ID", m.status.ThreadID}, {"Project ID", loc.ProjectID},
		{"Workspace", loc.Workspace}, {"Saved model", m.status.ModelName},
		{"Next message", m.status.NextModel},
	} {
		if field.value != "" {
			parts = append(parts, field.name+": "+field.value)
		}
	}
	parts = append(parts, m.authoritativeStatus(), m.connectionStatus())
	if m.snapshot != nil && m.snapshot.Metadata.RuntimeDetails != nil {
		runtime := m.snapshot.Metadata.RuntimeDetails
		if runtime.RunModelSelection != nil {
			parts = append(parts, "Requested run model: "+runtime.RunModelSelection.Summary())
		}
		if runtime.ProviderReportedSelection != nil {
			parts = append(parts, "Provider reported model: "+runtime.ProviderReportedSelection.Summary())
		} else {
			parts = append(parts, "Provider reported model: unavailable")
		}
	}
	if m.snapshot != nil {
		parts = append(parts, providerIdentityLines(m.snapshot.Metadata.ProviderIdentity)...)
	}
	parts = append(parts, "Saved delivery: "+reported(m.status.Delivery), "Next-send delivery: "+m.deliveryLabel())
	if m.status.ThreadID != "" && !m.status.Writable {
		parts = append(parts, "Read only: conversation mutations are unavailable.")
	}
	if m.snapshot != nil && m.snapshot.Metadata.Historical {
		parts = append(parts, "Earlier history: live detail is paused. Latest resumes it; Find searches only this loaded page.")
	} else {
		parts = append(parts, "Find searches the loaded page. Earlier loads an older page; activity history is separate.")
	}
	if receipt := m.status.Receipt; receipt != nil {
		parts = append(parts, "Send receipt: "+receipt.Phase, "Operation ID: "+receipt.OperationID,
			"Message ID: "+receipt.MessageID, "Receipt conversation: "+receipt.ThreadID)
		if receipt.RunID != "" {
			parts = append(parts, "Correlated run: "+receipt.RunID)
		}
		parts = append(parts, "Acknowledged confirms receipt only. A response requires matching message and run evidence from T3.")
	}
	if m.hasSavedActions() {
		parts = append(parts, "May already be sent. Review the original target, payload and ID in Commands → Review saved action (all environments). Reconnect never retries them.")
	}
	return strings.Join(parts, "\n")
}

// A disappearing pane must not keep the keyboard. This changes local focus
// only; it never edits a draft, reattaches, or moves the reading anchor.
func (m *Model) reconcileWorkspaceFocus() bool {
	if m.browserPage || m.launchPage || m.mode == "agentbox" {
		return false
	}
	changed := false
	if m.navigator.focused && !m.navigationVisible() {
		m.navigator.focused = false
		m.transcriptFocus = true
		changed = true
	}
	if m.contextFocused && !m.contextVisible() {
		m.contextFocused = false
		m.transcriptFocus = true
		changed = true
	}
	if m.activityFocused && m.recentActivity() == nil && !(m.pendingWorkspaceReturn != nil && m.snapshot == nil) {
		m.activityFocused = false
		m.activityFocusKey = ""
		m.transcriptFocus = true
		changed = true
	}
	return changed
}
