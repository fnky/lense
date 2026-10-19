// Package bw reads the vault the Bitwarden desktop app keeps on disk, and
// decrypts it with the keys Bitwarden uses: the master key derived from the
// master password, the user key it protects, and the keys of organizations.
package bw

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// EncType is the kind of an encrypted string, the number before its dot.
type EncType int

const (
	AesCbc256B64                   EncType = 0
	AesCbc128HmacSha256B64         EncType = 1
	AesCbc256HmacSha256B64         EncType = 2
	Rsa2048OaepSha256B64           EncType = 3
	Rsa2048OaepSha1B64             EncType = 4
	Rsa2048OaepSha256HmacSha256B64 EncType = 5
	Rsa2048OaepSha1HmacSha256B64   EncType = 6
	CoseEncrypt0                   EncType = 7
)

var (
	// ErrMAC is a string whose MAC does not match: encrypted with
	// another key, or a key derived from a wrong password.
	ErrMAC = errors.New("bw: the MAC does not match")
	// ErrUnsupported is an encryption this package cannot decrypt.
	ErrUnsupported = errors.New("bw: unsupported encryption")
	// ErrMalformed is a string that is not an encrypted string.
	ErrMalformed = errors.New("bw: malformed encrypted string")
)

// EncString is an encrypted string as Bitwarden writes it:
// "2.<iv>|<data>|<mac>" in base64, or "4.<data>" for RSA.
type EncString struct {
	Type EncType
	IV   []byte
	Data []byte
	MAC  []byte
}

// ParseEncString parses s. A string without a type is of type 0, or type 2
// when it has three parts, as Bitwarden's oldest strings were.
func ParseEncString(s string) (EncString, error) {
	var e EncString
	head, rest, ok := strings.Cut(s, ".")
	if ok {
		t, err := strconv.Atoi(head)
		if err != nil {
			return e, ErrMalformed
		}
		e.Type = EncType(t)
	} else {
		rest = s
		e.Type = AesCbc256B64
		if strings.Count(s, "|") == 2 {
			e.Type = AesCbc256HmacSha256B64
		}
	}
	parts := strings.Split(rest, "|")
	dec := func(i int) ([]byte, error) {
		b, err := base64.StdEncoding.DecodeString(parts[i])
		if err != nil {
			return nil, ErrMalformed
		}
		return b, nil
	}
	var err error
	switch e.Type {
	case AesCbc256B64:
		if len(parts) != 2 {
			return e, ErrMalformed
		}
		if e.IV, err = dec(0); err != nil {
			return e, err
		}
		e.Data, err = dec(1)
	case AesCbc128HmacSha256B64, AesCbc256HmacSha256B64:
		if len(parts) != 3 {
			return e, ErrMalformed
		}
		if e.IV, err = dec(0); err != nil {
			return e, err
		}
		if e.Data, err = dec(1); err != nil {
			return e, err
		}
		e.MAC, err = dec(2)
	case Rsa2048OaepSha256B64, Rsa2048OaepSha1B64:
		if len(parts) != 1 {
			return e, ErrMalformed
		}
		e.Data, err = dec(0)
	case Rsa2048OaepSha256HmacSha256B64, Rsa2048OaepSha1HmacSha256B64:
		if len(parts) != 2 {
			return e, ErrMalformed
		}
		if e.Data, err = dec(0); err != nil {
			return e, err
		}
		e.MAC, err = dec(1)
	default:
		return e, fmt.Errorf("%w: type %d", ErrUnsupported, e.Type)
	}
	return e, err
}

// String writes e as Bitwarden does.
func (e EncString) String() string {
	b64 := base64.StdEncoding.EncodeToString
	switch e.Type {
	case AesCbc256B64:
		return "0." + b64(e.IV) + "|" + b64(e.Data)
	case AesCbc128HmacSha256B64, AesCbc256HmacSha256B64:
		return strconv.Itoa(int(e.Type)) + "." + b64(e.IV) + "|" + b64(e.Data) + "|" + b64(e.MAC)
	case Rsa2048OaepSha256HmacSha256B64, Rsa2048OaepSha1HmacSha256B64:
		return strconv.Itoa(int(e.Type)) + "." + b64(e.Data) + "|" + b64(e.MAC)
	default:
		return strconv.Itoa(int(e.Type)) + "." + b64(e.Data)
	}
}

// SymmetricKey is a key of AES-256 in CBC mode with a key of HMAC-SHA256
// authenticating what it encrypts, 64 bytes in all; the oldest keys, of 32
// bytes, have no MAC key.
type SymmetricKey struct {
	Enc []byte
	MAC []byte
}

