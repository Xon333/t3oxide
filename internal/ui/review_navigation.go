package ui

import (
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"fmt"
	"strconv"
	"strings"
)

func (m *Model) reviewFiles() tea.Cmd {
	r := m.reviewIndex
	if r == nil {
		return nil
	}
	items := []list.Item{choiceItem{title: "Raw diff · source metadata", command: "review-nav", args: r.Digest + ":raw"}}
	for i, f := range r.Files {
		items = append(items, choiceItem{title: f.Path, description: fmt.Sprintf("%d hunks · %s", len(f.Hunks), f.Fallback), command: "review-nav", args: fmt.Sprintf("%s:%d", r.Digest, i)})
	}
	m.openLocal("Changes · files", "review-files", items)
	return nil
}
func (m *Model) reviewNavigate(args string) tea.Cmd {
	parts := strings.Split(args, ":")
	r := m.reviewIndex
	if r == nil || len(parts) < 2 || parts[0] != r.Digest || m.agentbox.reviewTarget.EnvironmentID != m.status.EnvironmentID || m.agentbox.reviewTarget.ThreadID != m.status.ThreadID || m.agentbox.reviewTarget.ProfileID != m.status.ProfileID || m.status.Location.ProjectID != "" && m.agentbox.reviewTarget.ProjectID != m.status.Location.ProjectID {
		m.notice = "Changes moved; reopen Changes. Existing reply text is retained"
		return nil
	}
	if parts[1] == "raw" && len(parts) == 2 {
		m.dialog = nil
		m.openDetail("Changes", m.reviewRaw)
		if m.session != nil && m.agentbox.review != nil {
			offset, notice := m.session.ReviewPosition(m.agentbox.reviewTarget, *m.agentbox.review)
			m.detailViewport.SetYOffset(offset)
			if notice != "" {
				m.notice = notice
			}
		}
		return nil
	}
	f, err := strconv.Atoi(parts[1])
	if err != nil || f < 0 || f >= len(r.Files) {
		return nil
	}
	file := r.Files[f]
	if len(parts) == 2 {
		items := []list.Item{choiceItem{title: "Inspect file", description: file.Fallback, command: "review-nav", args: args + ":raw"}, choiceItem{title: "Stage file note in reply", command: "review-nav", args: args + ":note"}}
		for i, h := range file.Hunks {
			items = append(items, choiceItem{title: h.Header, command: "review-nav", args: fmt.Sprintf("%s:%d", args, i)})
		}
		m.openLocal(file.Path, "review-hunks", items)
		return nil
	}
	if parts[2] == "raw" && len(parts) == 3 {
		m.openDetail(file.Path, safeText(file.Raw))
		return nil
	}
	if parts[2] == "note" && len(parts) != 3 {
		return nil
	}
	anchor := file.Path
	if parts[2] != "note" {
		h, e := strconv.Atoi(parts[2])
		if e != nil || h < 0 || h >= len(file.Hunks) {
			return nil
		}
		if len(parts) == 3 {
			m.openLocal(file.Path+" · "+file.Hunks[h].Header, "review-hunk", []list.Item{choiceItem{title: "Inspect hunk", description: file.Hunks[h].Text, command: "review-nav", args: args + ":raw"}, choiceItem{title: "Stage hunk note in reply", command: "review-nav", args: args + ":note"}})
			return nil
		}
		if len(parts) != 4 || (parts[3] != "raw" && parts[3] != "note") {
			return nil
		}
		if parts[3] == "raw" {
			m.openDetail(file.Path, safeText(file.Hunks[h].Text))
			return nil
		}
		anchor += " " + file.Hunks[h].Header
	}
	// The anchor is a human-readable historical citation, never an actionable
	// native target. Keeping it in the durable ordinary draft preserves edits.
	draft := m.currentDraft()
	if draft != "" {
		draft += "\n\n"
	}
	draft += "Review note · " + anchor + "\nSource " + r.Digest[:12] + " (recheck if changes move):\n"
	if len(draft) > maxInputBytes {
		m.problem = "Reply limit reached; existing draft retained"
		return nil
	}
	if err := m.storeDraft(draft); err != nil {
		m.problem = err.Error()
		return nil
	}
	m.dialog = nil
	m.detail = ""
	m.agentbox.reply = m.mode == "agentbox"
	m.transcriptFocus = false
	m.notice = "Note staged in editable reply · Send remains explicit"
	m.resize()
	return m.restoreFocus()
}
