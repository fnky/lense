package main

import (
	"math"
	"time"

	"lense/internal/macos"

	"github.com/egoist/mygo/ui"
)

// band is the rubber band of a scroll view, as macOS's scroll views have:
// scrolled on past an end of its content with the trackpad, the content
// goes on, much less far; scrolled back, it comes back as fast as the view
// scrolls, as in Finder, and goes back as the fingers lift; scrolling that
// coasts on after they lift bounces off the end. MyGo's scroll views stop
// at their ends: the view moves its content by the band's offset.
type band struct {
	// at is where the view shows its content (TrackScroll): at an end or
	// not.
	at ui.ScrollState
	// id is what the view shows, the band of which it keeps.
	id string
	// pull is how far the fingers hold the content off its place, in DIPs:
	// past the top when positive, which moves the content down, past the
	// bottom when negative.
	pull float32
	// held is whether the fingers hold the content stretched, and phased
	// whether the events tell when they lift: without phases, they lift
	// once none stretched it for releaseAfter, since stretched.
	held, phased bool
	stretched    time.Time
	// last is when the last event came, for the velocity of the next.
	last time.Time
	// going is whether the content goes back to its place (rebound): from
	// off by from, at vel DIPs a second, since then. coasting is whether
	// what coasts on of the gesture moves nothing: it coasted into the end,
	// which it bounces off, or the fingers lifted with the content
	// stretched.
	going, coasting bool
	from, vel       float32
	since           time.Time
	// off is how far the content showed off its place in the last frame.
	off float32
}

// The rubber band of AppKit's scroll views, as its functions that WebKit
// scrolls with too say (_NSElasticDeltaForReboundDelta,
// _NSElasticDeltaForTimeDelta): the content stretches by a twentieth of
// how far it is scrolled past the end, and goes back from there as
// rebound says.
const (
	elasticity       = 1.0 / 20
	reboundRate      = 12.5 // a second
	reboundAmplitude = 0.31
)

// releaseAfter is how long the fingers are taken to have lifted after the
// last event, which has no phases.
const releaseAfter = 120 * time.Millisecond

// rebound returns how far off its place content is t seconds after it
// starts back, from off by from at vel DIPs a second away from its place:
// back at once, decaying, or first further, by about 1% of vel at most,
// as what coasted into the end bounces.
func rebound(from, vel float32, t float64) float32 {
	return float32((float64(from) + reboundAmplitude*float64(vel)*t) * math.Exp(-reboundRate*t))
}

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
	if momentum != 0 {
		switch {
		case momentum&(macos.PhaseEnded|macos.PhaseCancelled) != 0:
			took := b.coasting || b.going
			b.coasting = false
			return took
		case b.coasting || b.held || b.going:
			// The content goes back: the coasting goes nowhere.
			return true
		case past:
			// Coasting into the end: the content bounces off it.
			b.coasting = true
			b.goBack(ev.Time, -ev.DY/float32(eventGap(dt).Seconds()))
			return true
		}
		return false
	}
	switch {
	case phase&(macos.PhaseEnded|macos.PhaseCancelled) != 0:
		held := b.held
		if held {
			// The scrolling that coasts on after the fingers lift moves
			// nothing, even once the content is back: it would bounce it
			// off the end again.
			b.goBack(ev.Time, 0)
			b.coasting = true
		}
		return held
	case phase&(macos.PhaseBegan|macos.PhaseMayBegin) != 0:
		b.coasting = false
		if b.going {
			// The fingers catch the content going back, where it is.
			b.going, b.held, b.pull = false, b.off != 0, b.off
		}
	}
	if !b.held && !past {
		return false
	}
	d := -ev.DY // down, past the top
	switch p := b.pull + d; {
	case b.pull == 0 || d > 0 == (b.pull > 0):
		b.pull += d * elasticity // further from its place, by a twentieth
	case p == 0 || p > 0 != (b.pull > 0):
		b.pull = 0 // back in its place: the view scrolls on from there
	default:
		b.pull = p // back toward it, as fast as the view scrolls
	}
	b.held, b.phased, b.stretched = b.pull != 0, phase != 0, ev.Time
	return true
}

// goBack lets the content go back to its place from where it is, from at,
// going away from it at vel DIPs a second.
func (b *band) goBack(at time.Time, vel float32) {
	from := b.off
	if b.held {
		from = b.pull
	}
	b.going, b.from, b.vel, b.since = true, from, vel, at
	b.held, b.pull = false, 0
}

// offset returns how far the content shows off its place in this frame,
// which the view moves it by: as far as the fingers stretch it, going back
// once they let go.
func (b *band) offset(c *ui.Context) float32 {
	now := c.Now()
	if b.held && !b.phased {
		if wait := releaseAfter - now.Sub(b.stretched); wait > 0 {
			c.After(wait)
		} else {
			b.goBack(b.stretched.Add(releaseAfter), 0)
		}
	}
	switch {
	case b.held:
		b.off = b.pull
	case b.going:
		t := now.Sub(b.since).Seconds()
		b.off = rebound(b.from, b.vel, t)
		// Once past the furthest a bounce goes, it is back within half a DIP.
		if t >= 1/reboundRate && b.off > -0.5 && b.off < 0.5 {
			b.going, b.off = false, 0
		} else {
			c.AnimationFrame()
		}
	default:
		b.off = 0
	}
	return b.off
}

// barInsets moves view's scroll bar in from its edges by inset, CSS style
// as ScrollbarInsets, and in from the end opposite the one the content is
// stretched past as it is off its place, as the band moved it in this
// frame (offset): the thumb, pinned to that end, shrinks to the share of
// the view that shows the content, as AppKit's knob does as the content
// bounces.
func (b *band) barInsets(view ui.Element, top, right, bottom, left float32) ui.Element {
	top, bottom = squeeze(b.off, view.Bounds().H, top, bottom)
	return view.ScrollbarInsets(top, right, bottom, left)
}

// squeeze returns the top and bottom insets of the scroll bar of a view
// high h, whose content is off its place by off: the bar's track shorter by
// the share of it off is of h, at the end the thumb is away from, so that
// it shrinks as the share of the view the content shows (AppKit's knob
// proportion, (visible − overhang) / content).
func squeeze(off, h, top, bottom float32) (float32, float32) {
	if off == 0 || h <= 0 {
		return top, bottom
	}
	by := min(float32(math.Abs(float64(off))), h) * max(h-top-bottom, 0) / h
	if off > 0 {
		return top, bottom + by // past the top: the thumb at the top
	}
	return top + by, bottom
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
