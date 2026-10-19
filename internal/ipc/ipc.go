// Package ipc talks to the Bitwarden desktop app over the socket its
// browser extensions reach it through, as the extension does: it sets up
// a session key, then asks the app to unlock an account with Touch ID,
// which gives the account's user key. Nothing goes to Bitwarden's servers.
//
// The socket carries frames of a 4-byte length, in the machine's byte
// order, and JSON. A client sends {"appId", "message"}: the message is a
// command in the clear to set up the session, then commands encrypted
// with the session key. The app answers every client: answers for other
// appIds are skipped.
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
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"bitwarden-mini/internal/bw"
)

// SocketPaths returns where the desktop app's socket may be: in its app
// group's container, as the App Store's app and newer releases have it,
// then in its caches, as older releases from bitwarden.com have it.
func SocketPaths() []string {
	home, _ := os.UserHomeDir()
	var paths []string
	if dir := os.Getenv("BITWARDEN_IPC_SOCKET_DIR"); dir != "" {
		paths = append(paths, filepath.Join(dir, "s.bw"))
	}
	return append(paths,
		filepath.Join(home, "Library", "Group Containers", "LTZ2PFU5D6.com.bitwarden.desktop", "s.bw"),
		filepath.Join(home, "Library", "Caches", "com.bitwarden.desktop", "s.bw"),
	)
}

var (
	// ErrNoSocket means the desktop app does not listen: it does not run,
	// or "Allow browser integration" is off in its settings.
	ErrNoSocket = errors.New("ipc: the Bitwarden app does not listen")
	// ErrWrongUser means the account is not signed in to the desktop app.
	ErrWrongUser = errors.New("ipc: the account is not signed in to the Bitwarden app")
	// ErrDenied means the desktop app did not unlock: Touch ID was
	// canceled or failed, or unlocking with it is off for the account.
	ErrDenied = errors.New("ipc: the Bitwarden app did not unlock the account")
	// ErrNoAnswer means the desktop app did not answer in time.
	ErrNoAnswer = errors.New("ipc: the Bitwarden app did not answer")
	// ErrDropped means the desktop app closed the connection without an
	// answer.
	ErrDropped = errors.New("ipc: the Bitwarden app closed the connection")
)

// Status is how the desktop app can unlock an account with biometrics.
type Status int

const (
	StatusAvailable           Status = 0
	StatusUnlockNeeded        Status = 1 // unlock with the master password first, once
	StatusHardwareUnavailable Status = 2
	StatusAutoSetupNeeded     Status = 3
	StatusManualSetupNeeded   Status = 4
	StatusPlatformUnsupported Status = 5
	StatusDesktopDisconnected Status = 6
	StatusNotEnabledLocally   Status = 7
	StatusNotEnabledInDesktop Status = 8
	StatusPermissionMissing   Status = 9
)

// maxFrame is the largest frame the desktop app sends.
const maxFrame = 1 << 20

// Client is a session with the desktop app.
type Client struct {
	appID    string
	conn     net.Conn
	key      bw.SymmetricKey
	next     int
	announce bool
}

// Dial connects to the desktop app's socket and sets up a session for an
// account, identifying this app by appID, a stable random UUID.
func Dial(ctx context.Context, appID, userID string) (*Client, error) {
	var lastErr error = ErrNoSocket
	for _, p := range SocketPaths() {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		// Releases of 2026 take no announcement; later ones drop clients
		// that do not announce what they are first.
		for _, announce := range []bool{false, true} {
			c, err := dialPath(ctx, p, appID, userID, announce)
			if err == nil {
				return c, nil
			}
			lastErr = err
			if isRefused(err) || !dropped(err) {
				break
			}
			lastErr = ErrDropped
		}
	}
	if isRefused(lastErr) {
		return nil, ErrNoSocket
	}
	return nil, lastErr
}

func isRefused(err error) bool {
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "dial"
}

// dropped reports whether the app closed the connection.
func dropped(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE)
}

