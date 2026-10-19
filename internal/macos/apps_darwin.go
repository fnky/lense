//go:build darwin

package macos

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	"github.com/ebitengine/purego/objc"
)

// FrontmostApp returns the app the user is in.
func FrontmostApp() App {
	if load() != nil {
		return App{}
	}
	var a App
	withPool(func() {
		ws := class("NSWorkspace").Send(sel("sharedWorkspace"))
		ra := ws.Send(sel("frontmostApplication"))
		if ra == 0 {
			return
		}
		a = App{
			PID:      int(objc.Send[int32](ra, sel("processIdentifier"))),
			BundleID: goString(ra.Send(sel("bundleIdentifier"))),
			Name:     goString(ra.Send(sel("localizedName"))),
		}
	})
	return a
}

// Activate brings the app of pid to the front.
func Activate(pid int) bool {
	if load() != nil || pid <= 0 {
		return false
	}
	ok := false
	withPool(func() {
		ra := class("NSRunningApplication").Send(sel("runningApplicationWithProcessIdentifier:"), int32(pid))
		if ra == 0 {
			return
		}
		// NSApplicationActivateAllWindows; macOS 14 ignores the old
		// "ignoring other apps" option.
		ok = objc.Send[bool](ra, sel("activateWithOptions:"), uint(1))
	})
	return ok
}

// IsRunning reports whether an app with the bundle identifier runs.
func IsRunning(bundleID string) bool {
	if load() != nil {
		return false
	}
	running := false
	withPool(func() {
		apps := class("NSRunningApplication").Send(sel("runningApplicationsWithBundleIdentifier:"), nsString(bundleID))
		running = apps != 0 && objc.Send[uint](apps, sel("count")) > 0
	})
	return running
}

// IsInstalled reports whether macOS knows an app with the bundle
// identifier.
func IsInstalled(bundleID string) bool {
	if load() != nil {
		return false
	}
	found := false
	withPool(func() {
		ws := class("NSWorkspace").Send(sel("sharedWorkspace"))
		found = ws.Send(sel("URLForApplicationWithBundleIdentifier:"), nsString(bundleID)) != 0
	})
	return found
}

// OpenApp opens the app with the bundle identifier, and the URLs given,
// with it.
func OpenApp(bundleID string, urls ...string) error {
	args := append([]string{"-b", bundleID}, urls...)
	out, err := exec.Command("/usr/bin/open", args...).CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return errors.New(msg)
		}
		return err
	}
	return nil
}

// browsers are the browsers whose address AppleScript tells, and how.
var browsers = map[string]string{
	"com.apple.Safari":                    "safari",
	"com.apple.SafariTechnologyPreview":   "safari",
	"com.google.Chrome":                   "chrome",
	"com.google.Chrome.beta":              "chrome",
	"com.google.Chrome.dev":               "chrome",
	"com.google.Chrome.canary":            "chrome",
	"org.chromium.Chromium":               "chrome",
	"com.brave.Browser":                   "chrome",
	"com.brave.Browser.beta":              "chrome",
	"com.microsoft.edgemac":               "chrome",
	"com.microsoft.edgemac.Beta":          "chrome",
	"com.microsoft.edgemac.Dev":           "chrome",
	"com.vivaldi.Vivaldi":                 "chrome",
	"com.operasoftware.Opera":             "chrome",
	"company.thebrowser.Browser":          "chrome",
	"company.thebrowser.dia":              "chrome",
	"org.mozilla.firefox":                 "",
	"org.mozilla.firefoxdeveloperedition": "",
	"app.zen-browser.zen":                 "",
	"com.kagi.kagimacOS":                  "safari",
}

// IsBrowser reports whether the app of bundleID is a web browser.
func IsBrowser(bundleID string) bool {
	_, ok := browsers[bundleID]
	return ok
}

// BrowserURL returns the address of the page the browser shows, asking
// it with AppleScript: macOS asks the user, once, to let Bitwarden Mini
// do so. Firefox does not tell.
func BrowserURL(ctx context.Context, bundleID string) (string, error) {
	kind, ok := browsers[bundleID]
	if !ok {
		return "", errors.New("macos: not a browser")
	}
	var script string
	switch kind {
	case "safari":
		script = `tell application id "` + bundleID + `" to get URL of front document`
	case "chrome":
		script = `tell application id "` + bundleID + `" to get URL of active tab of front window`
	default:
		return "", errors.New("macos: the browser does not tell its address")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", script).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// OwnPID is this app's process.
func OwnPID() int {
	if load() != nil {
		return 0
	}
	pid := 0
	withPool(func() {
		pid = int(objc.Send[int32](class("NSProcessInfo").Send(sel("processInfo")), sel("processIdentifier")))
	})
	return pid
}

// RoundMaterial rounds the corners of the material behind a window's
// content (the NSVisualEffectView of MyGo's vibrancy) as its content's, by
// radius points, continuously as macOS rounds its own, and has the window
// draw its shadow by that shape. Call it on the main thread.
func RoundMaterial(nsWindow uintptr, radius float64) {
	if load() != nil || nsWindow == 0 {
		return
	}
	withPool(func() {
		w := objc.ID(nsWindow)
		effect := class("NSVisualEffectView")
		var find func(v objc.ID, depth int) objc.ID
		find = func(v objc.ID, depth int) objc.ID {
			if v == 0 || depth > 3 {
				return 0
			}
			// The window's, behind its content, not a bar's (BarMaterial).
			if objc.Send[bool](v, sel("isKindOfClass:"), effect) && objc.Send[int](v, sel("blendingMode")) == 0 {
				return v
			}
			subs := v.Send(sel("subviews"))
			for i := range objc.Send[uint](subs, sel("count")) {
				if f := find(subs.Send(sel("objectAtIndex:"), i), depth+1); f != 0 {
					return f
				}
			}
			return 0
		}
		ev := find(w.Send(sel("contentView")), 0)
		if ev == 0 {
			return
		}
		ev.Send(sel("setWantsLayer:"), true)
		layer := ev.Send(sel("layer"))
		layer.Send(sel("setCornerRadius:"), radius)
		layer.Send(sel("setCornerCurve:"), nsString("continuous"))
		layer.Send(sel("setMasksToBounds:"), true)
		w.Send(sel("invalidateShadow"))
	})
}
