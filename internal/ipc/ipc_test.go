package ipc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bitwarden-mini/internal/bw"
)

// desktop is the desktop app's end of the socket, as its biometric
// message handler answers.
type desktop struct {
	t        *testing.T
	ln       net.Listener
	users    map[string]bw.SymmetricKey // the user keys it unlocks
	deny     bool                       // Touch ID is canceled
	status   Status
	announce bool // drops clients that do not announce themselves first
}

func startDesktop(t *testing.T, d *desktop) {
	t.Helper()
	// Unix socket paths must be short: t.TempDir's are too long on macOS.
	dir, err := os.MkdirTemp("/tmp", "bwm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("BITWARDEN_IPC_SOCKET_DIR", dir)
	t.Setenv("HOME", dir) // no real socket of the Mac's
	ln, err := net.Listen("unix", filepath.Join(dir, "s.bw"))
	if err != nil {
		t.Fatal(err)
	}
	d.t, d.ln = t, ln
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go d.serve(conn)
		}
	}()
}

func readFrame(r io.Reader) ([]byte, error) {
	var n [4]byte
	if _, err := io.ReadFull(r, n[:]); err != nil {
		return nil, err
	}
	b := make([]byte, binary.NativeEndian.Uint32(n[:]))
	_, err := io.ReadFull(r, b)
	return b, err
}

func writeFrame(w io.Writer, v any) {
	b, _ := json.Marshal(v)
	frame := binary.NativeEndian.AppendUint32(nil, uint32(len(b)))
	w.Write(append(frame, b...))
}

func (d *desktop) serve(conn net.Conn) {
	defer conn.Close()
	var key bw.SymmetricKey
	first := true
	for {
		b, err := readFrame(conn)
		if err != nil {
			return
		}
		if first && d.announce {
			first = false
			var a struct {
				ClientType string `json:"clientType"`
			}
			if json.Unmarshal(b, &a) != nil || a.ClientType == "" {
				return // as the server of 2026's later releases does
			}
			continue
		}
		first = false
		// Another client's message, which every client hears.
		writeFrame(conn, map[string]any{"appId": "someone-else", "command": "setupEncryption", "sharedSecret": "AAAA"})
		var msg struct {
			AppID   string          `json:"appId"`
			Message json.RawMessage `json:"message"`
		}
		if err := json.Unmarshal(b, &msg); err != nil {
			d.t.Errorf("not JSON: %s", b)
			return
		}
		var clear struct {
			Command   string `json:"command"`
			PublicKey string `json:"publicKey"`
			UserID    string `json:"userId"`
		}
		if json.Unmarshal(msg.Message, &clear) == nil && clear.Command == "setupEncryption" {
			if _, ok := d.users[clear.UserID]; !ok {
				writeFrame(conn, map[string]any{"appId": msg.AppID, "command": "wrongUserId"})
				continue
			}
			der, _ := base64.StdEncoding.DecodeString(clear.PublicKey)
			pub, err := x509.ParsePKIXPublicKey(der)
			if err != nil {
				d.t.Errorf("public key: %v", err)
				return
			}
			key = bw.GenerateSymmetricKey()
			secret, _ := rsa.EncryptOAEP(sha1.New(), rand.Reader, pub.(*rsa.PublicKey), key.Bytes(), nil)
			writeFrame(conn, map[string]any{"appId": msg.AppID, "command": "setupEncryption", "messageId": -1,
				"sharedSecret": base64.StdEncoding.EncodeToString(secret)})
			continue
		}
		var es encString
		if err := json.Unmarshal(msg.Message, &es); err != nil || !key.Valid() {
			writeFrame(conn, map[string]any{"appId": msg.AppID, "command": "invalidateEncryption"})
			continue
		}
		plain, err := key.DecryptString(es.EncryptedString)
		if err != nil {
			d.t.Errorf("decrypt: %v", err)
			return
		}
		var cmd struct {
			Command   string `json:"command"`
			MessageID int    `json:"messageId"`
			UserID    string `json:"userId"`
			Timestamp int64  `json:"timestamp"`
		}
		json.Unmarshal([]byte(plain), &cmd)
		if time.Since(time.UnixMilli(cmd.Timestamp)).Abs() > 10*time.Second {
			d.t.Errorf("an old timestamp")
		}
		resp := map[string]any{"command": cmd.Command, "messageId": cmd.MessageID, "timestamp": time.Now().UnixMilli()}
		switch cmd.Command {
		case "getBiometricsStatusForUser":
			resp["response"] = int(d.status)
		case "unlockWithBiometricsForUser":
			uk, ok := d.users[cmd.UserID]
			if d.deny || !ok {
				resp["response"] = false
			} else {
				resp["response"] = true
				resp["userKeyB64"] = base64.StdEncoding.EncodeToString(uk.Bytes())
			}
		}
		rb, _ := json.Marshal(resp)
		enc, _ := key.Encrypt(rb)
		b64 := base64.StdEncoding.EncodeToString
		writeFrame(conn, map[string]any{"appId": msg.AppID, "messageId": cmd.MessageID, "message": map[string]any{
			"encryptedString": enc.String(), "encryptionType": 2, "data": b64(enc.Data), "iv": b64(enc.IV), "mac": b64(enc.MAC),
		}})
	}
}

