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
	e := &Element{ref: ref}
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

// SetValue puts text into e at once, as accessibility sets a value,
// reporting whether the app took the request.
func (e *Element) SetValue(text string) bool {
	if e == nil || load() != nil {
		return false
	}
	ok := false
	withPool(func() {
		ok = axSetAttributeValue(e.ref, uintptr(nsString("AXValue")), uintptr(nsString(text))) == 0
	})
	return ok
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

// boolAttr returns an attribute of an element that is true or false, or
// otherwise for one it doesn't tell.
func boolAttr(el uintptr, name string, otherwise bool) bool {
	v := copyAttr(el, name)
	if v == 0 {
		return otherwise
	}
	defer cfRelease(v)
	if !objc.Send[bool](objc.ID(v), sel("isKindOfClass:"), class("NSNumber")) {
		return otherwise
	}
	return objc.Send[bool](objc.ID(v), sel("boolValue"))
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

// topPageOf returns the page an element is on, retained, or 0: for one in
// a frame, the page the frame is on, as far as the browser's tab.
func topPageOf(el uintptr) uintptr {
	top := uintptr(0)
	cur := cfRetain(el)
	for i := 0; cur != 0 && i < 64; i++ {
		if stringAttr(cur, "AXRole") == "AXWebArea" {
			if top != 0 {
				cfRelease(top)
			}
			top = cfRetain(cur)
		}
		parent := copyAttr(cur, "AXParent")
		cfRelease(cur)
		cur = parent
	}
	if cur != 0 {
		cfRelease(cur)
	}
	return top
}

// shownPage returns the web page the app's focused window shows,
// retained, or 0: the page of the element with the focus, that its frame
// is on for one in a frame, else the first a few levels into the window.
func shownPage(pid int) uintptr {
	app := axCreateApplication(int32(pid))
	if app == 0 {
		return 0
	}
	defer cfRelease(app)
	if el := copyAttr(app, "AXFocusedUIElement"); el != 0 {
		web := topPageOf(el)
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

// Fields returns the fields of the page near is on, or without it, of the
// page the app's focused window shows, in the page's order, with what
// each tells (Info): its text fields, menus and boxes, and those of its
// frames.
func Fields(pid int, near *Element) []*Element {
	if load() != nil || pid <= 0 {
		return nil
	}
	var out []*Element
	withPool(func() {
		app := axCreateApplication(int32(pid))
		if app == 0 {
			return
		}
		defer cfRelease(app)
		web := uintptr(0)
		if near != nil {
			web = topPageOf(near.ref)
		}
		if web == 0 {
			web = shownPage(pid)
		}
		if web == 0 {
			return
		}
		defer cfRelease(web)
		page := frameOf(web)
		n := 0
		var walk func(el uintptr, framed bool)
		walk = func(el uintptr, framed bool) {
			if n++; n > 5000 {
				return
			}
			if typ := fieldType(el); typ != "" {
				e := element(cfRetain(el))
				info := infoOf(el, app, page)
				info.OnPage, info.InFrame = true, framed
				e.info = &info
				out = append(out, e)
				return
			}
			// A page within the page is a frame's.
			framed = framed || el != web && stringAttr(el, "AXRole") == "AXWebArea"
			for _, c := range children(el) {
				walk(c, framed)
				cfRelease(c)
			}
		}
		walk(web, false)
	})
	return out
}

// Info returns what e tells of itself as a field, as Fields found it, or
// now.
func (e *Element) Info() FieldInfo {
	if e == nil || load() != nil {
		return FieldInfo{}
	}
	if e.info != nil {
		return *e.info
	}
	var info FieldInfo
	withPool(func() {
		app := axCreateApplication(int32(e.pid()))
		if app != 0 {
			defer cfRelease(app)
		}
		var page rect
		if web := pageOf(e.ref); web != 0 {
			page = frameOf(web)
			cfRelease(web)
		}
		info = infoOf(e.ref, app, page)
	})
	n := e.pages()
	info.OnPage, info.InFrame = n > 0, n > 1
	return info
}

// pid returns the process of the app e is of.
func (e *Element) pid() int {
	var pid int32
	axGetPid(e.ref, &pid)
	return int(pid)
}

type rect struct{ X, Y, W, H float64 }

func frameOf(el uintptr) rect {
	var r rect
	if v := copyAttr(el, "AXPosition"); v != 0 {
		var p struct{ X, Y float64 }
		if axValueGetValue(v, 1, unsafe.Pointer(&p)) { // kAXValueCGPointType
			r.X, r.Y = p.X, p.Y
		}
		cfRelease(v)
	}
	if v := copyAttr(el, "AXSize"); v != 0 {
		var s struct{ W, H float64 }
		if axValueGetValue(v, 2, unsafe.Pointer(&s)) { // kAXValueCGSizeType
			r.W, r.H = s.W, s.H
		}
		cfRelease(v)
	}
	return r
}

// infoOf reads what a field tells, on the page of frame page, in the app
// of app.
func infoOf(el, app uintptr, page rect) FieldInfo {
	info := FieldInfo{
		Type:        fieldType(el),
		ID:          stringAttr(el, "AXDOMIdentifier"),
		Classes:     strings.Join(stringsAttr(el, "AXDOMClassList"), " "),
		Label:       stringAttr(el, "AXTitle"),
		Placeholder: stringAttr(el, "AXPlaceholderValue"),
		AriaLabel:   stringAttr(el, "AXDescription"),
		Help:        stringAttr(el, "AXHelp"),
		Value:       stringAttr(el, "AXValue"),
		Enabled:     boolAttr(el, "AXEnabled", true),
	}
	axIsAttributeSettable(el, uintptr(nsString("AXValue")), &info.Settable)
	info.Form, info.FormHint = formOf(el)
	info.Viewable = viewable(el, app, page)
	return info
}

// fieldType returns the type of a field, as an input's, or "" for an
// element that is no field.
func fieldType(el uintptr) string {
	role := stringAttr(el, "AXRole")
	switch role {
	case "AXTextField", "AXComboBox":
		switch stringAttr(el, "AXSubrole") {
		case "AXSecureTextField":
			return "password"
		case "AXSearchField":
			return "search"
		}
		// The kind of input shows in how its role is described: "email",
		// "telephone", "secure text field".
		switch d := strings.ToLower(stringAttr(el, "AXRoleDescription")); {
		case strings.Contains(d, "email"):
			return "email"
		case strings.Contains(d, "phone"):
			return "tel"
		case strings.Contains(d, "url"), strings.Contains(d, "web address"):
			return "url"
		}
		return "text"
	case "AXTextArea":
		return "textarea"
	case "AXPopUpButton":
		return "select"
	case "AXIncrementor":
		return "number"
	case "AXDateField", "AXTimeField":
		return "date"
	case "AXCheckBox":
		return "checkbox"
	case "AXRadioButton":
		return "radio"
	}
	return ""
}

// formOf returns the form el is in, as its hash, and the form's
// identifier and classes; 0 outside any.
func formOf(el uintptr) (uint64, string) {
	cur := copyAttr(el, "AXParent")
	for i := 0; cur != 0 && i < 32; i++ {
		role := stringAttr(cur, "AXRole")
		if role == "AXWebArea" {
			break
		}
		if role == "AXGroup" && (stringAttr(cur, "AXRoleDescription") == "form" || stringAttr(cur, "AXSubrole") == "AXLandmarkForm") {
			hash := cfHash(cur)
			hint := strings.TrimSpace(stringAttr(cur, "AXDOMIdentifier") + " " + strings.Join(stringsAttr(cur, "AXDOMClassList"), " "))
			cfRelease(cur)
			return hash, hint
		}
		parent := copyAttr(cur, "AXParent")
		cfRelease(cur)
		cur = parent
	}
	if cur != 0 {
		cfRelease(cur)
	}
	return 0, ""
}

// viewable reports whether the user can see a field, as Bitwarden has it:
// at least 10 points square; its middle on the page, beside none of it,
// as a field put out of sight at left: -9999px, nor far above it; and on
// screen, nothing over its middle but itself or its label, where the app
// tells what is. A page scrolled away from it keeps it viewable.
func viewable(el, app uintptr, page rect) bool {
	r := frameOf(el)
	if r.W < 10 || r.H < 10 {
		return false
	}
	if page.W == 0 || app == 0 {
		return true
	}
	x, y := r.X+r.W/2, r.Y+r.H/2
	if x < page.X || x > page.X+page.W || y < page.Y-page.H {
		return false
	}
	if y > page.Y+page.H {
		return true // below, where the page scrolls to
	}
	var hit uintptr
	if axCopyElementAtPosition(app, float32(x), float32(y), &hit) != 0 || hit == 0 {
		return true
	}
	defer cfRelease(hit)
	// Chromium finds what is at a point in the background, and tells the
	// page itself till it has: that tells nothing.
	if stringAttr(hit, "AXRole") == "AXWebArea" {
		return true
	}
	label := copyAttr(el, "AXTitleUIElement")
	if label != 0 {
		defer cfRelease(label)
	}
	// What is at its middle may be part of it, as its text, or of its
	// label.
	cur := cfRetain(hit)
	defer func() {
		if cur != 0 {
			cfRelease(cur)
		}
	}()
	for i := 0; cur != 0 && i < 4; i++ {
		if cfEqual(cur, el) || label != 0 && cfEqual(cur, label) {
			return true
		}
		parent := copyAttr(cur, "AXParent")
		cfRelease(cur)
		cur = parent
	}
	return false
}

// stringsAttr returns an attribute of an element that is a list of text.
func stringsAttr(el uintptr, name string) []string {
	v := copyAttr(el, name)
	if v == 0 {
		return nil
	}
	defer cfRelease(v)
	if !objc.Send[bool](objc.ID(v), sel("isKindOfClass:"), class("NSArray")) {
		return nil
	}
	var out []string
	for i := uint(0); i < objc.Send[uint](objc.ID(v), sel("count")); i++ {
		if it := objc.ID(v).Send(sel("objectAtIndex:"), i); objc.Send[bool](it, sel("isKindOfClass:"), class("NSString")) {
			out = append(out, goString(it))
		}
	}
	return out
}
