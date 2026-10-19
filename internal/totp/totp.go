// Package totp generates the one-time passwords of logins, from what
// Bitwarden stores: a secret in base32, an otpauth:// URI, or a Steam
// Guard secret as steam://.
package totp

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Key is how codes are generated.
type Key struct {
	Secret    []byte
	Digits    int
	Period    int // seconds
	Algorithm string
	Steam     bool
	Issuer    string
	Account   string
}

// ErrInvalid is a TOTP that is no secret, URI or Steam secret.
var ErrInvalid = errors.New("totp: not a valid secret")

// Parse reads what a login stores for its one-time passwords.
func Parse(s string) (Key, error) {
	s = strings.TrimSpace(s)
	k := Key{Digits: 6, Period: 30, Algorithm: "SHA1"}
	switch {
	case strings.HasPrefix(strings.ToLower(s), "otpauth://"):
		u, err := url.Parse(s)
		if err != nil {
			return k, ErrInvalid
		}
		q := u.Query()
		secret := q.Get("secret")
		if d, err := strconv.Atoi(q.Get("digits")); err == nil && d >= 1 && d <= 10 {
			k.Digits = d
		}
		if p, err := strconv.Atoi(q.Get("period")); err == nil && p > 0 {
			k.Period = p
		}
		if a := strings.ToUpper(q.Get("algorithm")); a == "SHA256" || a == "SHA512" {
			k.Algorithm = a
		}
		label := strings.TrimPrefix(u.Path, "/")
		if issuer, account, ok := strings.Cut(label, ":"); ok {
			k.Issuer, k.Account = issuer, strings.TrimSpace(account)
		} else {
			k.Account = label
		}
		if i := q.Get("issuer"); i != "" {
			k.Issuer = i
		}
		if strings.EqualFold(u.Host, "steam") || strings.EqualFold(k.Issuer, "Steam") && q.Get("encoder") == "steam" {
			k.Steam, k.Digits = true, 5
		}
		s = secret
	case strings.HasPrefix(strings.ToLower(s), "steam://"):
		k.Steam, k.Digits = true, 5
		s = s[len("steam://"):]
	}
	secret, err := decodeBase32(s)
	if err != nil || len(secret) == 0 {
		return k, ErrInvalid
	}
	k.Secret = secret
	return k, nil
}

func decodeBase32(s string) ([]byte, error) {
	s = strings.ToUpper(strings.NewReplacer(" ", "", "-", "", "\t", "").Replace(s))
	s = strings.TrimRight(s, "=")
	return base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s)
}

const steamChars = "23456789BCDFGHJKMNPQRTVWXY"

// Code returns the code at t.
func (k Key) Code(t time.Time) string {
	var h func() hash.Hash
	switch k.Algorithm {
	case "SHA256":
		h = sha256.New
	case "SHA512":
		h = sha512.New
	default:
		h = sha1.New
	}
	period := k.Period
	if period <= 0 {
		period = 30
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(t.Unix()/int64(period)))
	m := hmac.New(h, k.Secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	if k.Steam {
		var b strings.Builder
		for range 5 {
			b.WriteByte(steamChars[bin%uint32(len(steamChars))])
			bin /= uint32(len(steamChars))
		}
		return b.String()
	}
	mod := uint64(1)
	for range k.Digits {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", k.Digits, uint64(bin)%mod)
}

// Remaining returns how long the code at t stays valid.
func (k Key) Remaining(t time.Time) time.Duration {
	period := int64(k.Period)
	if period <= 0 {
		period = 30
	}
	left := period - t.Unix()%period
	return time.Duration(left)*time.Second - time.Duration(t.Nanosecond())
}

// Format spaces a code in halves for reading: 123 456.
func Format(code string) string {
	if len(code) < 6 || len(code)%2 == 1 && len(code) != 7 {
		return code
	}
	h := len(code) / 2
	return code[:h] + " " + code[h:]
}
