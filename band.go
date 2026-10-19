package main

import (
	"time"

	"bitwarden-mini/internal/macos"

	"github.com/egoist/mygo/ui"
)

// band is the rubber band of a scroll view, as macOS's scroll views have:
// scrolled on past an end of its content with the trackpad, the content
// goes on, less and less far, and springs back as the fingers lift;
// scrolling that coasts on after they lift bounces off the end. MyGo's
// scroll views stop at their ends: the view moves its content by the
// band's offset.
type band struct {
	// at is where the view shows its content (TrackScroll): at an end or
	// not.
	at ui.ScrollState
	// id is what the view shows, the band of which it keeps.
	id string
	// pull is how far the fingers scrolled past an end: past the top when
	// positive, which moves the content down, past the bottom when
	// negative.
	pull float32
	// held is whether the fingers hold the content stretched, and phased
	// whether the events tell when they lift: without phases, they lift
	// once none stretched it for releaseAfter, since stretched.
	held, phased bool
	stretched    time.Time
	// last is when the last event came, for the velocity of the next.
	last time.Time
	// kick is the velocity of the scrolling that coasted into an end, which
	// the next frame bounces the content off it with, and coasting whether
	// it did: the rest of it moves nothing.
	kick     float32
	coasting bool
	// off is how far the content showed off its place in the last frame,
	// and size the height of the view then, the band's length.
	off, size float32
}

const (
	// releaseAfter is how long the fingers are taken to have lifted after
	// the last event, which has no phases.
	releaseAfter = 120 * time.Millisecond
	// settle is how long the content takes to spring back.
	settle = 450 * time.Millisecond
)

// show has b be the band of a view showing id: a new one for another.
func (b *band) show(id string) {
	if b.id != id {
		*b = band{id: id}
	}
}

// stretchy has view keep where it shows its content in b, and stretch past
// its ends as the trackpad scrolls it there.
func (a *app) stretchy(view ui.Element, b *band) ui.Element {
	return view.TrackScroll(&b.at).HandleInput(func(ev ui.InputEvent) bool { return a.stretch(b, ev) })
}

// stretch has b take a scroll event of its view, and reports whether it
// stretched the content rather than the view scrolling it.
func (a *app) stretch(b *band, ev ui.InputEvent) bool {
	if ev.Kind != ui.InputScroll {
		return false
	}
	phase, momentum := a.sys.scrollPhase()
	return b.scroll(ev, b.at.Y <= 0.5, b.at.Y >= b.at.MaxY-0.5, phase, momentum)
}

// scroll takes a scroll event of the view, which shows the top of its
// content when top, and the end when end, with the phases of the gesture
// (macos.ScrollPhase), and reports whether it stretched the content.
func (b *band) scroll(ev ui.InputEvent, top, end bool, phase, momentum macos.Phase) bool {
	if ev.Kind != ui.InputScroll || !ev.Precise {
		return false // a mouse's wheel stops at the ends
	}
	dt := ev.Time.Sub(b.last)
	b.last = ev.Time
	past := ev.DY < 0 && top || ev.DY > 0 && end
	away := b.off <= -0.5 || b.off >= 0.5
	if momentum != 0 {
		switch {
		case momentum&(macos.PhaseEnded|macos.PhaseCancelled) != 0:
			took := b.coasting || away
			b.coasting = false
			return took
		case b.coasting || b.held || away:
			// The content springs back: the coasting goes nowhere.
			return true
		case past:
			// Coasting into the end: the content bounces off it.
			b.coasting = true
			b.kick = -ev.DY / float32(eventGap(dt).Seconds())
			return true
		}
		return false
	}
	switch {
	case phase&(macos.PhaseEnded|macos.PhaseCancelled) != 0:
		held := b.held
		b.held, b.pull = false, 0
		return held
	case phase&(macos.PhaseBegan|macos.PhaseMayBegin) != 0:
		b.coasting = false
		if !b.held && away {
			// The fingers catch the content springing back.
			b.held, b.pull = true, unband(b.off, b.size)
		}
	}
	if !b.held && !past {
		return false
	}
	before := b.pull
	b.pull -= ev.DY
	if before > 0 && b.pull < 0 || before < 0 && b.pull > 0 {
		// Back in its place: the view scrolls on from there.
		b.pull = 0
	}
	b.held, b.phased, b.stretched = b.pull != 0, phase != 0, ev.Time
	return true
}

// offset returns how far the content shows off its place in this frame,
// which the view moves it by: as far as the fingers stretch it, springing
// back once they let go. e is an element of the view that lasts from
// frame to frame, as tall as it, which animates the offset.
func (b *band) offset(c *ui.Context, e ui.Element) float32 {
	const key = "band"
	if r := e.Bounds(); r.H > 0 {
		b.size = r.H
	}
	if b.held && !b.phased {
		if wait := releaseAfter - c.Now().Sub(b.stretched); wait > 0 {
			c.After(wait)
		} else {
			b.held, b.pull = false, 0
		}
	}
	switch {
	case b.held:
		b.off = e.Jump(key, ui.Rubberband(b.pull, 0, 0, b.size))
	case b.kick != 0:
		// Coasting into the end, the content goes on past it, a frame's
		// way at first, and springs back with the velocity it came with,
		// which takes it further first, by about a fortieth of it: at most
		// a quarter of the view.
		most := 10 * max(b.size, 200)
		v := min(max(b.kick, -most), most)
		b.kick = 0
		e.Jump(key, v/120)
		b.off = e.Spring(key, 0, ui.SpringOptions{Duration: settle, Velocity: v})
	default:
		b.off = e.Spring(key, 0, ui.SpringOptions{Duration: settle})
	}
	return b.off
}

// eventGap is the time between two scroll events of a gesture, for its
// velocity: a frame's at 60 Hz when it is unknown or too long to tell.
func eventGap(d time.Duration) time.Duration {
	switch {
	case d <= 0 || d > 50*time.Millisecond:
		return time.Second / 60
	case d < time.Second/240:
		return time.Second / 240
	}
	return d
}

// unband returns how far the fingers pull to stretch the content off its
// place by off, along a band of length size: ui.Rubberband's inverse.
func unband(off, size float32) float32 {
	const c = 0.55 // Rubberband's
	if size <= 0 {
		return 0
	}
	a := min(max(off, -off), 0.99*size)
	x := a / (c * (1 - a/size))
	if off < 0 {
		return -x
	}
	return x
}