const user = "11111111-1111-4111-8111-111111111111"

func TestUnlock(t *testing.T) {
	for _, announce := range []bool{false, true} {
		uk := bw.GenerateSymmetricKey()
		startDesktop(t, &desktop{users: map[string]bw.SymmetricKey{user: uk}, announce: announce})
		ctx := context.Background()
		c, err := Dial(ctx, "test-app", user)
		if err != nil {
			t.Fatalf("announce %v: %v", announce, err)
		}
		if s, err := c.Status(ctx, user); err != nil || s != StatusAvailable {
			t.Errorf("status %v %v", s, err)
		}
		got, err := c.Unlock(ctx, user)
		if err != nil {
			t.Fatal(err)
		}
		if string(got.Bytes()) != string(uk.Bytes()) {
			t.Error("another user key")
		}
		c.Close()
	}
}

func TestUnlockDenied(t *testing.T) {
	startDesktop(t, &desktop{users: map[string]bw.SymmetricKey{user: bw.GenerateSymmetricKey()}, deny: true, status: StatusNotEnabledLocally})
	ctx := context.Background()
	c, err := Dial(ctx, "test-app", user)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if s, _ := c.Status(ctx, user); s != StatusNotEnabledLocally {
		t.Errorf("status %v", s)
	}
	if _, err := c.Unlock(ctx, user); !errors.Is(err, ErrDenied) {
		t.Errorf("err %v", err)
	}
}

func TestWrongUser(t *testing.T) {
	startDesktop(t, &desktop{users: map[string]bw.SymmetricKey{}})
	if _, err := Dial(context.Background(), "test-app", user); !errors.Is(err, ErrWrongUser) {
		t.Errorf("err %v", err)
	}
}

func TestNoSocket(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BITWARDEN_IPC_SOCKET_DIR", dir)
	t.Setenv("HOME", dir)
	if _, err := Dial(context.Background(), "test-app", user); !errors.Is(err, ErrNoSocket) {
		t.Errorf("err %v", err)
	}
	// A socket left behind by an app that quit.
	short, _ := os.MkdirTemp("/tmp", "bwm")
	defer os.RemoveAll(short)
	t.Setenv("BITWARDEN_IPC_SOCKET_DIR", short)
	ln, _ := net.Listen("unix", filepath.Join(short, "s.bw"))
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close()
	if _, err := Dial(context.Background(), "test-app", user); !errors.Is(err, ErrNoSocket) {
		t.Errorf("stale socket: %v", err)
	}
}
