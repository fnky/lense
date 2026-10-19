//go:build darwin

package macos

import "github.com/ebitengine/purego/objc"

// The sharing types of NSWindow.
const (
	nsWindowSharingNone     = 0
	nsWindowSharingReadOnly = 1
)

// AllowCapture lets screenshots and screen recordings show a window, or
// has macOS leave it out of them, as Bitwarden's desktop app does unless
// allowed. Call it on the main thread.
func AllowCapture(nsWindow uintptr, allowed bool) {
	if load() != nil || nsWindow == 0 {
		return
	}
	sharing := nsWindowSharingNone
	if allowed {
		sharing = nsWindowSharingReadOnly
	}
	withPool(func() { objc.ID(nsWindow).Send(sel("setSharingType:"), sharing) })
}
