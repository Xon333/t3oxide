package ui

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

// Commands temporarily cover one local search or read-only detail surface.
// Keeping the native viewport preserves scroll; generation prevents a return
// from carrying content or focus into a different conversation attachment.
type paletteReturn struct {
	generation          uint64
	searching           bool
	detail, title, copy string
	viewport            viewport.Model
	isActivity          bool
	detailGeneration    uint64
}

func commandFilter(term string, targets []string) []list.Rank {
	term = strings.TrimPrefix(strings.TrimSpace(term), "/")
	ranks := list.DefaultFilter(term, targets)
	query := strings.ToLower(strings.TrimSpace(term))
	priority := func(index int) int {
		identity, remainder, structured := strings.Cut(strings.ToLower(targets[index]), "\t")
		words := strings.Fields(identity)
		if len(words) == 0 {
			return 3
		}
		if words[0] == query || words[0] == "help" && query == "keys" || strings.TrimSpace(identity) == query || strings.HasPrefix(identity, query+" ") {
			return 0
		}
		if structured {
			title, _, _ := strings.Cut(remainder, "\t")
			if title == query {
				return 1
			}
			if strings.HasPrefix(title, query+" ") {
				return 2
			}
		}
		return 3
	}
	slices.SortStableFunc(ranks, func(a, b list.Rank) int {
		return priority(a.Index) - priority(b.Index)
	})
	return ranks
}
func (m *Model) restorePaletteSource() tea.Cmd {
	source := m.dialog.paletteSource
	retained := m.dialog.paletteReturn
	m.dialog = nil
	if !dialogSourceLive(source) {
		m.notice = "The underlying request was retired"
		source = nil
		retained = nil
		if m.browserDialog != nil && !dialogSourceLive(m.browserDialog) {
			m.browserDialog = nil
		}
		if !m.actionBusy && !m.browserBusy {
			m.browserPage, m.launchPage = false, false
		}
	}
	m.dialog = source
	m.detailIsActivity, m.detailGeneration = false, 0
	if retained != nil && retained.generation == m.generation {
		m.searching = retained.searching
		m.detail, m.detailTitle, m.detailCopy = retained.detail, retained.title, retained.copy
		m.detailViewport = retained.viewport
		m.detailIsActivity, m.detailGeneration = retained.isActivity, retained.detailGeneration
	}
	m.resize()
	return m.restoreFocus()
}

// A local chooser or More menu borrows its owning prompt's lifetime. Closing
// Commands must not revive those controls after that owner has retired.
func dialogSourceLive(source *dialog) bool {
	seen := make(map[*dialog]bool)
	for source != nil {
		if seen[source] {
			return false
		}
		seen[source] = true
		if source.prompt != nil {
			select {
			case <-source.prompt.Done:
				return false
			default:
				return true
			}
		}
		switch {
		case source.formSource != nil:
			source = source.formSource
		case source.browserSource != nil:
			source = source.browserSource
		default:
			return true
		}
	}
	return true
}
