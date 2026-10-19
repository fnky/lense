package main

import (
	"slices"

	"github.com/egoist/mygo/ui"
)

// drawGuides paints the guides of a display over its work area r: the
// edges of the columns and the top, as much as they show, and brighter
// those the window is on.
func drawGuides(p *ui.Painter, r ui.Rect, g guides, accent ui.Color) {
	ox, oy := float32(g.work.X), float32(g.work.Y)
	if g.vAlpha > 0 {
		for _, x := range g.xs {
			a, width := 0.35*g.vAlpha, float32(1)
			if slices.Contains(g.snapX, x) {
				a, width = g.vAlpha, 2
			}
			lx := r.X + float32(x) - ox
			p.Line(lx, r.Y, lx, r.Y+r.H, width, accent.Alpha(float32(a)))
		}
	}
	if g.hAlpha > 0 {
		a, width := 0.35*g.hAlpha, float32(1)
		if g.snapY {
			a, width = g.hAlpha, 2
		}
		ly := r.Y + float32(g.y) - oy
		p.Line(r.X, ly, r.X+r.W, ly, width, accent.Alpha(float32(a)))
	}
}
