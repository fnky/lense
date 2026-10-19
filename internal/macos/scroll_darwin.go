//go:build darwin

package macos

import "github.com/ebitengine/purego/objc"

// nsEventTypeScrollWheel is NSEventTypeScrollWheel.
const nsEventTypeScrollWheel = 22

// ScrollPhase returns the phases of the scroll event AppKit is handling,
// which MyGo's scroll events leave out: phase is that of the fingers on
// the trackpad, momentum that of the scrolling that coasts on after they
// lift. Both are 0 for a mouse's wheel, or outside a scroll event. Call it
// on the main thread, as a view takes the event.
func ScrollPhase() (phase, momentum Phase) {
	if load() != nil {
		return 0, 0
	}
	ev := class("NSApplication").Send(sel("sharedApplication")).Send(sel("currentEvent"))
	if ev == 0 || objc.Send[uint](ev, sel("type")) != nsEventTypeScrollWheel {
		return 0, 0
	}
	return Phase(objc.Send[uint](ev, sel("phase"))), Phase(objc.Send[uint](ev, sel("momentumPhase")))
}
