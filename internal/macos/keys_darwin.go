//go:build darwin

package macos

import (
	"time"
	"unicode/utf16"

	"github.com/ebitengine/purego/objc"
)

// Trusted reports whether the user lets this app control the computer
// (System Settings → Privacy & Security → Accessibility), which typing
// into other apps needs. With prompt, macOS asks them to, once.
func Trusted(prompt bool) bool {
	if load() != nil {
		return false
	}
	trusted := false
	withPool(func() {
		var opts objc.ID
		if prompt && axTrustedPromptKey != 0 {
			yes := class("NSNumber").Send(sel("numberWithBool:"), true)
			opts = class("NSDictionary").Send(sel("dictionaryWithObject:forKey:"), yes, objc.ID(axTrustedPromptKey))
		}
		trusted = axIsProcessTrustedWithOptions(uintptr(opts))
	})
	return trusted
}

// copyAttr returns the value of an attribute of an accessibility element,
// retained, or 0.
func copyAttr(el uintptr, name string) uintptr {
	var out uintptr
	if axCopyAttributeValue(el, uintptr(nsString(name)), &out) != 0 {
		return 0
	}
	return out
}

// FocusedField returns the kind of the element with the keyboard focus in
// the frontmost app: FieldPassword, FieldText, or FieldNone for anything
// else, as a button or a page's body.
func FocusedField() FieldKind {
	if load() != nil {
		return FieldNone
	}
	kind := FieldNone
	withPool(func() {
		if sys := axCreateSystemWide(); sys != 0 {
			kind = focusedKind(sys)
			cfRelease(sys)
		}
	})
	return kind
}

// FocusedFieldOf returns the kind of the element that has the keyboard
// focus in an app, or would have as it comes to the front.
func FocusedFieldOf(pid int) FieldKind {
	if load() != nil || pid <= 0 {
		return FieldNone
	}
	kind := FieldNone
	withPool(func() {
		if app := axCreateApplication(int32(pid)); app != 0 {
			kind = focusedKind(app)
			cfRelease(app)
		}
	})
	return kind
}

// focusedKind returns the kind of the focused element of an accessibility
// element: the system's, or an app's.
func focusedKind(of uintptr) FieldKind {
	el := copyAttr(of, "AXFocusedUIElement")
	if el == 0 {
		return FieldNone
	}
	defer cfRelease(el)
	role, subrole := "", ""
	if r := copyAttr(el, "AXRole"); r != 0 {
		role = goString(objc.ID(r))
		cfRelease(r)
	}
	if r := copyAttr(el, "AXSubrole"); r != 0 {
		subrole = goString(objc.ID(r))
		cfRelease(r)
	}
	// Login forms have fields of one line: a search field, or a text area
	// as a terminal's or an editor's, is no place for a username.
	switch {
	case subrole == "AXSecureTextField" || role == "AXSecureTextField":
		return FieldPassword
	case subrole == "AXSearchField":
		return FieldNone
	case role == "AXTextField" || role == "AXComboBox":
		return FieldText
	}
	return FieldNone
}

// EnableWebAccessibility asks a Chromium browser or an Electron app to
// tell assistive technology about its pages, which it does only when
// asked, so that FocusedField sees their fields.
func EnableWebAccessibility(pid int) {
	if load() != nil || pid <= 0 {
		return
	}
	withPool(func() {
		app := axCreateApplication(int32(pid))
		if app == 0 {
			return
		}
		defer cfRelease(app)
		yes := class("NSNumber").Send(sel("numberWithBool:"), true)
		axSetAttributeValue(app, uintptr(nsString("AXManualAccessibility")), uintptr(yes))
		axSetAttributeValue(app, uintptr(nsString("AXEnhancedUserInterface")), uintptr(yes))
	})
}

// Virtual key codes of the keys Fill presses.
const (
	KeyTab    uint16 = 48
	KeyReturn uint16 = 36
)

// PressKey presses and releases a key, without modifiers.
func PressKey(code uint16) { press(code, 0) }

// SelectAll presses Command-A, which selects what the focused field holds,
// for typing over it.
func SelectAll() { press(0, 1<<20) } // kVK_ANSI_A, kCGEventFlagMaskCommand

func press(code uint16, flags uint64) {
	if load() != nil {
		return
	}
	src := cgEventSourceCreate(1) // kCGEventSourceStateHIDSystemState
	if src != 0 {
		defer cfRelease(src)
	}
	for _, down := range []bool{true, false} {
		ev := cgEventCreateKeyboardEvent(src, code, down)
		if ev == 0 {
			return
		}
		cgEventSetFlags(ev, flags)
		cgEventPost(0, ev) // kCGHIDEventTap
		cfRelease(ev)
	}
	time.Sleep(12 * time.Millisecond)
}

// TypeText types text into the frontmost app, as the keyboard would,
// whatever its layout.
func TypeText(text string) {
	if load() != nil {
		return
	}
	src := cgEventSourceCreate(1)
	if src != 0 {
		defer cfRelease(src)
	}
	for _, r := range text {
		units := utf16.Encode([]rune{r})
		for _, down := range []bool{true, false} {
			ev := cgEventCreateKeyboardEvent(src, 0, down)
			if ev == 0 {
				return
			}
			cgEventSetFlags(ev, 0)
			cgEventKeyboardSetUnicodeString(ev, uint64(len(units)), &units[0])
			cgEventPost(0, ev)
			cfRelease(ev)
		}
		// Some fields drop keys that come faster than people type.
		time.Sleep(4 * time.Millisecond)
	}
}
