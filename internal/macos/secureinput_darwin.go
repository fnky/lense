//go:build darwin

package macos

import (
	"sync"

	"github.com/ebitengine/purego"
)

var (
	secureOnce    sync.Once
	secureErr     error
	enableSecure  func() int32
	disableSecure func() int32

	secureMu sync.Mutex
	secureOn bool
)

// SecureInput turns Secure Event Input on or off, as macOS's own password
// fields do while they have the keyboard: other apps, which may watch the
// keys typed, see none meanwhile. It turns it on once, however often it is
// asked to, as macOS counts the times.
func SecureInput(on bool) {
	secureOnce.Do(func() {
		carbon, err := lib("/System/Library/Frameworks/Carbon.framework/Carbon")
		if err != nil {
			secureErr = err
			return
		}
		purego.RegisterLibFunc(&enableSecure, carbon, "EnableSecureEventInput")
		purego.RegisterLibFunc(&disableSecure, carbon, "DisableSecureEventInput")
	})
	if secureErr != nil {
		return
	}
	secureMu.Lock()
	defer secureMu.Unlock()
	if on == secureOn {
		return
	}
	if on {
		enableSecure()
	} else {
		disableSecure()
	}
	secureOn = on
}
