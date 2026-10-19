package bw

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DataPaths returns where the Bitwarden desktop app may keep its data on
// macOS, the most likely first: $BITWARDEN_APPDATA_DIR, the app from
// bitwarden.com or Homebrew, then the one from the App Store, in its
// sandbox.
func DataPaths() []string {
	home, _ := os.UserHomeDir()
	var paths []string
	if dir := os.Getenv("BITWARDEN_APPDATA_DIR"); dir != "" {
		paths = append(paths, filepath.Join(dir, "data.json"))
	}
	return append(paths,
		filepath.Join(home, "Library", "Application Support", "Bitwarden", "data.json"),
		filepath.Join(home, "Library", "Containers", "com.bitwarden.desktop", "Data", "Library", "Application Support", "Bitwarden", "data.json"),
	)
}

// FindData returns the data file of the desktop app written last, or
// ErrNoData.
func FindData() (string, error) {
	var best string
	var at time.Time
	for _, p := range DataPaths() {
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			continue
		}
		if best == "" || st.ModTime().After(at) {
			best, at = p, st.ModTime()
		}
	}
	if best == "" {
		return "", ErrNoData
	}
	return best, nil
}

var (
	// ErrNoData means the Bitwarden desktop app has no data on this Mac:
	// it is not installed, or has never been opened.
	ErrNoData = errors.New("bw: no data of the Bitwarden desktop app")
	// ErrNoAccount means the account is not signed in to the desktop app.
	ErrNoAccount = errors.New("bw: the account is not signed in to the Bitwarden app")
	// ErrWrongPassword means the master password does not unlock the
	// account.
	ErrWrongPassword = errors.New("bw: incorrect master password")
	// ErrNoMasterPassword means the account unlocks without a master
	// password, with SSO and trusted devices or Key Connector.
	ErrNoMasterPassword = errors.New("bw: the account has no master password")
)

// Store is the data file of the desktop app, as read at one time.
type Store struct {
	Path    string
	ModTime time.Time
	data    map[string]json.RawMessage
}

// Open reads the data file at path.
func Open(path string) (*Store, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoData
		}
		return nil, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	s, err := Parse(b)
	if err != nil {
		return nil, err
	}
	s.Path, s.ModTime = path, st.ModTime()
	return s, nil
}

// Parse reads the contents of a data file.
func Parse(b []byte) (*Store, error) {
	s := &Store{}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, fmt.Errorf("bw: the Bitwarden app's data is not JSON: %w", err)
	}
	return s, nil
}

// get decodes the value of key into v, reporting whether there was one.
func (s *Store) get(key string, v any) bool {
	raw, ok := s.data[key]
	if !ok || string(raw) == "null" {
		return false
	}
	return json.Unmarshal(raw, v) == nil
}

func userKey(userID, name string) string { return "user_" + userID + "_" + name }

// Server is the Bitwarden server an account is on.
type Server struct {
	Region string // "US", "EU" or "Self-hosted"
	URL    string // the base URL of a self-hosted server
}

// Regions of Bitwarden's cloud.
const (
	RegionUS         = "US"
	RegionEU         = "EU"
	RegionSelfHosted = "Self-hosted"
)

// Host names the server as the user knows it: bitwarden.com,
// bitwarden.eu, or the host of a server of their own.
func (s Server) Host() string {
	switch s.Region {
	case RegionEU:
		return "bitwarden.eu"
	case RegionSelfHosted:
		if u, err := url.Parse(s.URL); err == nil && u.Host != "" {
			return u.Host
		}
		if s.URL != "" {
			return strings.TrimPrefix(strings.TrimPrefix(s.URL, "https://"), "http://")
		}
		return "a self-hosted server"
	}
	return "bitwarden.com"
}

// Same reports whether s and o are the same server.
func (s Server) Same(o Server) bool {
	if s.Region == "" {
		s.Region = RegionUS
	}
	if o.Region == "" {
		o.Region = RegionUS
	}
	if s.Region != o.Region {
		return false
	}
	return s.Region != RegionSelfHosted || strings.EqualFold(s.Host(), o.Host())
}

// Account is an account signed in to the desktop app.
type Account struct {
	ID     string
	Email  string
	Name   string
	Server Server
	Active bool // the account the app shows
	// BiometricUnlock is whether the desktop app unlocks this account with
	// Touch ID, which Bitwarden Mini asks it to do.
	BiometricUnlock bool
	HasPassword     bool
	LastSync        time.Time
}

