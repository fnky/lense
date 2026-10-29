package main

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
)

// mode is where the keys of Quick Access act: on the items found, on the
// fields of the item chosen, on the vaults to choose from, or on the ways
// to fill the item chosen.
type mode int

const (
	modeResults mode = iota
	modeDetails
	modeVaults
	modeFillMenu
)

// scope is the modes an action works in.
type scope int

const (
	inGlobal scope = iota // from any app: the shortcut that shows the window
	inWindow              // wherever the window shows, locked too
	inItems               // the results and the details
	inResults
	inDetails
	inVaults
	inFillMenu
)

func (s scope) covers(m mode) bool {
	switch s {
	case inWindow:
		return true
	case inItems:
		return m == modeResults || m == modeDetails
	case inResults:
		return m == modeResults
	case inDetails:
		return m == modeDetails
	case inVaults:
		return m == modeVaults
	case inFillMenu:
		return m == modeFillMenu
	}
	return false
}

// overlaps reports whether two scopes share a mode, where one key can't do
// two things.
func (s scope) overlaps(o scope) bool {
	if s == inGlobal || o == inGlobal {
		return s == o
	}
	for _, m := range []mode{modeResults, modeDetails, modeVaults, modeFillMenu} {
		if s.covers(m) && o.covers(m) {
			return true
		}
	}
	return false
}

// action is something a key does, which the user may bind to other keys.
type action struct {
	id    string
	label string
	group string
	scope scope
	keys  []string // its keys by default
}

// actions are what Quick Access does from the keyboard, by group.
var actions = []action{
	{"show", "Show Lense", "Global", inGlobal, []string{defaultShortcut}},

	{"previous", "Previous item", "Search results", inResults, []string{"Up"}},
	{"next", "Next item", "Search results", inResults, []string{"Down"}},
	{"details", "Go to the item's fields", "Search results", inResults, []string{"Right"}},
	{"default", "Fill, or open a website and fill it", "Search results", inResults, []string{"Return"}},
	{"complete", "Finish the filter", "Search results", inResults, []string{"Tab"}},

	{"field-previous", "Previous field", "Item details", inDetails, []string{"Up"}},
	{"field-next", "Next field", "Item details", inDetails, []string{"Down"}},
	{"field-reveal", "Reveal, or show in Large Type", "Item details", inDetails, []string{"Right"}},
	{"field-back", "Back to the results", "Item details", inDetails, []string{"Left"}},
	{"field-copy", "Copy the field, and close", "Item details", inDetails, []string{"Return"}},

	{"vault-previous", "Previous vault", "Vault selector", inVaults, []string{"Up"}},
	{"vault-next", "Next vault", "Vault selector", inVaults, []string{"Down"}},
	{"vault-expand", "Show its collections", "Vault selector", inVaults, []string{"Right"}},
	{"vault-collapse", "Hide them, or go back", "Vault selector", inVaults, []string{"Left"}},
	{"vault-choose", "Search the vault", "Vault selector", inVaults, []string{"Return"}},

	{"menu-previous", "Previous choice", "Open and Fill menu", inFillMenu, []string{"Up"}},
	{"menu-next", "Next choice", "Open and Fill menu", inFillMenu, []string{"Down"}},
	{"menu-choose", "Open it and fill, or fill", "Open and Fill menu", inFillMenu, []string{"Return"}},

	{"copy-username", "Copy the username", "Items", inItems, []string{"Cmd+C", "Cmd+B"}},
	{"copy-password", "Copy the password", "Items", inItems, []string{"Shift+Cmd+C"}},
	{"copy-code", "Copy the one-time password", "Items", inItems, []string{"Option+Cmd+C"}},
	{"open-fill", "Open a website, and fill it", "Items", inItems, []string{"Shift+Return"}},
	{"open-window", "Open the item in a window", "Items", inItems, []string{"Cmd+O"}},
	{"open-bitwarden", "Open Bitwarden", "Items", inItems, []string{"Shift+Cmd+O"}},
	{"choose-vault", "Choose the vault", "Items", inItems, []string{"Cmd+K"}},
	{"vault-1", "All Vaults", "Items", inItems, []string{"Cmd+1"}},
	{"vault-2", "Vault 2", "Items", inItems, []string{"Cmd+2"}},
	{"vault-3", "Vault 3", "Items", inItems, []string{"Cmd+3"}},
	{"vault-4", "Vault 4", "Items", inItems, []string{"Cmd+4"}},
	{"vault-5", "Vault 5", "Items", inItems, []string{"Cmd+5"}},
	{"vault-6", "Vault 6", "Items", inItems, []string{"Cmd+6"}},
	{"vault-7", "Vault 7", "Items", inItems, []string{"Cmd+7"}},
	{"vault-8", "Vault 8", "Items", inItems, []string{"Cmd+8"}},
	{"vault-9", "Vault 9", "Items", inItems, []string{"Cmd+9"}},
	{"search", "Search", "Items", inItems, []string{"Cmd+F"}},
	{"lock", "Lock", "Items", inItems, []string{"Cmd+L"}},

	{"close", "Clear the search, or close", "Window", inWindow, []string{"Escape"}},
	{"hide", "Close the window", "Window", inWindow, []string{"Cmd+W"}},
	{"settings", "Settings", "Window", inWindow, []string{"Cmd+,"}},
}

