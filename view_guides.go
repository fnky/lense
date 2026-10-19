package main

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// The guides are dashed, and faint: white, as much as they show, blue for
// a moment as the window snaps to them.
const (
	guideDash  = 5
	guideGap   = 5
	guideWhite = 0.32
)

// drawGuides paints the guides of a display over its work area r.
func drawGuides(p *ui.Painter, r ui.Rect, g guides, accent ui.Color) {
	ox, oy := float32(g.work.X), float32(g.work.Y)
	color := func(show float64, since time.Time, on bool) (ui.Color, float32) {
		c := ui.RGBA(255, 255, 255, float32(guideWhite*show))
		if !on {
			return c, 1
		}
		k := flash(p.Now().Sub(since))
		if k > 0 {
			p.AnimationFrame() // until it is white again
		}
		return c.Mix(accent.Alpha(0.95), k), 1 + 0.5*k
	}
	if g.vAlpha > 0 {
		for _, x := range g.xs {
			since, on := g.since[x]
			c, w := color(g.vAlpha, since, on)
			lx := r.X + float32(x) - ox
			for y := r.Y; y < r.Y+r.H; y += guideDash + guideGap {
				p.Line(lx, y, lx, min(y+guideDash, r.Y+r.H), w, c)
			}
		}
	}
	if g.hAlpha > 0 {
		c, w := color(g.hAlpha, g.topSince, g.snapY)
		ly := r.Y + float32(g.y) - oy
		for x := r.X; x < r.X+r.W; x += guideDash + guideGap {
			p.Line(x, ly, min(x+guideDash, r.X+r.W), ly, w, c)
		}
	}
}
