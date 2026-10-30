package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/client"
)

func (m *Model) inboxServerSearchOffered() bool {
	a := &m.agentbox
	query := strings.TrimSpace(a.query.Value())
	return !a.reply && !a.loading && len(a.rows) == 0 && len(query) >= 2 && len(query) <= 200
}

func (m *Model) inboxEmptySearchSurface(width int) renderedSurface {
	if !m.inboxServerSearchOffered() {
		return renderedSurface{}
	}
	surface := controlSurface(width, []surfaceControl{{label: "[Search server conversations]", action: "inbox-server-search", args: m.agentbox.query.Value()}})
	for i := range surface.hits {
		surface.hits[i].owner = fmt.Sprintf("%s\x00%s\x00%s\x00%d", m.agentbox.view, m.agentbox.environment, m.agentbox.project, m.agentbox.minimumPriority)
	}
	return surface
}

func (m *Model) searchInboxServer() tea.Cmd {
	if m.session == nil || !m.inboxServerSearchOffered() {
		return nil
	}
	a := &m.agentbox
	a.searching, a.serverSearch = false, true
	a.view = "all"
	a.ack = nil
	a.decision = false
	a.cursors = map[string]int{}
	// A new explicit search has new membership. Keep exact selection identity
	// and the attached editor, but no old-source rows can masquerade as hits.
	a.page, a.rows, a.loaded = client.InboxPage{}, nil, false
	a.reorderNow = true
	m.notice = "Searching server conversations · All view · current source/project filters retained"
	return tea.Batch(m.loadInbox(true), m.restoreFocus())
}

func (m *Model) applyInboxQuery(previous string) {
	a := &m.agentbox
	if a.query.Value() == previous {
		return
	}
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	// Every changed query retires old result and editor callbacks immediately;
	// typing itself performs no native read. Loaded metadata is filtered locally.
	a.serial++
	a.serverSearch = false
	a.offer = nil
	if m.session != nil {
		for _, source := range m.session.InboxSources() {
			if a.pendingSources[source.ProfileID] {
				source.Error = "Previous query canceled while editing; explicitly search or refresh this source"
				a.page = client.MergeInboxSource(a.page, source)
			}
		}
	}
	a.loading = false
	a.pendingSources = nil
	m.filterInbox()
	m.layoutDirty = true
}

func (m *Model) inboxSourceHealth() string {
	a := &m.agentbox
	failed, unsupported, searched, incomplete := 0, 0, 0, 0
	more := false
	for _, source := range a.page.Sources {
		if source.Error != "" {
			failed++
			continue
		}
		if !source.NativeWorkflows {
			unsupported++
		}
		more = more || source.Page.More
		if source.Page.SearchScope != "" {
			searched++
		}
		if source.Page.SearchIncomplete {
			incomplete++
		}
	}
	var parts []string
	if len(a.pendingSources) > 0 {
		parts = append(parts, fmt.Sprintf("%d loading", len(a.pendingSources)))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d offline/failed", failed))
	}
	if a.serverSearch {
		parts = append(parts, fmt.Sprintf("%d searched", searched))
	} else if searched > 0 {
		parts = append(parts, "Loaded search results")
	}
	if incomplete > 0 {
		parts = append(parts, fmt.Sprintf("%d incomplete", incomplete))
	}
	if unsupported > 0 {
		parts = append(parts, fmt.Sprintf("%d limited", unsupported))
	}
	if more {
		parts = append(parts, "more")
	}
	return strings.Join(parts, " · ")
}
