//go:build !darwin

package macos

import (
	"context"
	"errors"
	"time"
)

// Elsewhere than macOS, nothing of this exists: the app builds, for tests,
// and does without.

func BiometricsAvailable() bool                          { return false }
func Authenticate(reason, fallback string) error         { return ErrBiometricsUnavailable }
func FrontmostApp() App                                  { return App{} }
func Activate(pid int) bool                              { return false }
func IsRunning(bundleID string) bool                     { return false }
func IsInstalled(bundleID string) bool                   { return false }
func OpenApp(bundleID string, urls ...string) error      { return errors.ErrUnsupported }
func DefaultBrowser() string                             { return "" }
func CountryCode(name string) string                     { return "" }
func CountryName(code string) string                     { return "" }
func LaunchHidden(bundleID string) error                 { return errors.ErrUnsupported }
func IsBrowser(bundleID string) bool                     { return false }
func Trusted(prompt bool) bool                           { return false }
func FocusedFieldOf(pid int) FieldKind                   { return FieldNone }
func EnableWebAccessibility(pid int)                     {}
func PressKey(pid int, code uint16)                      {}
func SelectAll(pid int)                                  {}
func OwnPID() int                                        { return 0 }
func RoundMaterial(nsWindow uintptr, radius float64)     {}
func TypeText(pid int, text string, pause time.Duration) {}
func ModifiersHeld() bool                                { return false }
func FocusedElement(pid int) *Element                    { return nil }
func PageLoaded(pid int) (loaded, known bool)            { return false, false }
func (e *Element) Same(o *Element) bool                  { return false }
func (e *Element) Kind() FieldKind                       { return FieldNone }
func (e *Element) Value() string                         { return "" }
func (e *Element) Length() (int, bool)                   { return 0, false }
func (e *Element) Selected() (int, bool)                 { return 0, false }
func (e *Element) Info() FieldInfo                       { return FieldInfo{} }
func Fields(pid int, near *Element) []*Element           { return nil }
func (e *Element) SetValue(text string) bool             { return false }
func (e *Element) PageURL() string                       { return "" }
func (e *Element) Focus() bool                           { return false }
func OpenMenu(pid int, of *Element) *Menu                { return nil }
func (m *Menu) Items() []string                          { return nil }
func (m *Menu) Choose(i int) bool                        { return false }
func (m *Menu) Close() bool                              { return false }
func CopySecret(text string, concealed bool) int         { return -1 }
func ClearIfUnchanged(count int) bool                    { return false }
func ScrollPhase() (phase, momentum Phase)               { return 0, 0 }
func BarMaterial(nsWindow uintptr, height, radius float64, material int) uintptr {
	return 0
}
func ShowBar(view uintptr, show bool)             {}
func SecureInput(on bool)                         {}
func DenyDebuggers() error                        { return nil }
func AllowCapture(nsWindow uintptr, allowed bool) {}

type Sealed struct{}

func Seal(secret []byte) (*Sealed, error)                      { return nil, ErrBiometricsUnavailable }
func (s *Sealed) Open(reason, fallback string) ([]byte, error) { return nil, ErrSealedGone }
func (s *Sealed) Release()                                     {}
func BrowserURL(ctx context.Context, bundleID string) (string, error) {
	return "", errors.ErrUnsupported
}

const (
	MaterialTitlebar   = 3
	MaterialHeaderView = 10
	MaterialHUDWindow  = 13
	MaterialContent    = 18
)

const (
	KeyTab    uint16 = 48
	KeyReturn uint16 = 36
	KeySpace  uint16 = 49
)
