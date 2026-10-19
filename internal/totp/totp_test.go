package totp

import (
	"encoding/base32"
	"testing"
	"time"
)

// The test vectors of RFC 6238, appendix B.
func TestRFC6238(t *testing.T) {
	secrets := map[string]string{
		"SHA1":   "12345678901234567890",
		"SHA256": "12345678901234567890123456789012",
		"SHA512": "1234567890123456789012345678901234567890123456789012345678901234",
	}
	for _, tc := range []struct {
		at   int64
		algo string
		want string
	}{
		{59, "SHA1", "94287082"}, {59, "SHA256", "46119246"}, {59, "SHA512", "90693936"},
		{1111111109, "SHA1", "07081804"}, {1111111109, "SHA256", "68084774"},
		{1234567890, "SHA1", "89005924"}, {2000000000, "SHA512", "38618901"},
	} {
		b32 := base32.StdEncoding.EncodeToString([]byte(secrets[tc.algo]))
		k, err := Parse("otpauth://totp/Example:alice@example.com?secret=" + b32 + "&digits=8&algorithm=" + tc.algo + "&issuer=Example")
		if err != nil {
			t.Fatal(err)
		}
		if got := k.Code(time.Unix(tc.at, 0)); got != tc.want {
			t.Errorf("%s at %d: %s, want %s", tc.algo, tc.at, got, tc.want)
		}
		if k.Issuer != "Example" || k.Account != "alice@example.com" {
			t.Errorf("label %q %q", k.Issuer, k.Account)
		}
	}
}

func TestPlainSecret(t *testing.T) {
	k, err := Parse("jbsw y3dp ehpk 3pxp")
	if err != nil {
		t.Fatal(err)
	}
	if k.Digits != 6 || k.Period != 30 {
		t.Fatalf("%+v", k)
	}
	at := time.Unix(1700000000, 0)
	if c := k.Code(at); len(c) != 6 {
		t.Errorf("code %q", c)
	}
	if r := k.Remaining(at); r != 10*time.Second {
		t.Errorf("remaining %v", r)
	}
	if _, err := Parse("not base32!"); err == nil {
		t.Error("parsed garbage")
	}
}

func TestSteam(t *testing.T) {
	k, err := Parse("steam://JBSWY3DPEHPK3PXP")
	if err != nil || !k.Steam {
		t.Fatal(err)
	}
	c := k.Code(time.Unix(1700000000, 0))
	if len(c) != 5 {
		t.Fatalf("code %q", c)
	}
	for _, r := range c {
		if !containsRune(steamChars, r) {
			t.Errorf("%q has %q", c, r)
		}
	}
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}

func TestFormat(t *testing.T) {
	for in, want := range map[string]string{"123456": "123 456", "12345678": "1234 5678", "ABCDE": "ABCDE"} {
		if got := Format(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}
