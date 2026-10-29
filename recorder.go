package main

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// recState is where the recorder of a key is.
type recState int

const (
	// recListening has nothing held: the recorder shows an example.
	recListening recState = iota
	// recHolding has modifiers held, which it shows as they are held.
	recHolding
	// recAccepted has taken a key, which it shows set for acceptedFor.
	recAccepted
	// recRefused has refused a key, which it shows with why, till a
	// modifier is pressed for another.
	recRefused
)

// acceptedFor is how long the recorder shows the key it took.
const acceptedFor = time.Second

// recorder records a key of an action in a popover, by the key it
// replaces, or by + for another: a finite state machine of the keys
// pressed.
//
//	listening --press modifiers--> holding --release them--> listening
//	listening, holding --press a key--> accepted, or refused
//	refused --press a modifier--> holding
//	refused --press a key--> accepted, or refused
//	accepted --acceptedFor--> closed
//
// Escape, the popover's close button, and a press outside it close it
// from any state, keeping the key it had.
type recorder struct {
	id string
	// index is the key it replaces, or adding another, the key it took.
	index  int
	adding bool
	state  recState
	// held is the modifiers held as last seen; mods and key those shown.
	held, mods ui.Modifiers
	key        ui.Key
	why        string    // why the key was refused
	at         time.Time // when it was accepted
}

// modifierKeys are the modifiers a key may be recorded with.
const modifierKeys = ui.Ctrl | ui.Alt | ui.Shift | ui.Super

// hold takes the modifiers held now, as they change.
func (r *recorder) hold(mods ui.Modifiers) {
	mods &= modifierKeys
	pressed := mods &^ r.held
	if mods == r.held {
		return
	}
	r.held = mods
	switch r.state {
	case recListening:
		r.state, r.mods = recHolding, mods
	case recHolding:
		r.mods = mods
		if mods == 0 {
			r.state = recListening
		}
	case recRefused:
		// Another modifier pressed begins another key; those of the
		// one refused let go leave it showing.
		if pressed != 0 {
			r.state, r.mods, r.key, r.why = recHolding, mods, 0, ""
		}
	}
}

// accept shows the key taken, from now. The modifiers it was pressed with
// are held, whether or not they showed held before it.
func (r *recorder) accept(mods ui.Modifiers, key ui.Key, now time.Time) {
	mods &= modifierKeys
	r.state, r.held, r.mods, r.key, r.why, r.at = recAccepted, mods, mods, key, "", now
}

// refuse shows the key refused, and why.
func (r *recorder) refuse(mods ui.Modifiers, key ui.Key, why string) {
	mods &= modifierKeys
	r.state, r.held, r.mods, r.key, r.why = recRefused, mods, mods, key, why
}

// done reports whether the key taken has shown long enough by now, or
// how much longer it shows.
func (r *recorder) done(now time.Time) (bool, time.Duration) {
	if r.state != recAccepted {
		return false, 0
	}
	left := acceptedFor - now.Sub(r.at)
	return left <= 0, left
}

// keyCaps are the keys of a binding, as their caps show them: the
// modifiers in the order of macOS's menus, ⌃⌥⇧⌘, then the key.
func keyCaps(mods ui.Modifiers, key ui.Key) []string {
	var caps []string
	for _, m := range []struct {
		mod ui.Modifiers
		sym string
	}{{ui.Ctrl, "⌃"}, {ui.Alt, "⌥"}, {ui.Shift, "⇧"}, {ui.Super, "⌘"}} {
		if mods&m.mod != 0 {
			caps = append(caps, m.sym)
		}
	}
	if name, ok := keyNames[key]; ok {
		caps = append(caps, keySymbols(name))
	} else if key == ui.KeyEscape {
		caps = append(caps, "esc")
	}
	return caps
}
