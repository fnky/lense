package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
)

// shortcutsSection lists every action with its keys, which a click
// records anew: the next key pressed, Delete to take it out, Escape to
// leave it.
func (a *app) shortcutsSection(c *ui.Context, k ink) {
	ui.Column(c).Gap(8).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Keyboard shortcuts").FontSize(11).FontWeight(650).TextColor(k.faint).Grow(1)
			textButton(c.Key("reset-keys"), k, "Reset All", nil).OnClick(a.resetKeys)
		})
		ui.Column(c).Gap(4).Padding(10, 14).Radius(10).Background(k.card).Border(1, k.line).Children(func() {
			ui.Text(c, "Click a key to record another; press Delete to take it out, or Escape to keep it.").
				FontSize(11.5).TextColor(k.muted).LineHeight(1.35).Margin(0, 0, 4, 0)
			group := ""
			for _, act := range actions {
				if act.group != group {
					group = act.group
					ui.Text(c, group).FontSize(11).FontWeight(600).TextColor(k.faint).Margin(10, 0, 2, 0)
				}
				a.shortcutRow(c.Key(act.id), k, act)
			}
		})
	})
}

// actionLabel names an action: the vault of a Command-digit, as it is
// now.
func (a *app) actionLabel(act action) string {
	if n, ok := strings.CutPrefix(act.id, "vault-"); ok && len(n) == 1 {
		for _, v := range a.vaultTree() {
			if fmt.Sprint(v.num) == n {
				return "Search " + v.name
			}
		}
	}
	return act.label
}

func (a *app) shortcutRow(c *ui.Context, k ink, act action) {
	keys := a.cfg.keysOf(act.id)
	ui.Column(c).Gap(2).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).MinHeight(30).Children(func() {
			ui.Text(c, a.actionLabel(act)).Grow(1).Shrink(1).SingleLine()
			for i, s := range keys {
				a.keyChip(c.Key(i), k, act, i, keySymbols(s))
			}
			if act.id != "show" {
				adding := a.recording(act.id, len(keys), true)
				add := iconButton(c.Key("add"), k, iconPlusSmall, "Add a key")
				if adding {
					add.Background(k.selected)
				}
				add.OnClick(func() { a.startRecording(act.id, len(keys), true) })
				if adding {
					a.recorderPopover(c.Key("recorder"), k, add)
				}
			}
			if !slices.Equal(keys, act.keys) {
				textButton(c.Key("reset"), k, "Reset", nil).OnClick(func() { a.resetKey(act) })
			} else {
				ui.Box(c).Width(48)
			}
		})
		if a.keyErrID == act.id && a.keyErr != "" {
			ui.Text(c, a.keyErr).FontSize(11.5).TextColor(k.danger).AlignSelf(ui.End)
		}
	})
}

// keyChip shows a key of an action, which a click records anew: the key
// shows as it is till another is taken, by the recorder's popover.
func (a *app) keyChip(c *ui.Context, k ink, act action, i int, label string) {
	recording := a.recording(act.id, i, false)
	b := ui.ButtonBase(c).Padding(3, 8).Radius(6).MinWidth(46).Center().Label(a.actionLabel(act)+" key").
		Border(1, k.line).Background(k.bg)
	switch {
	case recording:
		b.Border(2, k.accent)
	case b.Hovered():
		b.Background(k.hover)
	}
	b.OnClick(func() {
		if recording {
			a.rec = nil
			return
		}
		a.startRecording(act.id, i, false)
	})
	b.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind == ui.InputScroll {
			return a.stretch(&a.settingsBand, ev) // its handler's, which this one comes before
		}
		return false
	})
	b.Children(func() {
		ui.Text(c, label).FontSize(12.5).FontWeight(600)
	})
	if recording {
		a.recorderPopover(c.Key("recorder"), k, b)
	}
}

// recording reports whether the recorder records the i-th key of an
// action, or another, adding.
func (a *app) recording(id string, i int, adding bool) bool {
	r := a.rec
	return r != nil && r.id == id && r.adding == adding && (adding || r.index == i)
}

