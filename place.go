package main

import (
	"strconv"
	"time"
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

// guides are the lines a window snaps to as the user moves it, as
// Raycast's: the edges of columns as wide as the window, centered on the
// display, and the top of the window where it shows first. They show as
// the window nears its first place: the columns as its middle nears the
// display's, the top as its top nears it too.
type guides struct {
	work   rect
	xs     []int   // the columns' edges
	y      int     // the top
	vAlpha float64 // how much the columns show, 0 to 1
	hAlpha float64 // how much the top shows
	snapX  []int   // the edges the window is on
	snapY  bool    // whether its top is on the top
	// since is when the window snapped to each edge it is on, and topSince
	// to the top: the guides flash as it does.
	since    map[int]time.Time
	topSince time.Time
}

// carry keeps when the window snapped to the guides it was on already, and
// says it snapped now to the others it is on.
func (g *guides) carry(prev guides, now time.Time) {
	g.since = map[int]time.Time{}
	for _, x := range g.snapX {
		if t, ok := prev.since[x]; ok {
			g.since[x] = t
		} else {
			g.since[x] = now
		}
	}
	switch {
	case g.snapY && prev.snapY:
		g.topSince = prev.topSince
	case g.snapY:
		g.topSince = now
	}
}

// flash is how blue a guide is a while d after the window snapped to it:
// fully at first, then white again after about half a second.
func flash(d time.Duration) float32 {
	const hold, fade = 150 * time.Millisecond, 450 * time.Millisecond
	switch {
	case d < hold:
		return 1
	case d >= hold+fade:
		return 0
	}
	return 1 - float32(d-hold)/float32(fade)
}

// snapDistance is how near a line an edge snaps to it.
const snapDistance = 12

// snap returns where a window of w×h the user moves to (x, y) goes on a
// display with the work area work, snapped to the guides that show, and
// the guides.
func snap(x, y, w, h int, work rect) (int, int, guides) {
	cx := work.X + work.W/2
	g := guides{work: work, y: work.Y + max(0, (work.H-h)/5)}
	for left := cx - w/2; left >= work.X; left -= w {
		g.xs = append([]int{left}, g.xs...)
	}
	for right := cx - w/2 + w; right <= work.X+work.W; right += w {
		g.xs = append(g.xs, right)
	}
	// The columns show fully near the middle, and fade out by the edge of
	// the display, or two widths away on a wide one.
	g.vAlpha = fade(abs(x+w/2-cx), w/8, max(w/2, min(2*w, work.W/2)))
	g.hAlpha = g.vAlpha * fade(abs(y-g.y), 24, 200)
	if g.vAlpha > 0 {
		best, to := snapDistance+1, x
		for _, l := range g.xs {
			if d := abs(x - l); d < best {
				best, to = d, l
			}
			if d := abs(x + w - l); d < best {
				best, to = d, l-w
			}
		}
		x = to
	}
	if g.hAlpha > 0 && abs(y-g.y) <= snapDistance {
		y, g.snapY = g.y, true
	}
	for _, l := range g.xs {
		if l == x || l == x+w {
			g.snapX = append(g.snapX, l)
		}
	}
	return x, y, g
}

// fade is 1 at a distance d up to full, 0 from none on, and in between
// between them.
func fade(d, full, none int) float64 {
	switch {
	case d <= full:
		return 1
	case d >= none:
		return 0
	}
	return float64(none-d) / float64(none-full)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
