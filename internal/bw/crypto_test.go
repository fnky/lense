package bw

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"testing"
)

func TestSymmetricRoundTrip(t *testing.T) {
	k := GenerateSymmetricKey()
	for _, s := range []string{"", "a", "exactly sixteen!", "hunter2 — with ünïcode"} {
		enc, err := k.EncryptString(s)
		if err != nil {
			t.Fatal(err)
		}
		got, err := k.DecryptString(enc)
		if err != nil || got != s {
			t.Errorf("%q: got %q, %v", s, got, err)
		}
	}
}

func TestWrongKeyFailsMAC(t *testing.T) {
	enc, _ := GenerateSymmetricKey().EncryptString("secret")
	if _, err := GenerateSymmetricKey().DecryptString(enc); !errors.Is(err, ErrMAC) {
		t.Fatalf("err %v, want ErrMAC", err)
	}
}

func TestParseEncString(t *testing.T) {
	iv := base64.StdEncoding.EncodeToString(make([]byte, 16))
	data := base64.StdEncoding.EncodeToString(make([]byte, 32))
	mac := base64.StdEncoding.EncodeToString(make([]byte, 32))
	for _, tc := range []struct {
		in   string
		want EncType
		err  bool
	}{
		{"2." + iv + "|" + data + "|" + mac, AesCbc256HmacSha256B64, false},
		{iv + "|" + data + "|" + mac, AesCbc256HmacSha256B64, false},
		{"0." + iv + "|" + data, AesCbc256B64, false},
		{"4." + data, Rsa2048OaepSha1B64, false},
		{"6." + data + "|" + mac, Rsa2048OaepSha1HmacSha256B64, false},
		{"2." + iv + "|" + data, 0, true},
		{"x.abc", 0, true},
		{"7.abc", 0, true},
	} {
		e, err := ParseEncString(tc.in)
		if (err != nil) != tc.err || (!tc.err && e.Type != tc.want) {
			t.Errorf("%q: type %d, err %v", tc.in, e.Type, err)
		}
		if !tc.err && e.String() != tc.in && tc.in[1] == '.' {
			t.Errorf("%q written as %q", tc.in, e.String())
		}
	}
}

// The master key of an account: PBKDF2-SHA256 of "asdfasdf" salted with
// "test@example.com", checked against Python's hashlib.
func TestMasterKeyPBKDF2(t *testing.T) {
	k, err := MasterKey("asdfasdf", " Test@Example.com ", KDF{Type: KDFPBKDF2, Iterations: 100000})
	if err != nil {
		t.Fatal(err)
	}
	want := "GGkJ2Dsv6ridIS9NfsNznTvQYYQj/KJq3XDmEmVH7nY="
	if got := base64.StdEncoding.EncodeToString(k); got != want {
		t.Errorf("master key %s, want %s", got, want)
	}
	if got := MasterPasswordHash(k, "asdfasdf"); got != "5Yu1aswTFWsixyXsj9g20ROR9U/6SDVQ0H5qNACWB6E=" {
		t.Errorf("hash %s", got)
	}
	s, err := StretchKey(k)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(s.Enc[:8]) != "77245e51cf35a781" || hex.EncodeToString(s.MAC[:8]) != "10af25f6dc8cc31f" {
		t.Errorf("stretched %x %x", s.Enc[:8], s.MAC[:8])
	}
}

func TestMasterKeyArgon2id(t *testing.T) {
	k, err := MasterKey("asdfasdf", "test@example.com", KDF{Type: KDFArgon2id, Iterations: 3, Memory: 4, Parallelism: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(k) != 32 {
		t.Fatalf("len %d", len(k))
	}
	again, _ := MasterKey("asdfasdf", "TEST@example.com", KDF{Type: KDFArgon2id, Iterations: 3, Memory: 4, Parallelism: 1})
	if hex.EncodeToString(k) != hex.EncodeToString(again) {
		t.Error("the salt is not case-insensitive")
	}
}

func TestUserKeyRoundTrip(t *testing.T) {
	mk, _ := MasterKey("correct horse", "a@b.c", KDF{Type: KDFPBKDF2, Iterations: 1000})
	stretched, _ := StretchKey(mk)
	user := GenerateSymmetricKey()
	e, _ := stretched.Encrypt(user.Bytes())
	got, err := DecryptUserKey(mk, e.String())
	if err != nil || hex.EncodeToString(got.Bytes()) != hex.EncodeToString(user.Bytes()) {
		t.Fatalf("user key %v", err)
	}
	wrong, _ := MasterKey("wrong", "a@b.c", KDF{Type: KDFPBKDF2, Iterations: 1000})
	if _, err := DecryptUserKey(wrong, e.String()); !errors.Is(err, ErrMAC) {
		t.Errorf("wrong password: %v", err)
	}
}

func TestOrganizationKey(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	user := GenerateSymmetricKey()
	der, _ := x509.MarshalPKCS8PrivateKey(priv)
	encPriv, _ := user.Encrypt(der)
	org := GenerateSymmetricKey()
	encOrg, err := EncryptRSA(&priv.PublicKey, org.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	pk, err := DecryptPrivateKey(user, encPriv.String())
	if err != nil {
		t.Fatal(err)
	}
	b, err := DecryptRSA(pk, encOrg)
	if err != nil || hex.EncodeToString(b) != hex.EncodeToString(org.Bytes()) {
		t.Fatalf("org key %v", err)
	}
}
