//go:build darwin

package macos

import "github.com/ebitengine/purego/objc"

// CopySecret puts text on the pasteboard, marked concealed so that
// clipboard managers that honor nspasteboard.org's types do not keep it
// in their history, and returns the pasteboard's change count, for
// ClearIfUnchanged.
func CopySecret(text string, concealed bool) int {
	if load() != nil {
		return -1
	}
	count := -1
	withPool(func() {
		pb := class("NSPasteboard").Send(sel("generalPasteboard"))
		pb.Send(sel("clearContents"))
		pb.Send(sel("setString:forType:"), nsString(text), nsString("public.utf8-plain-text"))
		if concealed {
			pb.Send(sel("setString:forType:"), nsString(""), nsString("org.nspasteboard.ConcealedType"))
		}
		count = objc.Send[int](pb, sel("changeCount"))
	})
	return count
}

// ClearIfUnchanged empties the pasteboard if nothing was copied since the
// change count count, reporting whether it did.
func ClearIfUnchanged(count int) bool {
	if load() != nil {
		return false
	}
	cleared := false
	withPool(func() {
		pb := class("NSPasteboard").Send(sel("generalPasteboard"))
		if objc.Send[int](pb, sel("changeCount")) == count {
			pb.Send(sel("clearContents"))
			cleared = true
		}
	})
	return cleared
}
