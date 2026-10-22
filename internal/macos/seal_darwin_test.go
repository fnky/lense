//go:build darwin

package macos

import (
	"errors"
	"testing"
)

func TestSeal(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	s, err := Seal(secret)
	if errors.Is(err, ErrBiometricsUnavailable) {
		t.Skip("no Secure Enclave")
	}
	if err != nil {
		t.Fatal(err)
	}
	if s.priv == 0 || s.data == 0 || cfDataLen(s.data) <= len(secret) {
		t.Fatalf("sealed %+v", s)
	}
	// Opening asks for Touch ID, which a test can't give: forgotten, it
	// can't open at all.
	s.Release()
	if _, err := s.Open("test", "fallback"); !errors.Is(err, ErrSealedGone) {
		t.Fatalf("opened after release: %v", err)
	}
	if _, err := Seal(nil); err == nil {
		t.Fatal("sealed nothing")
	}
}

// Touch ID can allow what opening a sealed secret takes: forbidden to
// ask, LocalAuthentication says it would have to, where it refuses at once
// an operation the key doesn't allow.
func TestSealAsksForWhatOpens(t *testing.T) {
	s, err := Seal([]byte("0123456789abcdef0123456789abcdef"))
	if errors.Is(err, ErrBiometricsUnavailable) {
		t.Skip("no Secure Enclave")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer s.Release()
	withPool(func() { s.ctx.Send(sel("setInteractionNotAllowed:"), true) })
	var ae *AuthError
	if err := s.authorize("test", "fallback"); !errors.As(err, &ae) || ae.Code != laNotInteractive {
		t.Fatalf("authorize: %v", err)
	}
}
