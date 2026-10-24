package ui

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

func commandFilter(term string, targets []string) []list.Rank {
	term = strings.TrimPrefix(strings.TrimSpace(term), "/")
	ranks := list.DefaultFilter(term, targets)
	query := strings.ToLower(strings.TrimSpace(term))
	exact := func(index int) bool {
		words := strings.Fields(strings.ToLower(targets[index]))
		if len(words) == 0 {
			return false
		}
		return words[0] == query || words[0] == "help" && query == "keys" || strings.HasPrefix(strings.ToLower(targets[index]), query+" ")
	}
	slices.SortStableFunc(ranks, func(a, b list.Rank) int {
		if exact(a.Index) && !exact(b.Index) {
			return -1
		}
		if !exact(a.Index) && exact(b.Index) {
			return 1
		}
		return 0
	})
	return ranks
}
func (m *Model) restorePaletteSource() tea.Cmd {
	source := m.dialog.paletteSource
	m.dialog = nil
	if source != nil && source.prompt != nil {
		select {
		case <-source.prompt.Done:
			m.notice = "The underlying request was retired"
			source = nil
		default:
		}
	}
	m.dialog = source
	m.resize()
	return m.restoreFocus()
}
