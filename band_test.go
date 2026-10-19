package main

import (
	"math"
	"testing"
	"time"

	"bitwarden-mini/internal/macos"

	"github.com/egoist/mygo/ui"
)

// pull is a scroll of the trackpad by dy, at t.
func pull(dy float32, t time.Time) ui.InputEvent {
	return ui.InputEvent{Kind: ui.InputScroll, DY: dy, Precise: true, Time: t}
}

func TestBandStretchesPastTheEnds(t *testing.T) {
	t0 := time.Date(2026, 10, 19, 15, 20, 0, 0, time.UTC)
	var b band
	if b.scroll(ui.InputEvent{Kind: ui.InputScroll, DY: -40, Time: t0}, true, false, 0, 0) {
		t.Fatal("a mouse's wheel stretched it")
	}
	if b.scroll(pull(-30, t0), false, false, 0, 0) {
		t.Fatal("stretched off the ends")
	}
	if !b.scroll(pull(-30, t0), true, false, 0, 0) || b.pull != 30 || !b.held {
		t.Fatalf("past the top: pull %v, held %v", b.pull, b.held)
	}
	// Scrolling back lets the band go, down to its place, and no further.
	if !b.scroll(pull(20, t0), true, false, 0, 0) || b.pull != 10 {
		t.Fatalf("back: pull %v", b.pull)
	}
	if !b.scroll(pull(25, t0), true, false, 0, 0) || b.pull != 0 || b.held {
		t.Fatalf("back in place: pull %v, held %v", b.pull, b.held)
	}
	if b.scroll(pull(25, t0), true, false, 0, 0) {
		t.Fatal("the view didn't scroll on")
	}
	if !b.scroll(pull(12, t0), false, true, 0, 0) || b.pull != -12 {
		t.Fatalf("past the bottom: pull %v", b.pull)
	}
}

func TestBandLetsGoAsTheFingersLift(t *testing.T) {
	t0 := time.Date(2026, 10, 19, 15, 20, 0, 0, time.UTC)
	var b band
	b.scroll(pull(0, t0), true, false, macos.PhaseMayBegin, 0)
	b.scroll(pull(-10, t0), true, false, macos.PhaseBegan, 0)
	b.scroll(pull(-20, t0), true, false, macos.PhaseChanged, 0)
	if b.pull != 30 || !b.held || !b.phased {
		t.Fatalf("pull %v, held %v", b.pull, b.held)
	}
	if !b.scroll(pull(0, t0), true, false, macos.PhaseEnded, 0) || b.held || b.pull != 0 {
		t.Fatalf("lifted: pull %v, held %v", b.pull, b.held)
	}
	// Springing back, the coasting that follows moves nothing.
	b.off = 20
	if !b.scroll(pull(-8, t0), true, false, 0, macos.PhaseBegan) || b.kick != 0 {
		t.Fatalf("coasting while it springs back: kick %v", b.kick)
	}
	// The fingers catch it on the way, where it is.
	b.size = 400
	b.scroll(pull(0, t0), true, false, macos.PhaseMayBegin, 0)
	if !b.held || math.Abs(float64(ui.Rubberband(b.pull, 0, 0, b.size)-20)) > 0.01 {
		t.Fatalf("caught: pull %v, held %v", b.pull, b.held)
	}
}

func TestBandBouncesWhatCoastsIntoTheEnd(t *testing.T) {
	t0 := time.Date(2026, 10, 19, 15, 20, 0, 0, time.UTC)
	var b band
	if b.scroll(pull(30, t0), false, false, 0, macos.PhaseChanged) {
		t.Fatal("coasting off the ends stretched it")
	}
	if !b.scroll(pull(20, t0.Add(16*time.Millisecond)), false, true, 0, macos.PhaseChanged) || !b.coasting {
		t.Fatal("coasting into the end didn't bounce")
	}
	if b.kick != -1250 {
		t.Fatalf("kick %v, want the coasting's 1250 DIPs a second, up", b.kick)
	}
	b.kick, b.off = 0, -12
	if !b.scroll(pull(10, t0.Add(32*time.Millisecond)), false, true, 0, macos.PhaseChanged) || b.kick != 0 {
		t.Fatal("the rest of the coasting kicked it again")
	}
	if !b.scroll(pull(0, t0.Add(48*time.Millisecond)), false, true, 0, macos.PhaseEnded) || b.coasting {
		t.Fatal("coasting didn't end")
	}
}

func TestUnband(t *testing.T) {
	for _, x := range []float32{0, 5, 80, 400, -60} {
		if got := unband(ui.Rubberband(x, 0, 0, 300), 300); math.Abs(float64(got-x)) > 0.01*max(1, math.Abs(float64(x))) {
			t.Errorf("unband(Rubberband(%v)) = %v", x, got)
		}
	}
}

func TestTheListStretchesAndSpringsBack(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	// off is how far the rows show off their place.
	off := func() float32 {
		h.tt.Frame()
		return h.a.listOff
	}
	// The list shows its top: scrolling up stretches it, down.
	for range 4 {
		if !h.a.stretch(&h.a.listBand, pull(-30, h.tt.Now())) {
			t.Fatal("the list scrolled past its top")
		}
		h.tt.Advance(8 * time.Millisecond)
	}
	if d := off(); d < 40 || d > 120 {
		t.Fatalf("stretched by %v DIPs, for 120 scrolled", d)
	}
	snapshot(t, h.tt, "list-stretched")
	// The fingers lift: it springs back.
	h.tt.Advance(releaseAfter + settle + 200*time.Millisecond)
	if d := off(); d < -0.5 || d > 0.5 {
		t.Fatalf("%v DIPs off its place, sprung back", d)
	}
}
