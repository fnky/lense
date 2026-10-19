package main

import (
	"strconv"
)

// rect is a rectangle of the screen, in DIPs.
type rect struct{ X, Y, W, H int }

// spot is where the window was on a display: its center's offset from the
// middle of the display's work area, and its top's from the work area's
// top, so that it keeps its place on displays of another size.
type spot struct {
	DX int `json:"dx"`
	DY int `json:"dy"`
}

// placements remembers where the user put the window on each display, by
// its ID, and on the last one, for displays it never showed on.
type placements struct {
	Displays map[string]spot `json:"displays,omitempty"`
	Last     *spot           `json:"last,omitempty"`
}

func displayKey(id int64) string { return strconv.FormatInt(id, 10) }

// place returns where a window of size w×h shows on a display with the
// work area work: where it was there, else where it was last on another,
// else centered, a fifth of the way down, as Spotlight shows.
func (p placements) place(display int64, work rect, w, h int) (x, y int) {
	s, ok := p.Displays[displayKey(display)]
	if !ok && p.Last != nil {
		s, ok = *p.Last, true
	}
	if !ok {
		s = spot{DX: 0, DY: max(0, (work.H-h)/5)}
	}
	x = work.X + work.W/2 + s.DX - w/2
	y = work.Y + s.DY
	// Keep it on the display.
	x = min(max(x, work.X), work.X+work.W-w)
	y = min(max(y, work.Y), work.Y+work.H-h)
	if w > work.W {
		x = work.X
	}
	if h > work.H {
		y = work.Y
	}
	return x, y
}

// remember records where the window is, on the display it is on.
func (p *placements) remember(display int64, work rect, win rect) {
	s := spot{DX: win.X + win.W/2 - (work.X + work.W/2), DY: win.Y - work.Y}
	if p.Displays == nil {
		p.Displays = map[string]spot{}
	}
	p.Displays[displayKey(display)] = s
	p.Last = &s
}
