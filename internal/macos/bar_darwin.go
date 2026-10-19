//go:build darwin

package macos

import "github.com/ebitengine/purego/objc"

// NSVisualEffectView's materials for bars.
const (
	MaterialTitlebar   = 3
	MaterialHeaderView = 10
	MaterialHUDWindow  = 13
	MaterialContent    = 18
)

// BarMaterial lays AppKit's material over the bottom of a window's
// content, height points tall: the content under it blurs and tints as
// under a bar of a window of macOS's own, blended within the window, and
// its bottom corners round by radius as the window's. It returns the view,
// which ShowBar shows and hides; an existing one is reused. Call it on the
// main thread.
func BarMaterial(nsWindow uintptr, height, radius float64, material int) uintptr {
	if load() != nil || nsWindow == 0 {
		return 0
	}
	var v objc.ID
	withPool(func() {
		w := objc.ID(nsWindow)
		content := w.Send(sel("contentView"))
		b := objc.Send[nsRect](content, sel("bounds"))
		v = objc.ID(class("NSVisualEffectView")).Send(sel("alloc")).Send(sel("initWithFrame:"), nsRect{W: b.W, H: height})
		v.Send(sel("setBlendingMode:"), 1) // NSVisualEffectBlendingModeWithinWindow
		v.Send(sel("setMaterial:"), material)
		v.Send(sel("setState:"), 1)               // active, as the panel never is key's window's
		v.Send(sel("setAutoresizingMask:"), 2|32) // width sizable, top margin flexible
		v.Send(sel("setWantsLayer:"), true)
		layer := v.Send(sel("layer"))
		layer.Send(sel("setCornerRadius:"), radius)
		layer.Send(sel("setCornerCurve:"), nsString("continuous"))
		layer.Send(sel("setMaskedCorners:"), 1|2) // the bottom ones: min Y
		layer.Send(sel("setMasksToBounds:"), true)
		content.Send(sel("addSubview:positioned:relativeTo:"), v, 1, 0) // NSWindowAbove
	})
	return uintptr(v)
}

type nsRect struct{ X, Y, W, H float64 }
