package main

import (
	"slices"
	"testing"
)

func TestSnapGuides(t *testing.T) {
	work := rect{0, 25, 1512, 920} // a laptop's display
	w, h := windowWidth, windowHeight
	home := 25 + (920-h)/5
	// The columns: one centered, then as wide as the window out to the
	// edges, as far as they fit: on a laptop, the middle one alone.
	_, _, g := snap((1512-w)/2, home, w, h, work)
	if want := []int{376, 376 + w}; !slices.Equal(g.xs, want) {
		t.Fatalf("columns %v", g.xs)
	}
	wide := rect{1512, 0, 2560, 1415}
	if _, _, gw := snap(1512+900, 283, w, h, wide); !slices.Equal(gw.xs, []int{1512 + 140, 1512 + 900, 1512 + 1660, 1512 + 2420}) {
		t.Fatalf("wide columns %v", gw.xs)
	}
	if g.vAlpha != 1 || g.hAlpha != 1 || !g.snapY || len(g.snapX) != 2 {
		t.Fatalf("at home: %+v", g)
	}

	// Near the middle, an edge snaps to a column's.
	x, y, g := snap(376+8, home+100, w, h, work)
	if x != 376 || y != home+100 || g.snapY {
		t.Fatalf("snapped to %d,%d %+v", x, y, g)
	}
	if g.hAlpha == 0 || g.hAlpha >= 1 {
		t.Errorf("the top half shows: %v", g.hAlpha)
	}
	// The right edge snaps too.
	x, _, _ = snap(376+11, home, w, h, work) // its right edge 11 past 1136
	if x != 376 {
		t.Errorf("right edge: %d", x)
	}
	// Near the top, the top snaps.
	_, y, g = snap(376, home-9, w, h, work)
	if y != home || !g.snapY {
		t.Errorf("top: %d %+v", y, g)
	}

	// Far from the middle, nothing shows, nothing snaps.
	x, y, g = snap(1400, home+3, w, h, work)
	if x != 1400 || y != home+3 || g.vAlpha != 0 || g.hAlpha != 0 || g.snapY {
		t.Errorf("far: %d,%d %+v", x, y, g)
	}
	// Low on the display, the columns show, not the top.
	_, y, g = snap(376, home+400, w, h, work)
	if g.vAlpha != 1 || g.hAlpha != 0 || y != home+400 {
		t.Errorf("low: %+v", g)
	}
}
