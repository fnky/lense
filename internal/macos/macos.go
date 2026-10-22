package macos

import (
	"errors"
	"fmt"
)

// App is a running app.
type App struct {
	PID      int
	BundleID string
	Name     string
}

// FieldKind is the kind of the element with the keyboard focus.
type FieldKind int

const (
	FieldNone FieldKind = iota
	FieldText
	FieldPassword
)

// Phase is where a gesture on the trackpad is, as AppKit's NSEventPhase.
type Phase uint

const (
	PhaseBegan      Phase = 1
	PhaseStationary Phase = 2
	PhaseChanged    Phase = 4
	PhaseEnded      Phase = 8
	PhaseCancelled  Phase = 16
	PhaseMayBegin   Phase = 32
)

var (
	// ErrCanceled means the user, or the system, canceled Touch ID.
	ErrCanceled = errors.New("Touch ID was canceled")
	// ErrFallback means the user chose to enter the master password.
	ErrFallback = errors.New("the master password was chosen")
	// ErrBiometricsUnavailable means the Mac has no Touch ID, or no
	// finger enrolled.
	ErrBiometricsUnavailable = errors.New("Touch ID is not available on this Mac")
	// ErrBiometricsLockout means Touch ID failed too often, and needs the
	// Mac's password before it works again.
	ErrBiometricsLockout = errors.New("Touch ID is locked after too many attempts")
	// ErrAuthFailed means the finger was not recognized.
	ErrAuthFailed = errors.New("Touch ID did not recognize the fingerprint")
	// ErrSealedGone means the key of a sealed secret no longer opens it,
	// as when the fingerprints of Touch ID changed.
	ErrSealedGone = errors.New("the Secure Enclave no longer opens the secret")
)

// AuthError is an answer of LocalAuthentication that none of the errors
// above names: its code, and what it says of it.
type AuthError struct {
	Code   int
	Reason string
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("Touch ID: %s (%d)", e.Reason, e.Code)
}

// BitwardenBundleID is the bundle identifier of the Bitwarden desktop app.
const BitwardenBundleID = "com.bitwarden.desktop"

// AccessibilitySettingsURL opens the pane of System Settings that lets
// apps control the computer.
const AccessibilitySettingsURL = "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"
