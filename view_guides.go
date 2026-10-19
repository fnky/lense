package main

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// The guides are drawn as Raycast draws its own: dashes of 8 points 12
// apart, 2 points wide, in gray at half opacity, which is lighter than a
// dark background and darker than a light one, so it shows over both. They
// show as much as the window is near its first place, and turn blue for a
// moment as it snaps to them.
const (
	guideDash  = 8
	guideGap   = 12
	guideWidth = 2
	guideAlpha = 0.5
)

// drawGuides paints the guides of a display over its work area r.
func drawGuides(p *ui.Painter, r ui.Rect, g guides, accent ui.Color) {
	ox, oy := float32(g.work.X), float32(g.work.Y)
	color := func(show float64, since time.Time, on bool) ui.Color {
		c := ui.RGBA(128, 128, 128, float32(guideAlpha*show))
		if !on {
			return c
		}
		k := flash(p.Now().Sub(since))
		if k > 0 {
			p.AnimationFrame() // until it is gray again
		}
		return c.Mix(accent.Alpha(0.95), k)
	}
	if g.vAlpha > 0 {
		for _, x := range g.xs {
			since, on := g.since[x]
			c := color(g.vAlpha, since, on)
			lx := r.X + float32(x) - ox
			for y := r.Y; y < r.Y+r.H; y += guideDash + guideGap {
				p.Line(lx, y, lx, min(y+guideDash, r.Y+r.H), guideWidth, c)
			}
		}
	}
	if g.hAlpha > 0 {
		c := color(g.hAlpha, g.topSince, g.snapY)
		ly := r.Y + float32(g.y) - oy
		for x := r.X; x < r.X+r.W; x += guideDash + guideGap {
			p.Line(x, ly, min(x+guideDash, r.X+r.W), ly, guideWidth, c)
		}
	}
}
