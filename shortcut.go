package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// keyNames name the keys a global shortcut may end with, as accelerators
// write them.
var keyNames = func() map[ui.Key]string {
	m := map[ui.Key]string{
		ui.KeySpace: "Space", ui.KeyEnter: "Return", ui.KeyTab: "Tab", ui.KeyBackspace: "Backspace",
		ui.KeyDelete: "Delete", ui.KeyHome: "Home", ui.KeyEnd: "End", ui.KeyPageUp: "PageUp", ui.KeyPageDown: "PageDown",
		ui.KeyUp: "Up", ui.KeyDown: "Down", ui.KeyLeft: "Left", ui.KeyRight: "Right",
		ui.KeyComma: ",", ui.KeyPeriod: ".", ui.KeySlash: "/", ui.KeySemicolon: ";", ui.KeyQuote: "'",
		ui.KeyBracketLeft: "[", ui.KeyBracketRight: "]", ui.KeyBackslash: "\\", ui.KeyBackquote: "`",
		ui.KeyMinus: "-", ui.KeyEqual: "=",
	}
	for i := range 26 {
		m[ui.KeyA+ui.Key(i)] = string(rune('A' + i))
	}
	for i := range 10 {
		m[ui.Key0+ui.Key(i)] = string(rune('0' + i))
	}
	for i, k := range []ui.Key{ui.KeyF1, ui.KeyF2, ui.KeyF3, ui.KeyF4, ui.KeyF5, ui.KeyF6, ui.KeyF7, ui.KeyF8, ui.KeyF9, ui.KeyF10, ui.KeyF11, ui.KeyF12} {
		m[k] = "F" + string(rune('1'+i))
		if i >= 9 {
			m[k] = "F1" + string(rune('0'+i-9))
		}
	}
	return m
}()

// accelerator writes a key pressed with modifiers as a global shortcut:
// it needs Command, Control or Option, but for the function keys.
func accelerator(mods ui.Modifiers, key ui.Key) (string, bool) {
	name, ok := keyNames[key]
	if !ok {
		return "", false
	}
	fkey := len(name) > 1 && name[0] == 'F' && name[1] >= '0' && name[1] <= '9'
	if mods&(ui.Super|ui.Ctrl|ui.Alt) == 0 && !fkey {
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

// keySymbols shows a shortcut as macOS menus do: ⌃⌥⇧⌘Space.
func keySymbols(accel string) string {
	parts := strings.Split(accel, "+")
	if strings.HasSuffix(accel, "++") {
		parts = append(parts[:len(parts)-2], "+")
	}
	var mods, key string
	has := map[string]bool{}
	for i, p := range parts {
		if i == len(parts)-1 {
			key = p
			break
		}
		switch strings.ToLower(p) {
		case "ctrl", "control":
			has["⌃"] = true
		case "alt", "option":
			has["⌥"] = true
		case "shift":
			has["⇧"] = true
		case "cmd", "command", "super", "cmdorctrl", "commandorcontrol":
			has["⌘"] = true
		}
	}
	for _, s := range []string{"⌃", "⌥", "⇧", "⌘"} {
		if has[s] {
			mods += s
		}
	}
	switch strings.ToLower(key) {
	case "return", "enter":
		key = "↩"
	case "up":
		key = "↑"
	case "down":
		key = "↓"
	case "left":
		key = "←"
	case "right":
		key = "→"
	case "tab":
		key = "⇥"
	case "backspace":
		key = "⌫"
	case "escape", "esc":
		key = "esc"
	case "delete":
		key = "⌦"
	case "pageup":
		key = "⇞"
	case "pagedown":
		key = "⇟"
	case "home":
		key = "↖"
	case "end":
		key = "↘"
	}
	return mods + key
}
