//go:build darwin

package macos

import "github.com/ebitengine/purego/objc"

// The flags of NSEvent's modifierFlags.
const (
	nsEventModifierShift   = 1 << 17
	nsEventModifierControl = 1 << 18
	nsEventModifierOption  = 1 << 19
	nsEventModifierCommand = 1 << 20
)

// periodKeys is the monitor of CatchCommandPeriod.
var periodKeys objc.ID

// CatchCommandPeriod hands take ⌘. pressed in a window of the app, with
// the window, nsWindow, and the other modifiers held: AppKit makes ⌘. the
// window's key equivalent of Escape, cancelOperation:, which never reaches
// its view as a key. take reports whether it took the key; one it leaves
// goes on to AppKit. Call it once, on the main thread.
func CatchCommandPeriod(take func(nsWindow uintptr, shift, ctrl, alt bool) bool) {
	if load() != nil || periodKeys != 0 {
		return
	}
	const keyDownMask = 1 << 10 // NSEventMaskKeyDown
	blk := objc.NewBlock(func(_ objc.Block, ev objc.ID) objc.ID {
		flags := objc.Send[uint](ev, sel("modifierFlags"))
		if flags&nsEventModifierCommand == 0 || goString(ev.Send(sel("charactersIgnoringModifiers"))) != "." {
			return ev
		}
		win := uintptr(ev.Send(sel("window")))
		if take(win, flags&nsEventModifierShift != 0, flags&nsEventModifierControl != 0, flags&nsEventModifierOption != 0) {
			return 0
		}
		return ev
	})
	withPool(func() {
		periodKeys = class("NSEvent").Send(sel("addLocalMonitorForEventsMatchingMask:handler:"), uint64(keyDownMask), uintptr(blk))
		periodKeys.Send(sel("retain"))
	})
}
