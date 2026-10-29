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
	return kindOf(el)
}

// kindOf returns the kind of an accessibility element: FieldPassword,
// FieldText, or FieldNone for anything else, as a button or a page's
// body.
func kindOf(el uintptr) FieldKind {
	role, subrole := stringAttr(el, "AXRole"), stringAttr(el, "AXSubrole")
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
	KeySpace  uint16 = 49
)

// The keys typed come from a source of their own, apart from the keys the
// user holds (kCGEventSourceStatePrivate), and go to the app of pid
// alone: none lands in another that comes to the front meanwhile.
const privateSource = -1

// PressKey presses and releases a key, without modifiers, in the app of
// pid.
func PressKey(pid int, code uint16) { press(pid, code, 0) }

// SelectAll presses Command-A in the app of pid, which selects what its
// focused field holds, for typing over it.
func SelectAll(pid int) { press(pid, 0, 1<<20) } // kVK_ANSI_A, kCGEventFlagMaskCommand

func press(pid int, code uint16, flags uint64) {
	if load() != nil || pid <= 0 {
		return
	}
	src := cgEventSourceCreate(privateSource)
	if src != 0 {
		defer cfRelease(src)
	}
	for _, down := range []bool{true, false} {
		ev := cgEventCreateKeyboardEvent(src, code, down)
		if ev == 0 {
			return
		}
		cgEventSetFlags(ev, flags)
		cgEventPostToPid(int32(pid), ev)
		cfRelease(ev)
	}
	time.Sleep(12 * time.Millisecond)
}

// TypeText types text into the app of pid, as the keyboard would,
// whatever its layout, pausing between the characters.
func TypeText(pid int, text string, pause time.Duration) {
	if load() != nil || pid <= 0 {
		return
	}
	src := cgEventSourceCreate(privateSource)
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
			cgEventPostToPid(int32(pid), ev)
			cfRelease(ev)
		}
		time.Sleep(pause)
	}
}

// ModifiersHeld reports whether the user holds Shift, Control, Option or
// Command down.
func ModifiersHeld() bool {
	if load() != nil {
		return false
	}
	const mods = 1<<17 | 1<<18 | 1<<19 | 1<<20  // kCGEventFlagMaskShift … Command
	return cgEventSourceFlagsState(1)&mods != 0 // kCGEventSourceStateHIDSystemState
}
