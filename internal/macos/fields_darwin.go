//go:build darwin

package macos

import (
	"runtime"
	"strings"
	"unsafe"

	"github.com/ebitengine/purego/objc"
)

// element wraps a retained reference, which it releases as it goes.
func element(ref uintptr) *Element {
	if ref == 0 {
		return nil
	}
	e := &Element{ref}
	runtime.SetFinalizer(e, func(e *Element) { cfRelease(e.ref) })
	return e
}

// FocusedElement returns the element with the keyboard focus in the app
// of pid, or nil.
func FocusedElement(pid int) *Element {
	if load() != nil || pid <= 0 {
		return nil
	}
	var e *Element
	withPool(func() {
		if app := axCreateApplication(int32(pid)); app != 0 {
			e = element(copyAttr(app, "AXFocusedUIElement"))
			cfRelease(app)
		}
	})
	return e
}

// Same reports whether e and o are the same element.
func (e *Element) Same(o *Element) bool {
	return e != nil && o != nil && cfEqual(e.ref, o.ref)
}

// Kind returns the kind of field e is.
func (e *Element) Kind() FieldKind {
	if e == nil || load() != nil {
		return FieldNone
	}
	k := FieldNone
	withPool(func() { k = kindOf(e.ref) })
	return k
}

// Value returns the text e holds; a password field keeps its own.
func (e *Element) Value() string {
	if e == nil || load() != nil {
		return ""
	}
	v := ""
	withPool(func() { v = stringAttr(e.ref, "AXValue") })
	return v
}

// Length returns how many characters e holds, where the app tells.
func (e *Element) Length() (n int, ok bool) {
	if e == nil || load() != nil {
		return 0, false
	}
	withPool(func() {
		v := copyAttr(e.ref, "AXNumberOfCharacters")
		if v == 0 {
			return
		}
		defer cfRelease(v)
		if objc.Send[bool](objc.ID(v), sel("isKindOfClass:"), class("NSNumber")) {
			n, ok = objc.Send[int](objc.ID(v), sel("integerValue")), true
		}
	})
	return n, ok
}

// Selected returns how many characters of what e holds are selected,
// where the app tells.
func (e *Element) Selected() (n int, ok bool) {
	if e == nil || load() != nil {
		return 0, false
	}
	withPool(func() {
		v := copyAttr(e.ref, "AXSelectedTextRange")
		if v == 0 {
			return
		}
		defer cfRelease(v)
		var r struct{ Location, Length int64 }
		if axValueGetValue(v, 4, unsafe.Pointer(&r)) { // kAXValueCFRangeType
			n, ok = int(r.Length), true
		}
	})
	return n, ok
}

// Hint returns what names e, in lower case: its identifier in the page,
// its placeholder, title and description, as "login_field".
func (e *Element) Hint() string {
	if e == nil || load() != nil {
		return ""
	}
	var parts []string
	withPool(func() {
		for _, a := range []string{"AXDOMIdentifier", "AXPlaceholderValue", "AXTitle", "AXDescription"} {
			if s := stringAttr(e.ref, a); s != "" {
				parts = append(parts, s)
			}
		}
	})
	return strings.ToLower(strings.Join(parts, " "))
}

// OnPage reports whether e is on a web page, rather than in the app's own
// controls, as a browser's address field.
func (e *Element) OnPage() bool { return e.pages() > 0 }

// InFrame reports whether e is on a page inside another, in a frame.
func (e *Element) InFrame() bool { return e.pages() > 1 }

// pages counts the web pages e is on: the page of a frame, and the
// page the frame is on.
func (e *Element) pages() int {
	if e == nil || load() != nil {
		return 0
	}
	n := 0
	withPool(func() {
		cur := cfRetain(e.ref)
		for i := 0; cur != 0 && i < 64; i++ {
			if stringAttr(cur, "AXRole") == "AXWebArea" {
				n++
			}
			parent := copyAttr(cur, "AXParent")
			cfRelease(cur)
			cur = parent
		}
		if cur != 0 {
			cfRelease(cur)
		}
	})
	return n
}

// PageURL returns the address of the web page e is on, or "": of the
// frame, for a field in one.
func (e *Element) PageURL() string {
	if e == nil || load() != nil {
		return ""
	}
	u := ""
	withPool(func() {
		web := pageOf(e.ref)
		if web == 0 {
			return
		}
		defer cfRelease(web)
		v := copyAttr(web, "AXURL")
		if v == 0 {
			return
		}
		defer cfRelease(v)
		switch {
		case objc.Send[bool](objc.ID(v), sel("isKindOfClass:"), class("NSURL")):
			u = goString(objc.ID(v).Send(sel("absoluteString")))
		case objc.Send[bool](objc.ID(v), sel("isKindOfClass:"), class("NSString")):
			u = goString(objc.ID(v))
		}
	})
	return u
}

// Focus gives e the keyboard focus, reporting whether the app took the
// request.
func (e *Element) Focus() bool {
	if e == nil || load() != nil {
		return false
	}
	ok := false
	withPool(func() {
		yes := class("NSNumber").Send(sel("numberWithBool:"), true)
		ok = axSetAttributeValue(e.ref, uintptr(nsString("AXFocused")), uintptr(yes)) == 0
	})
	return ok
}

// TextFields returns the text and password fields of the page near is
// on, or without it, of the page the app's focused window shows, in the
// page's order: those too small to see are left out.
func TextFields(pid int, near *Element) []*Element {
	if load() != nil || pid <= 0 {
		return nil
	}
	var out []*Element
	withPool(func() {
		web := uintptr(0)
		if near != nil {
			web = pageOf(near.ref)
		}
		if web == 0 {
			web = shownPage(pid)
		}
		if web == 0 {
			return
		}
		defer cfRelease(web)
		refs, ok := searchTextFields(web)
		if !ok {
			refs = walkTextFields(web)
		}
		for _, r := range refs {
			if seen(r) {
				out = append(out, element(r))
			} else {
				cfRelease(r)
			}
		}
	})
	return out
}

