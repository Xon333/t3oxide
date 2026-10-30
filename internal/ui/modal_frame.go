package ui

import (
	"encoding/json"

	"charm.land/lipgloss/v2"
)

type modalFrame struct {
	surface       renderedSurface
	generation    uint64
	width, height int
	authority     string
}

// Hit geometry is captured from the surface actually drawn. Validation keeps
// the current offer identity separate, so a cached frame can never authorize a
// changed native request, model catalog, selected item or resized control.
func (m *Model) modalAuthority() string {
	d := m.dialog
	if d == nil || m.detail != "" || d.details {
		return ""
	}
	values := []any{m.generation, d.serial, m.width, m.height, m.modalWidth, m.modalHeight, m.theme, m.browserPage, m.launchPage, d.surface, d.kind, d.title, d.description, d.local, d.actionFocus, d.finishFocused, d.resultsFocused, d.list.Index(), d.list.Paginator.Page, d.list.FilterInput.Value(), d.list.FilterState(), d.preservedInitial, d.selected, m.questionViewport.YOffset()}
	if d.prompt != nil {
		q := d.prompt.Question
		values = append(values, q.EnvironmentID, q.ThreadID, q.RequestID, q.RequestRevision, q.Kind, q.SingleLine, q.Navigation, q.ConfirmChoice, q.Choices, q.RequestQuestions, q.BrowserPage)
	}
	if d.settingsForm != nil {
		values = append(values, d.settingsForm.selection, d.settingsForm.focus, d.settingsForm.review.Catalog, d.settingsForm.review.Location, d.settingsForm.review.Scope, d.settingsForm.review.Actions)
	}
	if d.requestForm != nil {
		values = append(values, d.requestForm.index, d.requestForm.focus, d.requestForm.option, d.requestForm.questions, d.requestForm.answers, d.requestForm.problem)
	}
	if d.launch != nil {
		values = append(values, d.launch.focus, d.launch.review.Actions, d.launch.review.Location, d.launch.review.Selection, d.launch.review.Unavailable, m.launchInput())
	}
	for _, raw := range d.list.VisibleItems() {
		if i, ok := raw.(choiceItem); ok {
			values = append(values, []any{i.id, i.title, i.description, i.command, i.args, i.disabled, i.multiple, i.checked})
		}
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		// Unencodable values cannot provide a reusable authority fingerprint.
		// In particular, two failures must not compare as one valid offer.
		return ""
	}
	return string(encoded)
}

func (m *Model) captureModalFrame(surface renderedSurface, full bool) {
	hits := append([]hitZone(nil), surface.hits...)
	if !full {
		hits = moveHits(hits, max(0, (m.width-lipgloss.Width(surface.content))/2), max(0, (m.height-lipgloss.Height(surface.content))/2))
	}
	if m.dialog != nil {
		owner := ""
		if m.dialog.settingsForm != nil {
			encoded, _ := json.Marshal(m.dialog.settingsForm.selection)
			owner = string(encoded)
		}
		for i := range hits {
			hits[i].serial = m.dialog.serial
			if m.dialog.settingsForm != nil {
				hits[i].owner = owner
			}
		}
	}
	authority := m.modalAuthority()
	if authority == "" {
		hits = nil
	}
	surface.hits = clipHits(hits, 0, 0, m.width, m.height)
	m.drawnModal = modalFrame{surface: surface, generation: m.generation, width: m.width, height: m.height, authority: authority}
}
