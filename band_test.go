package main

import (
	"math"
	"testing"
	"time"

	"lense/internal/macos"

	"github.com/egoist/mygo/ui"
)

// pull is a scroll of the trackpad by dy, at t.
func pull(dy float32, t time.Time) ui.InputEvent {
	return ui.InputEvent{Kind: ui.InputScroll, DY: dy, Precise: true, Time: t}
}

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 0.01 }

func TestBandIsAppKits(t *testing.T) {
	// As AppKit's functions answered, on macOS 26.
	for _, c := range []struct {
		from, vel float32
		t         float64
		want      float32
	}{
		{50, 0, 0, 50},
		{50, 0, 0.016, 40.937},
		{50, 0, 0.1, 14.325},
		{50, 0, 0.3, 1.176},
		{0, 2000, 0.016, 8.122},
		{0, 2000, 0.08, 18.247},
		{0, 2000, 0.2, 10.179},
		{0, -2000, 0.08, -18.247},
	} {
		if got := rebound(c.from, c.vel, c.t); !near(got, c.want) {
			t.Errorf("rebound(%v, %v, %v) = %v, want %v", c.from, c.vel, c.t, got, c.want)
		}
	}
	for pull, want := range map[float32]float32{1: 0.05, 100: 5, 1000: 50, -100: -5} {
		if got := pull * elasticity; !near(got, want) {
			t.Errorf("a pull of %v stretches by %v, want %v", pull, got, want)
		}
	}
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
	b.scroll(pull(-100, t0), true, false, macos.PhaseBegan, 0)
	b.scroll(pull(-300, t0), true, false, macos.PhaseChanged, 0)
	if b.pull != 400 || !b.held || !b.phased {
		t.Fatalf("pull %v, held %v", b.pull, b.held)
	}
	lift := t0.Add(time.Second)
	if !b.scroll(pull(0, lift), true, false, macos.PhaseEnded, 0) || b.held || b.pull != 0 {
		t.Fatalf("lifted: pull %v, held %v", b.pull, b.held)
	}
	if !b.going || b.from != 20 || b.vel != 0 || !b.since.Equal(lift) {
		t.Fatalf("goes back from %v at %v, since %v", b.from, b.vel, b.since)
	}
	// The coasting that follows moves nothing, on its way back or once it
	// is back: it doesn't bounce off the end again.
	b.off = 12
	if !b.scroll(pull(-8, lift), true, false, 0, macos.PhaseBegan) || b.from != 20 {
		t.Fatal("coasting moved it on its way back")
	}
	back := b
	back.going, back.off = false, 0
	if !back.scroll(pull(-6, lift.Add(500*time.Millisecond)), true, false, 0, macos.PhaseChanged) || back.going {
		t.Fatal("coasting bounced it off the end again, once back")
	}
	if !back.scroll(pull(0, lift.Add(600*time.Millisecond)), true, false, 0, macos.PhaseEnded) || back.coasting {
		t.Fatal("coasting didn't end")
	}
	if back.scroll(pull(5, lift.Add(time.Second)), true, false, macos.PhaseBegan, 0) {
		t.Fatal("the next gesture didn't scroll the view")
	}
	// The fingers catch it on the way, where it is.
	b.scroll(pull(0, lift), true, false, macos.PhaseMayBegin, 0)
	if !b.held || b.going || !near(b.stretch(), 12) {
		t.Fatalf("caught: %v off, held %v", b.stretch(), b.held)
	}
}

