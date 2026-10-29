//go:build darwin

package macos

import (
	"runtime"
	"strings"
)

// OpenMenu returns the menu the app of pid shows open of a page's select,
// of, or nil: AppKit's, which a browser opens in its window, over the
// page, of the select's options, as the page tells them as it's open. One
// of other items, which opened as of did, it closes.
func OpenMenu(pid int, of *Element) *Menu {
	if load() != nil || pid <= 0 || of == nil {
		return nil
	}
	var m *Menu
	withPool(func() {
		app := axCreateApplication(int32(pid))
		if app == 0 {
			return
		}
		defer cfRelease(app)
		ref := openMenuIn(app, 0)
		if ref == 0 {
			return
		}
		if options := optionsOf(of.ref); options != nil && !inOrder(options, titlesOf(ref)) {
			axPerformAction(ref, uintptr(nsString("AXCancel")))
			cfRelease(ref)
			return
		}
		m = &Menu{ref: ref}
		runtime.SetFinalizer(m, func(m *Menu) { cfRelease(m.ref) })
	})
	return m
}

// openMenuIn returns the menu open in an element, retained, or 0: a few
// levels into the app's windows, beside its menu bar, whose menus are
// always there, and its pages.
func openMenuIn(el uintptr, depth int) uintptr {
	var found uintptr
	for _, c := range children(el) {
		switch role := stringAttr(c, "AXRole"); {
		case found != 0, role == "AXMenuBar", role == "AXWebArea":
		case role == "AXMenu":
			found = cfRetain(c)
		case depth < 2:
			found = openMenuIn(c, depth+1)
		}
		cfRelease(c)
	}
	return found
}

// optionsOf returns the options of a select as the page tells them as
// it's open, or nil.
func optionsOf(sel uintptr) []string {
	var out []string
	for _, c := range children(sel) {
		if out == nil && stringAttr(c, "AXRole") == "AXMenu" {
			out = titlesOf(c)
		}
		cfRelease(c)
	}
	return out
}

// titlesOf returns the titles of a menu's items, or of the options a page
// tells, which hold them as their values, those of its groups among them;
// "" for one that can't be chosen, as a separator or a group's title.
func titlesOf(menu uintptr) []string {
	out := []string{}
	for _, c := range children(menu) {
		if stringAttr(c, "AXRole") == "AXGroup" {
			out = append(out, titlesOf(c)...)
			cfRelease(c)
			continue
		}
		t := stringAttr(c, "AXTitle")
		if t == "" {
			t = stringAttr(c, "AXValue")
		}
		if !boolAttr(c, "AXEnabled", true) {
			t = ""
		}
		out = append(out, strings.TrimSpace(t))
		cfRelease(c)
	}
	return out
}

// inOrder reports whether all has each of some, but those of "", in their
// order: AppKit's menu has the titles of the select's groups too.
func inOrder(some, all []string) bool {
	i := 0
	for _, s := range some {
		if s == "" {
			continue
		}
		for i < len(all) && all[i] != s {
			i++
		}
		if i == len(all) {
			return false
		}
		i++
	}
	return true
}

// Items returns the titles of the menu's items, in their order; "" for
// one that can't be chosen.
func (m *Menu) Items() []string {
	if m == nil || load() != nil {
		return nil
	}
	var out []string
	withPool(func() { out = titlesOf(m.ref) })
	return out
}

// Choose chooses the item at i, as a click would, which closes the menu.
func (m *Menu) Choose(i int) bool {
	if m == nil || load() != nil {
		return false
	}
	ok := false
	withPool(func() {
		items := children(m.ref)
		if i >= 0 && i < len(items) {
			ok = axPerformAction(items[i], uintptr(nsString("AXPress"))) == 0
		}
		for _, c := range items {
			cfRelease(c)
		}
	})
	return ok
}

// Close closes the menu, choosing nothing.
func (m *Menu) Close() bool {
	if m == nil || load() != nil {
		return false
	}
	ok := false
	withPool(func() { ok = axPerformAction(m.ref, uintptr(nsString("AXCancel"))) == 0 })
	return ok
}
