//go:build !darwin

package macos

import (
	"context"
	"errors"
)

// Elsewhere than macOS, nothing of this exists: the app builds, for tests,
// and does without.

func BiometricsAvailable() bool                     { return false }
func Authenticate(reason, fallback string) error    { return ErrBiometricsUnavailable }
func FrontmostApp() App                             { return App{} }
func Activate(pid int) bool                         { return false }
func IsRunning(bundleID string) bool                { return false }
func IsInstalled(bundleID string) bool              { return false }
func OpenApp(bundleID string, urls ...string) error { return errors.ErrUnsupported }
func IsBrowser(bundleID string) bool                { return false }
func Trusted(prompt bool) bool                      { return false }
func FocusedField() FieldKind                       { return FieldNone }
func FocusedFieldOf(pid int) FieldKind              { return FieldNone }
func EnableWebAccessibility(pid int)                {}
func PressKey(code uint16)                          {}
func SelectAll()                                    {}
func OwnPID() int                                   { return 0 }
func TypeText(text string)                          {}
func CopySecret(text string, concealed bool) int    { return -1 }
func ClearIfUnchanged(count int) bool               { return false }
func BrowserURL(ctx context.Context, bundleID string) (string, error) {
	return "", errors.ErrUnsupported
}

const (
	KeyTab    uint16 = 48
	KeyReturn uint16 = 36
)