func TestBandCaughtGoesBackAsFastAsScrolled(t *testing.T) {
	t0 := time.Date(2026, 10, 19, 15, 20, 0, 0, time.UTC)
	var b band
	b.going, b.off = true, 12 // going back to the top of its place
	b.scroll(pull(0, t0), true, false, macos.PhaseMayBegin, 0)
	// Further from its place, by a twentieth of what is scrolled.
	b.scroll(pull(-100, t0), true, false, macos.PhaseBegan, 0)
	if !near(b.stretch(), 17) {
		t.Fatalf("pulled further: %v off", b.stretch())
	}
	// Back: what the fingers stretched it by as they did, then the rest as
	// fast as they scroll, as the view scrolls.
	b.scroll(pull(100, t0), true, false, macos.PhaseChanged, 0)
	if !near(b.stretch(), 12) || !b.held {
		t.Fatalf("back by what they pulled: %v off", b.stretch())
	}
	b.scroll(pull(5, t0), true, false, macos.PhaseChanged, 0)
	if !near(b.stretch(), 7) {
		t.Fatalf("back by 5: %v off, want 7", b.stretch())
	}
	if !b.scroll(pull(10, t0), true, false, macos.PhaseChanged, 0) || b.held || b.stretch() != 0 {
		t.Fatalf("back in place: %v off, held %v", b.stretch(), b.held)
	}
	if b.scroll(pull(10, t0), true, false, macos.PhaseChanged, 0) {
		t.Fatal("the view didn't scroll on")
	}
	// Past the bottom, likewise.
	b = band{going: true, off: -12}
	b.scroll(pull(0, t0), false, true, macos.PhaseBegan, 0)
	b.scroll(pull(-12, t0), false, true, macos.PhaseChanged, 0)
	if b.held || b.stretch() != 0 {
		t.Fatalf("back from the bottom: %v off", b.stretch())
	}
}

func TestBandBouncesWhatCoastsIntoTheEnd(t *testing.T) {
	t0 := time.Date(2026, 10, 19, 15, 20, 0, 0, time.UTC)
	var b band
	if b.scroll(pull(30, t0), false, false, 0, macos.PhaseChanged) {
		t.Fatal("coasting off the ends stretched it")
	}
	at := t0.Add(16 * time.Millisecond)
	if !b.scroll(pull(20, at), false, true, 0, macos.PhaseChanged) || !b.coasting {
		t.Fatal("coasting into the end didn't bounce")
	}
	if !b.going || b.from != 0 || b.vel != -1250 || !b.since.Equal(at) {
		t.Fatalf("bounces from %v at %v DIPs a second, want 1250 up", b.from, b.vel)
	}
	if !b.scroll(pull(10, t0.Add(32*time.Millisecond)), false, true, 0, macos.PhaseChanged) || b.vel != -1250 {
		t.Fatal("the rest of the coasting kicked it again")
	}
	if !b.scroll(pull(0, t0.Add(48*time.Millisecond)), false, true, 0, macos.PhaseEnded) || b.coasting {
		t.Fatal("coasting didn't end")
	}
}

func TestTheListStretchesAndGoesBack(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	// off is how far the rows show off their place.
	off := func() float32 {
		h.tt.Frame()
		return h.a.listOff
	}
	// The list shows its top: scrolling up stretches it, down, by a
	// twentieth.
	for range 4 {
		if !h.a.stretch(&h.a.listBand, pull(-100, h.tt.Now())) {
			t.Fatal("the list scrolled past its top")
		}
		h.tt.Advance(8 * time.Millisecond)
	}
	if d := off(); d != 20 {
		t.Fatalf("stretched by %v DIPs, for 400 scrolled", d)
	}
	snapshot(t, h.tt, "list-stretched")
	// The fingers lift: it goes back, as AppKit's.
	lifted := h.tt.Now().Add(releaseAfter - 8*time.Millisecond)
	h.tt.Advance(releaseAfter + 100*time.Millisecond)
	if d, want := off(), rebound(20, 0, h.tt.Now().Sub(lifted).Seconds()); !near(d, want) {
		t.Fatalf("%v DIPs off its place on its way back, want %v", d, want)
	}
	h.tt.Advance(500 * time.Millisecond)
	if d := off(); d != 0 || h.a.listBand.going {
		t.Fatalf("%v DIPs off its place, back", d)
	}
}
