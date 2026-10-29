//go:build darwin

// Package macos reaches the parts of macOS that MyGo does not cover: Touch
// ID, the frontmost app and its focused field, typing into it, the
// pasteboard's concealed type and the browser's address. It calls them
// through the Objective-C runtime with purego, without cgo.
package macos

import (
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

var (
	loadOnce sync.Once
	loadErr  error

	poolPush func() uintptr
	poolPop  func(uintptr)

	cfRelease func(uintptr)
	cfRetain  func(uintptr) uintptr
	cfEqual   func(a, b uintptr) bool
	cfHash    func(uintptr) uint64

	// CoreGraphics
	cgEventSourceCreate             func(state int32) uintptr
	cgEventCreateKeyboardEvent      func(src uintptr, key uint16, down bool) uintptr
	cgEventKeyboardSetUnicodeString func(ev uintptr, n uint64, chars *uint16)
	cgEventSetFlags                 func(ev uintptr, flags uint64)
	cgEventPostToPid                func(pid int32, ev uintptr)
	cgEventSourceFlagsState         func(state int32) uint64

	// Accessibility
	axIsProcessTrustedWithOptions func(opts uintptr) bool
	axCreateApplication           func(pid int32) uintptr
	axCopyAttributeValue          func(el uintptr, attr uintptr, out *uintptr) int32
	axIsAttributeSettable         func(el uintptr, attr uintptr, out *bool) int32
	axCopyElementAtPosition       func(app uintptr, x, y float32, out *uintptr) int32
	axGetPid                      func(el uintptr, pid *int32) int32
	axCopyParameterizedValue      func(el uintptr, attr uintptr, param uintptr, out *uintptr) int32
	axSetAttributeValue           func(el uintptr, attr uintptr, value uintptr) int32
	axValueGetValue               func(value uintptr, typ int32, out unsafe.Pointer) bool
	axTrustedPromptKey            uintptr // kAXTrustedCheckOptionPrompt
)

func lib(path string) (uintptr, error) {
	return purego.Dlopen(path, purego.RTLD_GLOBAL|purego.RTLD_NOW)
}

// load opens the frameworks, once.
func load() error {
	loadOnce.Do(func() {
		objcLib, err := lib("/usr/lib/libobjc.A.dylib")
		if err != nil {
			loadErr = err
			return
		}
		for _, p := range []string{
			"/System/Library/Frameworks/Foundation.framework/Foundation",
			"/System/Library/Frameworks/AppKit.framework/AppKit",
			"/System/Library/Frameworks/LocalAuthentication.framework/LocalAuthentication",
		} {
			if _, err := lib(p); err != nil {
				loadErr = err
				return
			}
		}
		cf, err := lib("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation")
		if err != nil {
			loadErr = err
			return
		}
		cg, err := lib("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics")
		if err != nil {
			loadErr = err
			return
		}
		as, err := lib("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices")
		if err != nil {
			loadErr = err
			return
		}
		purego.RegisterLibFunc(&poolPush, objcLib, "objc_autoreleasePoolPush")
		purego.RegisterLibFunc(&poolPop, objcLib, "objc_autoreleasePoolPop")
		purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
		purego.RegisterLibFunc(&cfRetain, cf, "CFRetain")
		purego.RegisterLibFunc(&cfEqual, cf, "CFEqual")
		purego.RegisterLibFunc(&cfHash, cf, "CFHash")
		purego.RegisterLibFunc(&cgEventSourceCreate, cg, "CGEventSourceCreate")
		purego.RegisterLibFunc(&cgEventCreateKeyboardEvent, cg, "CGEventCreateKeyboardEvent")
		purego.RegisterLibFunc(&cgEventKeyboardSetUnicodeString, cg, "CGEventKeyboardSetUnicodeString")
		purego.RegisterLibFunc(&cgEventSetFlags, cg, "CGEventSetFlags")
		purego.RegisterLibFunc(&cgEventPostToPid, cg, "CGEventPostToPid")
		purego.RegisterLibFunc(&cgEventSourceFlagsState, cg, "CGEventSourceFlagsState")
		purego.RegisterLibFunc(&axIsProcessTrustedWithOptions, as, "AXIsProcessTrustedWithOptions")
		purego.RegisterLibFunc(&axCreateApplication, as, "AXUIElementCreateApplication")
		purego.RegisterLibFunc(&axCopyAttributeValue, as, "AXUIElementCopyAttributeValue")
		purego.RegisterLibFunc(&axIsAttributeSettable, as, "AXUIElementIsAttributeSettable")
		purego.RegisterLibFunc(&axCopyElementAtPosition, as, "AXUIElementCopyElementAtPosition")
		purego.RegisterLibFunc(&axGetPid, as, "AXUIElementGetPid")
		purego.RegisterLibFunc(&axCopyParameterizedValue, as, "AXUIElementCopyParameterizedAttributeValue")
		purego.RegisterLibFunc(&axValueGetValue, as, "AXValueGetValue")
		purego.RegisterLibFunc(&axSetAttributeValue, as, "AXUIElementSetAttributeValue")
		if p, err := purego.Dlsym(as, "kAXTrustedCheckOptionPrompt"); err == nil {
			axTrustedPromptKey = *(*uintptr)(cptr(p))
		}
	})
	return loadErr
}

// withPool runs fn in an autorelease pool, on one thread.
func withPool(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	p := poolPush()
	defer poolPop(p)
	fn()
}

var selCache sync.Map

func sel(name string) objc.SEL {
	if s, ok := selCache.Load(name); ok {
		return s.(objc.SEL)
	}
	s := objc.RegisterName(name)
	selCache.Store(name, s)
	return s
}

func class(name string) objc.ID { return objc.ID(objc.GetClass(name)) }

// nsString returns an autoreleased NSString of s.
func nsString(s string) objc.ID {
	return class("NSString").Send(sel("stringWithUTF8String:"), s)
}

// goString copies an NSString.
func goString(s objc.ID) string {
	if s == 0 {
		return ""
	}
	p := objc.Send[uintptr](s, sel("UTF8String"))
	if p == 0 {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(cptr(p), n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(cptr(p)), n))
}

// cptr turns the address of C memory, which Go's collector does not move,
// into a pointer.
func cptr(p uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&p))
}