// NewSymmetricKey splits a key of 64 bytes, or takes one of 32.
func NewSymmetricKey(b []byte) (SymmetricKey, error) {
	switch len(b) {
	case 64:
		return SymmetricKey{Enc: bytes.Clone(b[:32]), MAC: bytes.Clone(b[32:])}, nil
	case 32:
		return SymmetricKey{Enc: bytes.Clone(b)}, nil
	}
	return SymmetricKey{}, fmt.Errorf("bw: a key of %d bytes", len(b))
}

// GenerateSymmetricKey returns a new random key of 64 bytes.
func GenerateSymmetricKey() SymmetricKey {
	b := make([]byte, 64)
	_, _ = rand.Read(b)
	k, _ := NewSymmetricKey(b)
	return k
}

// Bytes returns the key as Bitwarden stores it: the encryption key, then
// the MAC key.
func (k SymmetricKey) Bytes() []byte {
	return append(bytes.Clone(k.Enc), k.MAC...)
}

// Valid reports whether k holds a key.
func (k SymmetricKey) Valid() bool { return len(k.Enc) == 32 }

// Wipe overwrites the key in memory.
func (k SymmetricKey) Wipe() {
	clear(k.Enc)
	clear(k.MAC)
}

// verify checks that k encrypted e, by its MAC, without decrypting it.
func (k SymmetricKey) verify(e EncString) error {
	if !k.Valid() {
		return errors.New("bw: no key")
	}
	switch e.Type {
	case AesCbc256B64:
		if len(k.MAC) != 0 {
			// A key with a MAC key never decrypts strings without a MAC,
			// which an attacker could otherwise forge.
			return ErrMAC
		}
	case AesCbc256HmacSha256B64:
		if len(k.MAC) == 0 {
			return ErrMAC
		}
		m := hmac.New(sha256.New, k.MAC)
		m.Write(e.IV)
		m.Write(e.Data)
		if subtle.ConstantTimeCompare(m.Sum(nil), e.MAC) != 1 {
			return ErrMAC
		}
	default:
		return fmt.Errorf("%w: type %d with a symmetric key", ErrUnsupported, e.Type)
	}
	return nil
}

// Decrypt decrypts an AES string with k.
func (k SymmetricKey) Decrypt(e EncString) ([]byte, error) {
	if err := k.verify(e); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(k.Enc)
	if err != nil {
		return nil, err
	}
	if len(e.IV) != aes.BlockSize || len(e.Data) == 0 || len(e.Data)%aes.BlockSize != 0 {
		return nil, ErrMalformed
	}
	out := make([]byte, len(e.Data))
	cipher.NewCBCDecrypter(block, e.IV).CryptBlocks(out, e.Data)
	return unpad(out)
}

// DecryptString parses and decrypts s.
func (k SymmetricKey) DecryptString(s string) (string, error) {
	e, err := ParseEncString(s)
	if err != nil {
		return "", err
	}
	b, err := k.Decrypt(e)
	return string(b), err
}

// Encrypt encrypts b with k, as a string of type 2.
func (k SymmetricKey) Encrypt(b []byte) (EncString, error) {
	if !k.Valid() || len(k.MAC) == 0 {
		return EncString{}, errors.New("bw: no key to encrypt with")
	}
	block, err := aes.NewCipher(k.Enc)
	if err != nil {
		return EncString{}, err
	}
	iv := make([]byte, aes.BlockSize)
	_, _ = rand.Read(iv)
	data := pad(b)
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(data, data)
	m := hmac.New(sha256.New, k.MAC)
	m.Write(iv)
	m.Write(data)
	return EncString{Type: AesCbc256HmacSha256B64, IV: iv, Data: data, MAC: m.Sum(nil)}, nil
}

// EncryptString encrypts s, and writes it as Bitwarden does.
func (k SymmetricKey) EncryptString(s string) (string, error) {
	e, err := k.Encrypt([]byte(s))
	if err != nil {
		return "", err
	}
	return e.String(), nil
}

func pad(b []byte) []byte {
	n := aes.BlockSize - len(b)%aes.BlockSize
	out := make([]byte, len(b)+n)
	copy(out, b)
	for i := len(b); i < len(out); i++ {
		out[i] = byte(n)
	}
	return out
}

func unpad(b []byte) ([]byte, error) {
	if len(b) == 0 {
		return nil, ErrMalformed
	}
	n := int(b[len(b)-1])
	if n == 0 || n > aes.BlockSize || n > len(b) {
		return nil, ErrMalformed
	}
	for _, c := range b[len(b)-n:] {
		if int(c) != n {
			return nil, ErrMalformed
		}
	}
	return b[:len(b)-n], nil
}

// KDF is how a master key is derived from a master password.
type KDF struct {
	Type        int `json:"kdfType"` // 0: PBKDF2-SHA256, 1: Argon2id
	Iterations  int `json:"iterations"`
	Memory      int `json:"memory,omitempty"` // MiB, for Argon2id
	Parallelism int `json:"parallelism,omitempty"`
}