func actionByID(id string) (action, bool) {
	for _, a := range actions {
		if a.id == id {
			return a, true
		}
	}
	return action{}, false
}

// binding is a key and the modifiers held with it.
type binding struct {
	mods ui.Modifiers
	key  ui.Key
}

// keyByName looks keys up by the names accelerators give them.
var keyByName = func() map[string]ui.Key {
	m := map[string]ui.Key{"escape": ui.KeyEscape, "esc": ui.KeyEscape, "enter": ui.KeyEnter}
	for k, name := range keyNames {
		m[strings.ToLower(name)] = k
	}
	return m
}()

// parseBinding reads an accelerator, as "Shift+Cmd+C" or "Return".
func parseBinding(s string) (binding, bool) {
	parts := strings.Split(s, "+")
	if strings.HasSuffix(s, "++") {
		parts = append(parts[:len(parts)-2], "+")
	}
	var b binding
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if i < len(parts)-1 {
			switch strings.ToLower(p) {
			case "cmd", "command", "super", "cmdorctrl", "commandorcontrol":
				b.mods |= ui.Super
			case "ctrl", "control":
				b.mods |= ui.Ctrl
			case "alt", "option":
				b.mods |= ui.Alt
			case "shift":
				b.mods |= ui.Shift
			default:
				return binding{}, false
			}
			continue
		}
		k, ok := keyByName[strings.ToLower(p)]
		if !ok {
			return binding{}, false
		}
		b.key = k
	}
	return b, b.key != 0
}

// keysOf returns the keys of an action: the user's, or its defaults.
func (c *config) keysOf(id string) []string {
	if id == "show" {
		return []string{c.Shortcut}
	}
	if ks, ok := c.Keys[id]; ok {
		return ks
	}
	a, _ := actionByID(id)
	return a.keys
}

// actionFor returns the action that a key does in a mode, or "".
func (c *config) actionFor(m mode, mods ui.Modifiers, key ui.Key) string {
	for _, a := range actions {
		if a.scope == inGlobal || !a.scope.covers(m) {
			continue
		}
		for _, s := range c.keysOf(a.id) {
			if b, ok := parseBinding(s); ok && b.mods == mods && b.key == key {
				return a.id
			}
		}
	}
	return ""
}

// conflict returns the action other than id that accel already does where
// id works, or "".
func (c *config) conflict(id, accel string) string {
	a, _ := actionByID(id)
	want, ok := parseBinding(accel)
	if !ok {
		return ""
	}
	for _, o := range actions {
		if o.id == id || !o.scope.overlaps(a.scope) {
			continue
		}
		for _, s := range c.keysOf(o.id) {
			if b, ok := parseBinding(s); ok && b == want {
				return o.id
			}
		}
	}
	return ""
}

// setKeys gives an action its keys, forgetting them when they are its
// defaults.
func (c *config) setKeys(id string, keys []string) {
	if id == "show" {
		if len(keys) > 0 {
			c.Shortcut = keys[0]
		}
		return
	}
	a, _ := actionByID(id)
	if slices.Equal(keys, a.keys) {
		delete(c.Keys, id)
		return
	}
	if c.Keys == nil {
		c.Keys = map[string][]string{}
	}
	c.Keys[id] = keys
}

// windowAccelerator writes a key pressed in the window as a binding: keys
// that type need Command, Control or Option, the others, as the arrows,
// don't.
func windowAccelerator(mods ui.Modifiers, key ui.Key) (string, bool) {
	name, ok := keyNames[key]
	if key == ui.KeyEscape {
		name, ok = "Escape", true
	}
	if !ok {
		return "", false
	}
	types := len(name) == 1 || key == ui.KeySpace
	if types && mods&(ui.Super|ui.Ctrl|ui.Alt) == 0 {
		return "", false
	}
	var parts []string
	if mods&ui.Ctrl != 0 {
		parts = append(parts, "Ctrl")
	}
	if mods&ui.Alt != 0 {
		parts = append(parts, "Option")
	}
	if mods&ui.Shift != 0 {
		parts = append(parts, "Shift")
	}
	if mods&ui.Super != 0 {
		parts = append(parts, "Cmd")
	}
	return strings.Join(append(parts, name), "+"), true
}

// keyHint shows an action's first key as macOS menus do, or "".
func (c *config) keyHint(id string) string {
	ks := c.keysOf(id)
	if len(ks) == 0 {
		return ""
	}
	return keySymbols(ks[0])
}
