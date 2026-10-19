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
			adding := a.recordID == act.id && a.recordIndex >= len(keys)
			if adding {
				a.keyChip(c.Key("new"), k, act, len(keys), "")
			}
			if act.id != "show" && !adding {
				iconButton(c.Key("add"), k, iconPlusSmall, "Add a key").OnClick(func() {
					a.recordID, a.recordIndex, a.keyErr = act.id, len(keys), ""
				})
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

// keyChip shows a key of an action, which a click records anew.
func (a *app) keyChip(c *ui.Context, k ink, act action, i int, label string) {
	recording := a.recordID == act.id && a.recordIndex == i
	b := ui.ButtonBase(c).Padding(3, 8).Radius(6).MinWidth(46).Center().Label(a.actionLabel(act)+" key").
		Border(1, k.line).Background(k.bg)
	switch {
	case recording:
		b.Border(2, k.accent).Focus()
	case b.Hovered():
		b.Background(k.hover)
	}
	b.OnClick(func() {
		if recording {
			a.recordID = ""
			return
		}
		a.recordID, a.recordIndex, a.keyErr = act.id, i, ""
	})
	b.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind == ui.InputScroll {
			return a.stretch(&a.settingsBand, ev) // its handler's, which this one comes before
		}
		if !recording || ev.Kind != ui.InputKeyDown {
			return false
		}
		a.record(act, i, ev.Mods, ev.Key)
		return true
	})
	b.Children(func() {
		if recording {
			ui.Text(c, "Type…").FontSize(12).TextColor(k.accent)
			return
		}
		ui.Text(c, label).FontSize(12.5).FontWeight(600)
	})
}

// record gives an action the key pressed, in place of its i-th or after
// them: Escape leaves it, Delete takes the i-th out. A key that another
// action does where this one works is refused, saying which.
func (a *app) record(act action, i int, mods ui.Modifiers, key ui.Key) {
	keys := slices.Clone(a.cfg.keysOf(act.id))
	a.keyErrID = act.id
	switch {
	case mods == 0 && key == ui.KeyEscape:
		a.recordID, a.keyErr = "", ""
		return
	case mods == 0 && (key == ui.KeyBackspace || key == ui.KeyDelete):
		if act.id == "show" {
			a.keyErr = "Bitwarden Mini needs a shortcut to show it."
			return
		}
		if i < len(keys) {
			keys = slices.Delete(keys, i, i+1)
			a.cfg.setKeys(act.id, keys)
			a.changed()
		}
		a.recordID, a.keyErr = "", ""
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
		if _, named := keyNames[key]; named {
			a.keyErr = "Hold ⌘, ⌃ or ⌥ with that key."
		}
		return
	}
	if other := a.cfg.conflict(act.id, accel); other != "" {
		o, _ := actionByID(other)
		a.keyErr = fmt.Sprintf("%s already does “%s”.", keySymbols(accel), a.actionLabel(o))
		return
	}
	if act.id == "show" {
		if err := a.setShortcut(accel); err != nil {
			a.keyErr = fmt.Sprintf("%s is taken by another app. Try another.", keySymbols(accel))
			return
		}
		keys = []string{accel}
	} else if j := slices.Index(keys, accel); j >= 0 && j != i {
		keys = slices.Delete(keys, i, min(i+1, len(keys))) // it had it already
	} else if i < len(keys) {
		keys[i] = accel
	} else {
		keys = append(keys, accel)
	}
	a.cfg.setKeys(act.id, keys)
	a.changed()
	a.recordID, a.keyErr = "", ""
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
	a.recordID, a.keyErr = "", ""
}

// resetKeys gives every action its keys by default back.
func (a *app) resetKeys() {
	a.cfg.Keys = nil
	a.recordID, a.keyErr = "", ""
	if a.cfg.Shortcut != defaultShortcut {
		if err := a.setShortcut(defaultShortcut); err != nil {
			a.keyErr, a.keyErrID = fmt.Sprintf("%s is taken by another app.", keySymbols(defaultShortcut)), "show"
		} else {
			a.cfg.Shortcut = defaultShortcut
		}
	}
	a.changed()
}
