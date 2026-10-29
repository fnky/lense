package main

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestRecorderStates(t *testing.T) {
	r := &recorder{id: "show"}
	// Modifiers held show as held, in the order of macOS's menus.
	r.hold(ui.Super)
	r.hold(ui.Super | ui.Shift)
	if r.state != recHolding || !slices.Equal(keyCaps(r.mods, r.key), []string{"⇧", "⌘"}) {
		t.Fatalf("held: %+v", r)
	}
	// Some let go, the rest show; all let go, it listens again.
	r.hold(ui.Shift)
	if r.state != recHolding || r.mods != ui.Shift {
		t.Fatalf("let go of ⌘: %+v", r)
	}
	r.hold(0)
	if r.state != recListening || r.mods != 0 {
		t.Fatalf("let go: %+v", r)
	}
	// Caps Lock is none of them.
	r.hold(1 << 7)
	if r.state != recListening {
		t.Fatalf("held another: %+v", r)
	}

	// A key refused shows as its modifiers are let go, those it came with
	// whether or not they showed held before, till another is pressed.
	r.refuse(ui.Super, ui.KeyO, "taken")
	r.hold(ui.Super)
	r.hold(0)
	if r.state != recRefused || r.why != "taken" || !slices.Equal(keyCaps(r.mods, r.key), []string{"⌘", "O"}) {
		t.Fatalf("refused: %+v", r)
	}
	r.hold(ui.Ctrl)
	if r.state != recHolding || r.mods != ui.Ctrl || r.key != 0 || r.why != "" {
		t.Fatalf("after a refusal: %+v", r)
	}

	// A key taken shows for a second, whatever is held.
	at := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	r.accept(ui.Ctrl, ui.KeySpace, at)
	r.hold(0)
	if r.state != recAccepted || !slices.Equal(keyCaps(r.mods, r.key), []string{"⌃", "Space"}) {
		t.Fatalf("accepted: %+v", r)
	}
	if done, left := r.done(at.Add(400 * time.Millisecond)); done || left != 600*time.Millisecond {
		t.Errorf("done %v, %v left", done, left)
	}
	if done, _ := r.done(at.Add(time.Second)); !done {
		t.Error("not done after a second")
	}
}
