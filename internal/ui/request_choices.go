package ui

import (
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

func (m *Model) searchRequestChoices() tea.Cmd {
	source := m.dialog
	if source == nil || source.requestForm == nil || !modalPromptLive(source) {
		return nil
	}
	if !m.saveRequestAnswer() {
		return nil
	}
	f := source.requestForm
	q := f.questions[f.index]
	items := []list.Item{}
	for _, option := range q.Options {
		items = append(items, choiceItem{id: option.Value, title: option.Label, description: option.Description})
	}
	m.openLocal("Choices · "+q.ID, "request-choices", items)
	m.dialog.requestSource = source
	return nil
}
func (m *Model) restoreRequestChoices(selectValue bool) tea.Cmd {
	picker := m.dialog
	if picker == nil || picker.requestSource == nil {
		return nil
	}
	source := picker.requestSource
	if !modalPromptLive(source) {
		m.dialog = nil
		m.notice = "Request changed; review its current form"
		return m.restoreFocus()
	}
	selected, ok := picker.list.SelectedItem().(choiceItem)
	m.dialog = source
	if selectValue && ok {
		f := source.requestForm
		for i, option := range f.questions[f.index].Options {
			if option.Value == selected.id {
				f.focus = 0
				f.option = i
				m.toggleRequestOption()
				break
			}
		}
	}
	m.resize()
	return m.restoreFocus()
}