func dialPath(ctx context.Context, path, appID, userID string, announce bool) (*Client, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	c := &Client{appID: appID, conn: conn, announce: announce}
	if err := c.setup(ctx, userID); err != nil {
		conn.Close()
		return nil, err
	}
	return c, nil
}

// Close ends the session.
func (c *Client) Close() error {
	c.key.Wipe()
	return c.conn.Close()
}

func (c *Client) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	frame := make([]byte, 4+len(b))
	binary.NativeEndian.PutUint32(frame, uint32(len(b)))
	copy(frame[4:], b)
	_, err = c.conn.Write(frame)
	return err
}

func (c *Client) read() ([]byte, error) {
	var n [4]byte
	if _, err := io.ReadFull(c.conn, n[:]); err != nil {
		return nil, err
	}
	size := binary.NativeEndian.Uint32(n[:])
	if size > maxFrame {
		return nil, fmt.Errorf("ipc: a frame of %d bytes", size)
	}
	b := make([]byte, size)
	_, err := io.ReadFull(c.conn, b)
	return b, err
}

// outer is a frame of the desktop app: a command in the clear, or an
// encrypted message.
type outer struct {
	AppID        string          `json:"appId"`
	Command      string          `json:"command"`
	MessageID    *int            `json:"messageId"`
	SharedSecret string          `json:"sharedSecret"`
	Message      json.RawMessage `json:"message"`
}

// next reads frames until one for this app, with the context's deadline.
func (c *Client) nextFrame(ctx context.Context) (outer, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}
	_ = c.conn.SetReadDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = c.conn.SetReadDeadline(time.Now()) })
	defer stop()
	for {
		b, err := c.read()
		if err != nil {
			if ctx.Err() != nil {
				return outer{}, ctx.Err()
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				return outer{}, ErrNoAnswer
			}
			return outer{}, err
		}
		var o outer
		if json.Unmarshal(b, &o) != nil || o.AppID != c.appID {
			continue // another client's
		}
		return o, nil
	}
}

func (c *Client) setup(ctx context.Context, userID string) error {
	if c.announce {
		if err := c.write(map[string]string{"clientType": "cli"}); err != nil {
			return err
		}
	}
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	pub, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return err
	}
	c.next++
	err = c.write(map[string]any{
		"appId": c.appID,
		"message": map[string]any{
			"command":   "setupEncryption",
			"publicKey": base64.StdEncoding.EncodeToString(pub),
			"userId":    userID,
			"messageId": c.next,
			"timestamp": time.Now().UnixMilli(),
		},
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for {
		o, err := c.nextFrame(ctx)
		if err != nil {
			return err
		}
		switch o.Command {
		case "wrongUserId":
			return ErrWrongUser
		case "setupEncryption":
			secret, err := base64.StdEncoding.DecodeString(o.SharedSecret)
			if err != nil {
				return fmt.Errorf("ipc: the session key: %w", err)
			}
			raw, err := rsa.DecryptOAEP(sha1.New(), nil, priv, secret, nil)
			if err != nil {
				return fmt.Errorf("ipc: the session key: %w", err)
			}
			c.key, err = bw.NewSymmetricKey(raw)
			clear(raw)
			return err
		}
	}
}

// encString is an encrypted string as the extension sends it, an object.
type encString struct {
	EncryptedString string `json:"encryptedString"`
	EncryptionType  int    `json:"encryptionType"`
	Data            string `json:"data"`
	IV              string `json:"iv"`
	MAC             string `json:"mac"`
}

func (c *Client) encrypt(v any) (encString, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return encString{}, err
	}
	e, err := c.key.Encrypt(b)
	if err != nil {
		return encString{}, err
	}
	b64 := base64.StdEncoding.EncodeToString
	return encString{
		EncryptedString: e.String(), EncryptionType: int(e.Type),
		Data: b64(e.Data), IV: b64(e.IV), MAC: b64(e.MAC),
	}, nil
}