const (
	KDFPBKDF2   = 0
	KDFArgon2id = 1
)

// MasterKey derives the master key of an account from its master password
// and its salt, its email address in lower case.
func MasterKey(password, salt string, kdf KDF) ([]byte, error) {
	salt = strings.ToLower(strings.TrimSpace(salt))
	switch kdf.Type {
	case KDFPBKDF2:
		if kdf.Iterations < 1 {
			return nil, errors.New("bw: PBKDF2 without iterations")
		}
		return pbkdf2.Key(sha256.New, password, []byte(salt), kdf.Iterations, 32)
	case KDFArgon2id:
		if kdf.Iterations < 1 || kdf.Memory < 1 || kdf.Parallelism < 1 {
			return nil, errors.New("bw: Argon2id without its parameters")
		}
		s := sha256.Sum256([]byte(salt))
		return argon2.IDKey([]byte(password), s[:], uint32(kdf.Iterations), uint32(kdf.Memory)*1024, uint8(kdf.Parallelism), 32), nil
	}
	return nil, fmt.Errorf("bw: unknown KDF %d", kdf.Type)
}

// StretchKey expands a master key into the key of 64 bytes that encrypts
// the user key, with HKDF-Expand.
func StretchKey(masterKey []byte) (SymmetricKey, error) {
	enc, err := hkdf.Expand(sha256.New, masterKey, "enc", 32)
	if err != nil {
		return SymmetricKey{}, err
	}
	mac, err := hkdf.Expand(sha256.New, masterKey, "mac", 32)
	if err != nil {
		return SymmetricKey{}, err
	}
	return SymmetricKey{Enc: enc, MAC: mac}, nil
}

// DecryptUserKey decrypts the user key that the master key protects: with
// the stretched master key, or the master key itself for the oldest
// accounts, whose strings have no MAC.
func DecryptUserKey(masterKey []byte, encUserKey string) (SymmetricKey, error) {
	e, err := ParseEncString(encUserKey)
	if err != nil {
		return SymmetricKey{}, err
	}
	var b []byte
	switch e.Type {
	case AesCbc256B64:
		k, _ := NewSymmetricKey(masterKey)
		b, err = k.Decrypt(e)
	case AesCbc256HmacSha256B64:
		var k SymmetricKey
		if k, err = StretchKey(masterKey); err != nil {
			return SymmetricKey{}, err
		}
		b, err = k.Decrypt(e)
		k.Wipe()
	default:
		return SymmetricKey{}, fmt.Errorf("%w: a user key of type %d", ErrUnsupported, e.Type)
	}
	if err != nil {
		return SymmetricKey{}, err
	}
	defer clear(b)
	return NewSymmetricKey(b)
}

// MasterPasswordHash is the hash of the master password Bitwarden servers
// check, PBKDF2 of the master key salted with the password, once.
func MasterPasswordHash(masterKey []byte, password string) string {
	h, _ := pbkdf2.Key(sha256.New, string(masterKey), []byte(password), 1, 32)
	return base64.StdEncoding.EncodeToString(h)
}

// DecryptPrivateKey decrypts the RSA key of the account with its user key.
func DecryptPrivateKey(userKey SymmetricKey, enc string) (*rsa.PrivateKey, error) {
	e, err := ParseEncString(enc)
	if err != nil {
		return nil, err
	}
	der, err := userKey.Decrypt(e)
	if err != nil {
		return nil, err
	}
	defer clear(der)
	key, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("bw: private key: %w", err)
	}
	rk, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("bw: the private key is not RSA")
	}
	return rk, nil
}

// DecryptRSA decrypts a string encrypted to the account's public key, as
// the keys of its organizations are.
func DecryptRSA(key *rsa.PrivateKey, enc string) ([]byte, error) {
	e, err := ParseEncString(enc)
	if err != nil {
		return nil, err
	}
	switch e.Type {
	case Rsa2048OaepSha1B64, Rsa2048OaepSha1HmacSha256B64:
		return rsa.DecryptOAEP(sha1.New(), nil, key, e.Data, nil)
	case Rsa2048OaepSha256B64, Rsa2048OaepSha256HmacSha256B64:
		return rsa.DecryptOAEP(sha256.New(), nil, key, e.Data, nil)
	}
	return nil, fmt.Errorf("%w: type %d with an RSA key", ErrUnsupported, e.Type)
}

// EncryptRSA encrypts b to a public key, as type 4: RSA-OAEP with SHA-1.
func EncryptRSA(pub *rsa.PublicKey, b []byte) (string, error) {
	data, err := rsa.EncryptOAEP(sha1.New(), rand.Reader, pub, b, nil)
	if err != nil {
		return "", err
	}
	return EncString{Type: Rsa2048OaepSha1B64, Data: data}.String(), nil
}
