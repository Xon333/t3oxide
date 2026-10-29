package ui

import (
	"strings"

	"charm.land/bubbles/v2/viewport"
	"github.com/charmbracelet/x/ansi"
)

// A surface carries its controls through the same wrapping, viewport and
// clipping operations as its text. Coordinates remain relative until the
// composed page places the surface. No input action is inferred from a label.
type renderedSurface struct {
	content string
	hits    []hitZone
}

type surfaceControl struct {
	label, action, args string
	disabled            bool
}

func controlSurface(width int, controls []surfaceControl) renderedSurface {
	width = max(1, width)
	rows := []string{""}
	surface := renderedSurface{}
	for _, control := range controls {
		x := ansi.StringWidth(rows[len(rows)-1])
		if x > 0 {
			if x+2+ansi.StringWidth(control.label) > width {
				rows = append(rows, "")
				x = 0
			} else {
				rows[len(rows)-1] += "  "
				x += 2
			}
		}
		for i, line := range strings.Split(ansi.Hardwrap(control.label, width, true), "\n") {
			if i > 0 {
				rows = append(rows, "")
				x = 0
			}
			rows[len(rows)-1] += line
			if !control.disabled && control.action != "" {
				surface.hits = append(surface.hits, hitZone{x: x, y: len(rows) - 1, width: ansi.StringWidth(line), height: 1, action: control.action, args: control.args})
			}
		}
	}
	surface.content = strings.Join(rows, "\n")
	return surface
}

func moveHits(hits []hitZone, x, y int) []hitZone {
	result := make([]hitZone, 0, len(hits))
	for _, hit := range hits {
		hit.x += x
		hit.y += y
		result = append(result, hit)
	}
	return result
}

// Clip controls to visible cells, then translate the viewport origin. Hidden
// labels, offscreen editor rows and frame/help text cannot become hit targets.
func clipHits(hits []hitZone, x, y, width, height int) []hitZone {
	result := make([]hitZone, 0, len(hits))
	for _, hit := range hits {
		left, top := max(hit.x, x), max(hit.y, y)
		right, bottom := min(hit.x+hit.width, x+width), min(hit.y+hit.height, y+height)
		if right > left && bottom > top {
			hit.x, hit.y = left-x, top-y
			hit.width, hit.height = right-left, bottom-top
			result = append(result, hit)
		}
	}
	return result
}

func (s renderedSurface) inViewport(v viewport.Model) renderedSurface {
	return renderedSurface{content: v.View(), hits: clipHits(s.hits, 0, v.YOffset(), v.Width(), v.Height())}
}

func (m *Model) frameSurface(title string, surface renderedSurface, width int) renderedSurface {
	return renderedSurface{content: framed(title, surface.content, width, colors(m.theme), true), hits: moveHits(surface.hits, 2, 1)}
}

func (m *Model) textFormSurface(width, height int) renderedSurface {
	d := m.dialog
	labels := []string{"Submit", "Cancel"}
	controls := make([]surfaceControl, 0, len(labels))
	for i, label := range labels {
		if d.actionFocus == i+1 {
			label = colors(m.theme).selection().Render("[" + label + "]")
		}
		controls = append(controls, surfaceControl{label: label, action: "text-action", args: labels[i], disabled: i == 0 && d.preservedInitial != ""})
	}
	buttons := controlSurface(width, controls)
	question := ""
	questionRows := m.questionViewport.Height()
	if questionRows > 0 {
		question = m.questionViewport.View() + "\n"
	}
	inputHeight := max(1, height-questionRows-strings.Count(buttons.content, "\n")-1)
	input := padBlock(m.modalInput.View(), width, inputHeight)
	result := renderedSurface{content: question + input + "\n" + buttons.content}
	result.hits = append(result.hits, hitZone{x: 0, y: questionRows, width: width, height: inputHeight, action: "text-field"})
	result.hits = append(result.hits, moveHits(buttons.hits, 0, questionRows+inputHeight)...)
	return result
}
