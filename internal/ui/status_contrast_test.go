package ui

import (
	"image/color"
	"math"
	"testing"

	"github.com/Xon333/t3oxide/internal/client"
)

func statusLuminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	linear := func(v uint32) float64 {
		value := float64(v) / 65535
		if value <= 0.04045 {
			return value / 12.92
		}
		return math.Pow((value+0.055)/1.055, 2.4)
	}
	return 0.2126*linear(r) + 0.7152*linear(g) + 0.0722*linear(b)
}

func TestCriticalStatusColorsIgnoreThemeAndKeepHighContrast(t *testing.T) {
	for _, theme := range semanticSlotThemes() {
		for _, tc := range []struct{ state, symbol, color string }{
			{"failed", "!", "#FF7575"},
			{"completed", "+", "#5CFF85"},
			{"waiting", "?", "#FFE45E"},
			{"running", ">", "#5DE8FF"},
		} {
			t.Run(theme+"/"+tc.state, func(t *testing.T) {
				p := colors(theme)
				badge := threadStatusBadge(client.Thread{Status: tc.state}).compact()
				for _, selected := range []bool{false, true} {
					base := p.plain()
					if selected {
						base = p.selection()
					}
					rendered := badge.style(p, base).Render(badge.label)
					cells := semanticSlotCells(t, rendered, tc.symbol)
					for _, cell := range cells {
						assertSemanticSlotColor(t, cell.Style.Fg, tc.color)
						if cell.Style.Bg == nil {
							t.Fatal("status contrast depends on unknown terminal background")
						}
						contrast := (statusLuminance(cell.Style.Fg) + 0.05) / (statusLuminance(cell.Style.Bg) + 0.05)
						if contrast < 7 {
							t.Fatalf("status contrast %.2f:1 is below 7:1", contrast)
						}
					}
				}
			})
		}
	}
}
