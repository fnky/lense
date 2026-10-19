package bw

import (
	"errors"

	"github.com/awnumar/memguard"
)

// ErrLocked means the vault locked: the keys that open its secrets are
// gone.
var ErrLocked = errors.New("bw: the vault is locked")

// Secret is a value of an item that stays encrypted in memory, as
// Bitwarden stores it, until it is opened, to show, copy or fill it: a
// password, a code's key, a card's number, a note. The vault's keys open
// it, which memguard keeps encrypted in memory in turn.
type Secret struct {
	enc *EncString
	key *cipherKey
}

// cipherKey is how the secrets of an item decrypt: with the key of its
// vault, the user's or an organization's, or with a key of its own, which
// that key encrypts.
type cipherKey struct {
	v   *Vault
	org string
	own *EncString
}

// Empty reports whether the item has no such value.
func (s Secret) Empty() bool { return s.enc == nil }

// Open decrypts the secret. Wipe what it returns once used.
func (s Secret) Open() ([]byte, error) {
	if s.enc == nil {
		return nil, nil
	}
	return s.key.v.decrypt(s.key.org, s.key.own, *s.enc)
}

// Reveal decrypts the secret into a string, for what shows it, or takes
// it as one: unlike what Open returns, the string can't be wiped.
func (s Secret) Reveal() (string, error) {
	b, err := s.Open()
	defer clear(b)
	return string(b), err
}

// sealKey keeps k in an enclave of memguard: encrypted in memory, with a
// key that memory locked out of swap and guarded holds, and re-keys. k is
// left as it was.
func sealKey(k SymmetricKey) *memguard.Enclave {
	return memguard.NewEnclave(k.Bytes()) // which wipes the copy
}

// withKey opens the key of the organization's vault, or of the user's
// without one, for fn, in memory locked and guarded, which it wipes after.
func (v *Vault) withKey(org string, fn func(SymmetricKey) error) error {
	v.mu.RLock()
	e := v.userKey
	if org != "" {
		e = v.orgKeys[org]
	}
	locked := v.userKey == nil
	v.mu.RUnlock()
	switch {
	case locked:
		return ErrLocked
	case e == nil:
		return ErrNoOrganizationKey
	}
	buf, err := e.Open()
	if err != nil {
		return ErrLocked
	}
	defer buf.Destroy()
	b := buf.Bytes()
	k := SymmetricKey{Enc: b[:32]}
	if len(b) == 64 {
		k.MAC = b[32:]
	}
	return fn(k)
}

// decrypt decrypts e with the key of the organization's vault, or of the
// user's, or with the item's own key, which that key encrypts.
func (v *Vault) decrypt(org string, own *EncString, e EncString) ([]byte, error) {
	var out []byte
	err := v.withKey(org, func(k SymmetricKey) error {
		if own != nil {
			b, err := k.Decrypt(*own)
			if err != nil {
				return err
			}
			ik, err := NewSymmetricKey(b)
			clear(b)
			if err != nil {
				return err
			}
			defer ik.Wipe()
			k = ik
		}
		var err error
		out, err = k.Decrypt(e)
		return err
	})
	return out, err
}

// Forget destroys the key that memguard keeps the vaults' keys with, and
// the memory it locked: with no vault unlocked, as it stops its re-keying
// until a vault unlocks again. The keys of a vault still unlocked would
// open no more.
func Forget() { memguard.Purge() }