func (c *Client) decrypt(raw json.RawMessage) ([]byte, error) {
	var es encString
	var s string
	switch {
	case json.Unmarshal(raw, &s) == nil:
	case json.Unmarshal(raw, &es) == nil && es.EncryptedString != "":
		s = es.EncryptedString
	case es.Data != "":
		s = fmt.Sprintf("%d.%s|%s|%s", es.EncryptionType, es.IV, es.Data, es.MAC)
	default:
		return nil, errors.New("ipc: an answer that is not encrypted")
	}
	e, err := bw.ParseEncString(s)
	if err != nil {
		return nil, err
	}
	return c.key.Decrypt(e)
}

// answer is a decrypted answer of the desktop app.
type answer struct {
	Command   string          `json:"command"`
	MessageID int             `json:"messageId"`
	Response  json.RawMessage `json:"response"`
	// UserKeyB64 is the user key, in base64 as a JSON string: bytes, which
	// Unlock wipes once it decoded them, as it could not a string.
	UserKeyB64 json.RawMessage `json:"userKeyB64"`
	Timestamp  int64           `json:"timestamp"`
}

// call sends an encrypted command and waits for its answer.
func (c *Client) call(ctx context.Context, cmd map[string]any) (answer, error) {
	c.next++
	id := c.next
	cmd["messageId"] = id
	cmd["timestamp"] = time.Now().UnixMilli()
	msg, err := c.encrypt(cmd)
	if err != nil {
		return answer{}, err
	}
	if err := c.write(map[string]any{"appId": c.appID, "message": msg}); err != nil {
		return answer{}, err
	}
	for {
		o, err := c.nextFrame(ctx)
		if err != nil {
			return answer{}, err
		}
		switch o.Command {
		case "invalidateEncryption":
			return answer{}, ErrDenied
		case "wrongUserId":
			return answer{}, ErrWrongUser
		}
		if len(o.Message) == 0 {
			continue
		}
		b, err := c.decrypt(o.Message)
		if err != nil {
			continue
		}
		var a answer
		err = json.Unmarshal(b, &a)
		clear(b) // the answer decrypted, the user key in it
		if err != nil || a.MessageID != id {
			clear(a.UserKeyB64)
			continue
		}
		return a, nil
	}
}

// Status asks how the desktop app can unlock the account with Touch ID.
func (c *Client) Status(ctx context.Context, userID string) (Status, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	a, err := c.call(ctx, map[string]any{"command": "getBiometricsStatusForUser", "userId": userID})
	if err != nil {
		return 0, err
	}
	var s Status
	if err := json.Unmarshal(a.Response, &s); err != nil {
		return 0, fmt.Errorf("ipc: a status of %s", a.Response)
	}
	return s, nil
}

// Unlock asks the desktop app to unlock the account with Touch ID, which
// it prompts for, and returns the account's user key. It waits for the
// user up to a minute.
func (c *Client) Unlock(ctx context.Context, userID string) (bw.SymmetricKey, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	a, err := c.call(ctx, map[string]any{"command": "unlockWithBiometricsForUser", "userId": userID})
	if err != nil {
		return bw.SymmetricKey{}, err
	}
	defer clear(a.UserKeyB64)
	var ok bool
	if json.Unmarshal(a.Response, &ok) != nil || !ok || len(a.UserKeyB64) < 2 || a.UserKeyB64[0] != '"' {
		return bw.SymmetricKey{}, ErrDenied
	}
	// Base64 has nothing a JSON string escapes: its quotes go, and the
	// rest decodes into memory wiped once the key copied it.
	b64 := a.UserKeyB64[1 : len(a.UserKeyB64)-1]
	raw := make([]byte, base64.StdEncoding.DecodedLen(len(b64)))
	defer clear(raw)
	n, err := base64.StdEncoding.Decode(raw, b64)
	if err != nil {
		return bw.SymmetricKey{}, fmt.Errorf("ipc: the user key: %w", err)
	}
	return bw.NewSymmetricKey(raw[:n])
}