// startRecording opens the recorder of an action's i-th key, or of
// another, adding.
func (a *app) startRecording(id string, i int, adding bool) {
	a.rec = &recorder{id: id, index: i, adding: adding}
	a.keyErr = ""
}

// recorderPopover shows, by the key it records, what the recorder hears:
// an example, the modifiers held, the key taken in green, or the key
// refused with why in red. It takes the keys pressed while it shows. Its
// width stays: what it says wraps, and it grows in height alone.
func (a *app) recorderPopover(c *ui.Context, k ink, anchor ui.Element) {
	r := a.rec
	r.hold(c.Modifiers())
	if done, left := r.done(a.now()); done {
		a.rec = nil
		return
	} else if left > 0 {
		c.After(left)
	}
	open := true
	ui.PopoverBase(c, anchor, &open, func(panel ui.Element) {
		panel.AttachTo(anchor, ui.AnchorTop, ui.AnchorBottom).Margin(0, 0, 10, 0).Width(recorderWidth).
			Padding(14, 26, 12, 26).Gap(8).AlignItems(ui.Center).Radius(12).
			Background(k.raised).Border(1, k.edge).Shadow(0, 10, 30, 0, ui.RGBA(0, 0, 0, 0.25)).
			Focusable().Focus().HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputKeyDown {
				return false
			}
			if !ev.Repeat {
				a.recordKey(ev.Mods, ev.Key)
			}
			return true
		})
		popoverArrow(c, k, panel, anchor)
		iconButton(c.Key("close"), k, iconClose, "Stop recording").Size(22, 22).
			Absolute().Top(6).Right(6).OnClick(func() {
			if a.rec == r {
				a.rec = nil
			}
		})
		ui.Row(c).Gap(5).AlignItems(ui.Center).MinHeight(22).Children(func() {
			switch r.state {
			case recListening:
				ui.Text(c, "e.g.").FontSize(11.5).TextColor(k.faint).Margin(0, 2, 0, 0)
				for _, s := range keyCaps(ui.Shift|ui.Super, ui.KeySpace) {
					keyCap(c, s, k.faint, k.line)
				}
			case recAccepted:
				for _, s := range keyCaps(r.mods, r.key) {
					keyCap(c, s, k.okay, k.okay.Alpha(0.16))
				}
			default:
				for _, s := range keyCaps(r.mods, r.key) {
					keyCap(c, s, k.text, k.press)
				}
			}
		})
		say, tone := "Recording…", k.muted
		switch r.state {
		case recAccepted:
			say, tone = "New hotkey is set!", k.okay
		case recRefused:
			say, tone = r.why, k.danger
		}
		ui.Text(c, say).FontSize(11.5).TextColor(tone).LineHeight(1.35).TextAlign(ui.Center).AlignSelf(ui.Stretch)
	})
	if !open && a.rec == r {
		a.rec = nil // Escape, or a press outside it
	}
}

// recorderWidth is the width of the recorder's popover.
const recorderWidth = 230

// keyCap shows a key of a hotkey, as its cap.
func keyCap(c *ui.Context, label string, text, cap ui.Color) {
	ui.Box(c).MinWidth(22).Height(22).Padding(0, 6).Center().Radius(5).Background(cap).Children(func() {
		ui.Text(c, label).FontSize(11.5).FontWeight(600).TextColor(text)
	})
}

// popoverArrow points a popover at its anchor, from the side of the panel
// that faces it, as the panel was placed last: above the anchor, or below
// it where there was no room above.
func popoverArrow(c *ui.Context, k ink, panel, anchor ui.Element) {
	p, an := panel.Bounds(), anchor.Bounds()
	if p.W == 0 || an.W == 0 {
		c.After(0) // the panel's first frame: placed, it points
		return
	}
	const size = 12
	x := an.X + an.W/2 - p.X - size/2
	if x < 14 || x > p.W-14-size {
		return // the anchor isn't along the panel
	}
	arrow := ui.Box(c).Absolute().Left(x).Size(size, size).Background(k.raised).
		BorderColor(k.edge).Transform(ui.Rotation(45))
	if p.Y+p.H/2 < an.Y {
		arrow.Bottom(-size/2-1).BorderWidth(0, 1, 1, 0)
	} else {
		arrow.Top(-size/2-1).BorderWidth(1, 0, 0, 1)
	}
}

