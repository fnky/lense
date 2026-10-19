//go:build darwin

package macos

import (
	"github.com/ebitengine/purego/objc"
)

// Policies of LocalAuthentication.
const (
	policyBiometrics        = 1 // LAPolicyDeviceOwnerAuthenticationWithBiometrics
	policyBiometricsOrWatch = 4 // LAPolicyDeviceOwnerAuthenticationWithBiometricsOrWatch
)

// laError turns an LAError code into one of this package's errors.
func laError(code int) error {
	switch code {
	case -2, -4, -9: // user, system or app cancel
		return ErrCanceled
	case -3:
		return ErrFallback
	case -6, -7, -5: // not available, not enrolled, no password
		return ErrBiometricsUnavailable
	case -8:
		return ErrBiometricsLockout
	case -1:
		return ErrAuthFailed
	}
	return ErrAuthFailed
}

// policy returns the policy this Mac can evaluate, or an error.
func policy(ctx objc.ID) (int, error) {
	var nsErr objc.ID
	for _, p := range []int{policyBiometricsOrWatch, policyBiometrics} {
		nsErr = 0
		if objc.Send[bool](ctx, sel("canEvaluatePolicy:error:"), p, &nsErr) {
			return p, nil
		}
	}
	if nsErr != 0 {
		return 0, laError(objc.Send[int](nsErr, sel("code")))
	}
	return 0, ErrBiometricsUnavailable
}

// BiometricsAvailable reports whether Touch ID, or an Apple Watch, can
// authenticate the user now.
func BiometricsAvailable() bool {
	if load() != nil {
		return false
	}
	ok := false
	withPool(func() {
		ctx := class("LAContext").Send(sel("alloc")).Send(sel("init"))
		defer ctx.Send(sel("release"))
		_, err := policy(ctx)
		ok = err == nil
	})
	return ok
}

// Authenticate asks the user for Touch ID, saying why, and waits for the
// answer: nil when they passed, ErrCanceled when they canceled, and
// ErrFallback when they chose fallback, the master password.
func Authenticate(reason, fallback string) error {
	if err := load(); err != nil {
		return ErrBiometricsUnavailable
	}
	done := make(chan error, 1)
	var block objc.Block
	var ctx objc.ID
	var setupErr error
	withPool(func() {
		ctx = class("LAContext").Send(sel("alloc")).Send(sel("init"))
		p, err := policy(ctx)
		if err != nil {
			setupErr = err
			return
		}
		ctx.Send(sel("setLocalizedFallbackTitle:"), nsString(fallback))
		block = objc.NewBlock(func(_ objc.Block, success bool, nsErr objc.ID) {
			if success {
				done <- nil
				return
			}
			code := -1
			if nsErr != 0 {
				code = objc.Send[int](nsErr, sel("code"))
			}
			done <- laError(code)
		})
		ctx.Send(sel("evaluatePolicy:localizedReason:reply:"), p, nsString(reason), block)
	})
	if setupErr != nil {
		withPool(func() { ctx.Send(sel("release")) })
		return setupErr
	}
	err := <-done
	withPool(func() { ctx.Send(sel("release")) })
	block.Release()
	return err
}