// PageLoaded reports whether the page the app's focused window shows has
// loaded, and whether the app tells.
func PageLoaded(pid int) (loaded, known bool) {
	if load() != nil || pid <= 0 {
		return false, false
	}
	withPool(func() {
		web := shownPage(pid)
		if web == 0 {
			return
		}
		defer cfRelease(web)
		v := copyAttr(web, "AXLoaded")
		if v == 0 {
			return
		}
		defer cfRelease(v)
		if objc.Send[bool](objc.ID(v), sel("isKindOfClass:"), class("NSNumber")) {
			loaded, known = objc.Send[bool](objc.ID(v), sel("boolValue")), true
		}
	})
	return loaded, known
}

// stringAttr returns an attribute of an element that is text, or "".
func stringAttr(el uintptr, name string) string {
	v := copyAttr(el, name)
	if v == 0 {
		return ""
	}
	defer cfRelease(v)
	if !objc.Send[bool](objc.ID(v), sel("isKindOfClass:"), class("NSString")) {
		return ""
	}
	return goString(objc.ID(v))
}

// children returns the children of an element, retained.
func children(el uintptr) []uintptr {
	arr := copyAttr(el, "AXChildren")
	if arr == 0 {
		return nil
	}
	defer cfRelease(arr)
	return retainedItems(arr)
}

// retainedItems returns the items of an array of elements, retained.
func retainedItems(arr uintptr) []uintptr {
	if !objc.Send[bool](objc.ID(arr), sel("isKindOfClass:"), class("NSArray")) {
		return nil
	}
	n := objc.Send[uint](objc.ID(arr), sel("count"))
	out := make([]uintptr, 0, n)
	for i := uint(0); i < n; i++ {
		out = append(out, cfRetain(uintptr(objc.ID(arr).Send(sel("objectAtIndex:"), i))))
	}
	return out
}

// pageOf returns the web page an element is on, retained, or 0.
func pageOf(el uintptr) uintptr {
	cur := cfRetain(el)
	for i := 0; cur != 0 && i < 64; i++ {
		if stringAttr(cur, "AXRole") == "AXWebArea" {
			return cur
		}
		parent := copyAttr(cur, "AXParent")
		cfRelease(cur)
		cur = parent
	}
	if cur != 0 {
		cfRelease(cur)
	}
	return 0
}

// shownPage returns the web page the app's focused window shows,
// retained, or 0: the page of the element with the focus, else the
// first a few levels into the window.
func shownPage(pid int) uintptr {
	app := axCreateApplication(int32(pid))
	if app == 0 {
		return 0
	}
	defer cfRelease(app)
	if el := copyAttr(app, "AXFocusedUIElement"); el != 0 {
		web := pageOf(el)
		cfRelease(el)
		if web != 0 {
			return web
		}
	}
	win := copyAttr(app, "AXFocusedWindow")
	if win == 0 {
		return 0
	}
	queue := []uintptr{win}
	defer func() {
		for _, el := range queue {
			cfRelease(el)
		}
	}()
	for n := 0; len(queue) > 0 && n < 2000; n++ {
		el := queue[0]
		queue = queue[1:]
		if stringAttr(el, "AXRole") == "AXWebArea" {
			return el
		}
		queue = append(queue, children(el)...)
		cfRelease(el)
	}
	return 0
}

// searchTextFields asks a page for its text fields, as VoiceOver's
// rotor does, in the page's order; ok is false for an app that doesn't
// take the search.
func searchTextFields(web uintptr) (fields []uintptr, ok bool) {
	pred := class("NSMutableDictionary").Send(sel("dictionary"))
	set := func(key string, v objc.ID) { pred.Send(sel("setObject:forKey:"), v, nsString(key)) }
	set("AXSearchKey", nsString("AXTextFieldSearchKey"))
	set("AXDirection", nsString("AXDirectionNext"))
	set("AXResultsLimit", class("NSNumber").Send(sel("numberWithInt:"), int32(200)))
	set("AXVisibleOnly", class("NSNumber").Send(sel("numberWithBool:"), false))
	set("AXImmediateDescendantsOnly", class("NSNumber").Send(sel("numberWithBool:"), false))
	var out uintptr
	if axCopyParameterizedValue(web, uintptr(nsString("AXUIElementsForSearchPredicate")), uintptr(pred), &out) != 0 || out == 0 {
		return nil, false
	}
	defer cfRelease(out)
	return retainedItems(out), true
}

// walkTextFields finds the text fields of a page through its elements,
// in the page's order, as far as the first few thousand.
func walkTextFields(web uintptr) []uintptr {
	var out []uintptr
	n := 0
	var walk func(el uintptr)
	walk = func(el uintptr) {
		if n++; n > 5000 {
			return
		}
		if r := stringAttr(el, "AXRole"); r == "AXTextField" || r == "AXComboBox" {
			out = append(out, cfRetain(el))
			return
		}
		for _, c := range children(el) {
			walk(c)
			cfRelease(c)
		}
	}
	walk(web)
	return out
}

// seen reports whether an element is large enough to see: a page keeps
// fields of no size for scripts, as a password field before its turn.
func seen(el uintptr) bool {
	v := copyAttr(el, "AXSize")
	if v == 0 {
		return true
	}
	defer cfRelease(v)
	var size struct{ W, H float64 }
	if !axValueGetValue(v, 2, unsafe.Pointer(&size)) { // kAXValueCGSizeType
		return true
	}
	return size.W >= 2 && size.H >= 2
}