// recordKey takes a key pressed as the recorder listens: Escape closes it,
// keeping the key it had, and Delete takes that key out. Another key it
// accepts, or refuses, saying why: one that types, or a global shortcut's,
// without ⌘, ⌃ or ⌥; one that another action does where this one works;
// a global shortcut that another app has.
func (a *app) recordKey(mods ui.Modifiers, key ui.Key) {
	r := a.rec
	if r == nil || r.state == recAccepted {
		return
	}
	mods &= modifierKeys
	act, _ := actionByID(r.id)
	keys := slices.Clone(a.cfg.keysOf(act.id))
	switch {
	case mods == 0 && key == ui.KeyEscape:
		a.rec = nil
		return
	case mods == 0 && (key == ui.KeyBackspace || key == ui.KeyDelete):
		if act.id == "show" {
			r.refuse(0, 0, "Lense needs a shortcut to show it.")
			return
		}
		if !r.adding && r.index < len(keys) {
			a.cfg.setKeys(act.id, slices.Delete(keys, r.index, r.index+1))
			a.changed()
		}
		a.rec = nil
		return
	}
	var accel string
	var ok bool
	if act.scope == inGlobal {
		accel, ok = accelerator(mods, key)
	} else {
		accel, ok = windowAccelerator(mods, key)
	}
	if !ok {
		switch _, named := keyNames[key]; {
		case !named: // a key no shortcut takes, as Caps Lock
		case mods == 0:
			r.refuse(mods, key, "At least one modifier should be included in a hotkey.")
		default:
			r.refuse(mods, key, "At least one modifier besides ⇧ should be included in this hotkey.")
		}
		return
	}
	if other := a.cfg.conflict(act.id, accel); other != "" {
		o, _ := actionByID(other)
		r.refuse(mods, key, fmt.Sprintf("%s already does “%s”.", keySymbols(accel), a.actionLabel(o)))
		return
	}
	switch j := slices.Index(keys, accel); {
	case act.id == "show":
		if err := a.setShortcut(accel); err != nil {
			r.refuse(mods, key, fmt.Sprintf("%s is taken by another app. Try another.", keySymbols(accel)))
			return
		}
		keys = []string{accel}
	case j >= 0 && r.adding:
		r.index, r.adding = j, false // it had it already: the popover goes to it
	case j >= 0 && j != r.index:
		keys = slices.Delete(keys, r.index, r.index+1) // it had it already
		if j > r.index {
			j--
		}
		r.index = j
	case r.adding:
		keys = append(keys, accel)
	default:
		keys[r.index] = accel
	}
	a.cfg.setKeys(act.id, keys)
	a.changed()
	r.accept(mods, key, a.now())
}

// resetKey gives an action its keys by default back, unless another took
// them meanwhile.
func (a *app) resetKey(act action) {
	a.keyErrID = act.id
	for _, s := range act.keys {
		if other := a.cfg.conflict(act.id, s); other != "" {
			o, _ := actionByID(other)
			a.keyErr = fmt.Sprintf("%s does “%s” now: give that another key first.", keySymbols(s), a.actionLabel(o))
			return
		}
	}
	if act.id == "show" {
		if err := a.setShortcut(act.keys[0]); err != nil {
			a.keyErr = fmt.Sprintf("%s is taken by another app.", keySymbols(act.keys[0]))
			return
		}
	}
	a.cfg.setKeys(act.id, slices.Clone(act.keys))
	a.changed()
	a.rec, a.keyErr = nil, ""
}

// resetKeys gives every action its keys by default back.
func (a *app) resetKeys() {
	a.cfg.Keys = nil
	a.rec, a.keyErr = nil, ""
	if a.cfg.Shortcut != defaultShortcut {
		if err := a.setShortcut(defaultShortcut); err != nil {
			a.keyErr, a.keyErrID = fmt.Sprintf("%s is taken by another app.", keySymbols(defaultShortcut)), "show"
		} else {
			a.cfg.Shortcut = defaultShortcut
		}
	}
	a.changed()
}