// Accounts lists the accounts signed in to the desktop app, the active
// one first.
func (s *Store) Accounts() []Account {
	var accounts map[string]struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	s.get("global_account_accounts", &accounts)
	var active string
	s.get("global_account_activeAccountId", &active)
	var out []Account
	for id, a := range accounts {
		acc := Account{ID: id, Email: a.Email, Name: a.Name, Active: id == active, Server: s.server(id)}
		s.get(userKey(id, "biometricSettings_biometricUnlockEnabled"), &acc.BiometricUnlock)
		var opts struct {
			HasMasterPassword *bool `json:"hasMasterPassword"`
		}
		acc.HasPassword = true
		if s.get(userKey(id, "userDecryptionOptions_decryptionOptions"), &opts) && opts.HasMasterPassword != nil {
			acc.HasPassword = *opts.HasMasterPassword
		}
		var last string
		if s.get(userKey(id, "sync_lastSync"), &last) {
			acc.LastSync, _ = time.Parse(time.RFC3339Nano, last)
		}
		out = append(out, acc)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Active != out[j].Active {
			return out[i].Active
		}
		return out[i].Email < out[j].Email
	})
	return out
}

// Account returns the account of userID.
func (s *Store) Account(userID string) (Account, bool) {
	for _, a := range s.Accounts() {
		if a.ID == userID {
			return a, true
		}
	}
	return Account{}, false
}

// FindAccount returns the account signed in with email, on any server.
func (s *Store) FindAccount(email string) (Account, bool) {
	email = strings.TrimSpace(email)
	for _, a := range s.Accounts() {
		if strings.EqualFold(a.Email, email) {
			return a, true
		}
	}
	return Account{}, false
}

func (s *Store) server(userID string) Server {
	var env struct {
		Region string `json:"region"`
		URLs   *struct {
			Base     string `json:"base"`
			WebVault string `json:"webVault"`
			API      string `json:"api"`
		} `json:"urls"`
	}
	if !s.get(userKey(userID, "environment_environment"), &env) {
		s.get("global_environment_environment", &env)
	}
	sv := Server{Region: env.Region}
	if sv.Region == "" {
		sv.Region = RegionUS
	}
	if env.URLs != nil {
		for _, u := range []string{env.URLs.Base, env.URLs.WebVault, env.URLs.API} {
			if u != "" {
				sv.URL = u
				break
			}
		}
	}
	return sv
}

// unlockData is what derives the user key from the master password.
type unlockData struct {
	Salt       string `json:"salt"`
	KDF        KDF    `json:"kdf"`
	WrappedKey string `json:"masterKeyWrappedUserKey"`
}

func (s *Store) unlockData(userID string) (unlockData, error) {
	var u unlockData
	if s.get(userKey(userID, "masterPasswordUnlock_masterPasswordUnlockKey"), &u) && u.WrappedKey != "" {
		return u, nil
	}
	// Before 2025, the parts were apart, and the salt was the email.
	acc, ok := s.Account(userID)
	if !ok {
		return u, ErrNoAccount
	}
	u.Salt = acc.Email
	if !s.get(userKey(userID, "kdfConfig_kdfConfig"), &u.KDF) {
		return u, ErrNoMasterPassword
	}
	if !s.get(userKey(userID, "masterPassword_masterKeyEncryptedUserKey"), &u.WrappedKey) || u.WrappedKey == "" {
		return u, ErrNoMasterPassword
	}
	return u, nil
}

// KDF returns how the account derives its master key, for the settings.
func (s *Store) KDF(userID string) (KDF, error) {
	u, err := s.unlockData(userID)
	return u.KDF, err
}

// UnlockWithPassword derives the user key of the account from its master
// password, with no server: ErrWrongPassword when it is not the one.
func (s *Store) UnlockWithPassword(userID, password string) (SymmetricKey, error) {
	u, err := s.unlockData(userID)
	if err != nil {
		return SymmetricKey{}, err
	}
	mk, err := MasterKey(password, u.Salt, u.KDF)
	if err != nil {
		return SymmetricKey{}, err
	}
	defer clear(mk)
	k, err := DecryptUserKey(mk, u.WrappedKey)
	if errors.Is(err, ErrMAC) || errors.Is(err, ErrMalformed) {
		return SymmetricKey{}, ErrWrongPassword
	}
	return k, err
}

// CheckUserKey reports whether key is the user key of the account, by
// decrypting what it encrypts.
func (s *Store) CheckUserKey(userID string, key SymmetricKey) error {
	if enc := s.privateKey(userID); enc != "" {
		_, err := DecryptPrivateKey(key, enc)
		return err
	}
	var folders map[string]struct {
		Name string `json:"name"`
	}
	s.get(userKey(userID, "folder_folders"), &folders)
	for _, f := range folders {
		_, err := key.DecryptString(f.Name)
		return err
	}
	return nil
}

func (s *Store) privateKey(userID string) string {
	var state struct {
		V1 *struct {
			PrivateKey string `json:"private_key"`
		} `json:"V1"`
		V2 *struct {
			PrivateKey string `json:"private_key"`
		} `json:"V2"`
	}
	if s.get(userKey(userID, "crypto_accountCryptographicState"), &state) {
		switch {
		case state.V1 != nil && state.V1.PrivateKey != "":
			return state.V1.PrivateKey
		case state.V2 != nil && state.V2.PrivateKey != "":
			return state.V2.PrivateKey
		}
	}
	var enc string
	s.get(userKey(userID, "crypto_privateKey"), &enc)
	return enc
}
