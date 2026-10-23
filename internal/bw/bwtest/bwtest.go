// Package bwtest builds data files of the Bitwarden desktop app, encrypted
// as Bitwarden encrypts them, for tests.
package bwtest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"lense/internal/bw"
)

// Fixture is an account of the desktop app, with a personal vault and an
// organization's.
type Fixture struct {
	UserID   string
	Email    string
	Password string
	Server   bw.Server
	KDF      bw.KDF
	UserKey  bw.SymmetricKey
	OrgID    string
	OrgName  string
	OrgKey   bw.SymmetricKey
	// Biometric is whether the desktop app unlocks the account with Touch
	// ID.
	Biometric bool

	priv        *rsa.PrivateKey
	folders     map[string]any
	collections map[string]any
	ciphers     map[string]any
	n           int
}

var (
	keyOnce sync.Once
	rsaKey  *rsa.PrivateKey
)

// New returns an account with no items, on bitwarden.com, whose master
// password is "correct horse battery staple".
func New() *Fixture {
	keyOnce.Do(func() { rsaKey, _ = rsa.GenerateKey(rand.Reader, 2048) })
	return &Fixture{
		UserID:      "11111111-1111-4111-8111-111111111111",
		Email:       "ada@example.com",
		Password:    "correct horse battery staple",
		Server:      bw.Server{Region: bw.RegionUS},
		KDF:         bw.KDF{Type: bw.KDFPBKDF2, Iterations: 5000},
		UserKey:     bw.GenerateSymmetricKey(),
		OrgID:       "22222222-2222-4222-8222-222222222222",
		OrgName:     "Acme Inc.",
		OrgKey:      bw.GenerateSymmetricKey(),
		Biometric:   true,
		priv:        rsaKey,
		folders:     map[string]any{},
		collections: map[string]any{},
		ciphers:     map[string]any{},
	}
}

func (f *Fixture) id(prefix int) string {
	f.n++
	return fmt.Sprintf("%08d-0000-4000-8000-%012d", prefix, f.n)
}

func enc(k bw.SymmetricKey, s string) any {
	if s == "" {
		return nil
	}
	out, err := k.EncryptString(s)
	if err != nil {
		panic(err)
	}
	return out
}

// Folder adds a folder, returning its ID.
func (f *Fixture) Folder(name string) string {
	id := f.id(3)
	f.folders[id] = map[string]any{"id": id, "name": enc(f.UserKey, name), "revisionDate": "2026-01-02T03:04:05.000Z"}
	return id
}

// Collection adds a collection of the organization, returning its ID.
func (f *Fixture) Collection(name string) string {
	id := f.id(4)
	f.collections[id] = map[string]any{"id": id, "organizationId": f.OrgID, "name": enc(f.OrgKey, name), "readOnly": false}
	return id
}

// Item is an item to add, in the clear: as bw.Item is, with its secrets
// strings, which bw keeps encrypted.
type Item struct {
	ID             string
	Type           bw.ItemType
	Name           string
	Notes          string
	OrganizationID string
	FolderID       string
	CollectionIDs  []string
	Favorite       bool
	Reprompt       bool
	Deleted        bool
	Login          *Login
	Card           *Card
	Identity       *Identity
	SSHKey         *SSHKey
	Fields         []Field
}

type Login struct {
	Username, Password, TOTP string
	URIs                     []bw.URI
	Passkeys                 []bw.Passkey
}

type Card struct {
	CardholderName, Brand, Number, ExpMonth, ExpYear, Code string
}

type Identity struct {
	Title, FirstName, LastName, Email, Phone string
	Address1, City, Country, Company, SSN    string
}

type SSHKey struct {
	PrivateKey, PublicKey, Fingerprint string
}

type Field struct {
	Type        bw.FieldType
	Name, Value string
}

// Add adds an item, encrypted with a key of its own when itemKey is true,
// returning its ID. Its organization is it.OrganizationID.
func (f *Fixture) Add(it Item, itemKey bool) string {
	if it.ID == "" {
		it.ID = f.id(5)
	}
	key := f.UserKey
	if it.OrganizationID != "" {
		key = f.OrgKey
	}
	c := map[string]any{
		"id": it.ID, "type": int(it.Type), "favorite": it.Favorite, "edit": true, "viewPassword": true,
		"folderId": nil, "organizationId": nil, "collectionIds": it.CollectionIDs, "reprompt": 0,
		"revisionDate": "2026-03-04T05:06:07.000Z", "creationDate": "2026-01-02T03:04:05.000Z",
		"deletedDate": nil, "key": nil,
	}
	if it.FolderID != "" {
		c["folderId"] = it.FolderID
	}
	if it.OrganizationID != "" {
		c["organizationId"] = it.OrganizationID
	}
	if it.Reprompt {
		c["reprompt"] = 1
	}
	if it.Deleted {
		c["deletedDate"] = "2026-05-06T07:08:09.000Z"
	}
	if itemKey {
		own := bw.GenerateSymmetricKey()
		e, _ := key.Encrypt(own.Bytes())
		c["key"] = e.String()
		key = own
	}
	c["name"] = enc(key, it.Name)
	c["notes"] = enc(key, it.Notes)
	if l := it.Login; l != nil {
		var uris []any
		for _, u := range l.URIs {
			uris = append(uris, map[string]any{"uri": enc(key, u.URI), "match": u.Match})
		}
		var passkeys []any
		for _, p := range l.Passkeys {
			passkeys = append(passkeys, map[string]any{
				"rpId": enc(key, p.RpID), "rpName": enc(key, p.RpName), "userName": enc(key, p.UserName),
				"creationDate": "2026-01-02T03:04:05.000Z",
			})
		}
		c["login"] = map[string]any{
			"username": enc(key, l.Username), "password": enc(key, l.Password), "totp": enc(key, l.TOTP),
			"uris": uris, "fido2Credentials": passkeys,
		}
	}
	if cd := it.Card; cd != nil {
		c["card"] = map[string]any{
			"cardholderName": enc(key, cd.CardholderName), "brand": enc(key, cd.Brand), "number": enc(key, cd.Number),
			"expMonth": enc(key, cd.ExpMonth), "expYear": enc(key, cd.ExpYear), "code": enc(key, cd.Code),
		}
	}
	if it.Type == bw.TypeSecureNote {
		c["secureNote"] = map[string]any{"type": 0}
	}
	if id := it.Identity; id != nil {
		c["identity"] = map[string]any{
			"title": enc(key, id.Title), "firstName": enc(key, id.FirstName), "lastName": enc(key, id.LastName),
			"email": enc(key, id.Email), "phone": enc(key, id.Phone), "address1": enc(key, id.Address1),
			"city": enc(key, id.City), "country": enc(key, id.Country), "company": enc(key, id.Company),
			"ssn": enc(key, id.SSN),
		}
	}
	if k := it.SSHKey; k != nil {
		c["sshKey"] = map[string]any{"privateKey": enc(key, k.PrivateKey), "publicKey": enc(key, k.PublicKey), "keyFingerprint": enc(key, k.Fingerprint)}
	}
	var fields []any
	for _, fl := range it.Fields {
		fields = append(fields, map[string]any{"type": int(fl.Type), "name": enc(key, fl.Name), "value": enc(key, fl.Value)})
	}
	c["fields"] = fields
	f.ciphers[it.ID] = c
	return it.ID
}

// JSON returns the data file, as the desktop app writes it in 2026.
func (f *Fixture) JSON() []byte {
	mk, err := bw.MasterKey(f.Password, f.Email, f.KDF)
	if err != nil {
		panic(err)
	}
	stretched, _ := bw.StretchKey(mk)
	wrapped, _ := stretched.Encrypt(f.UserKey.Bytes())
	der, _ := x509.MarshalPKCS8PrivateKey(f.priv)
	encPriv, _ := f.UserKey.Encrypt(der)
	encOrg, _ := bw.EncryptRSA(&f.priv.PublicKey, f.OrgKey.Bytes())
	u := func(name string) string { return "user_" + f.UserID + "_" + name }
	var urls any
	if f.Server.URL != "" {
		urls = map[string]any{"base": f.Server.URL}
	}
	data := map[string]any{
		"stateVersion":                   85,
		"global_account_accounts":        map[string]any{f.UserID: map[string]any{"email": f.Email, "emailVerified": true}},
		"global_account_activeAccountId": f.UserID,
		"global_environment_environment": map[string]any{"region": f.Server.Region, "urls": urls},
		u("environment_environment"):     map[string]any{"region": f.Server.Region, "urls": urls},
		u("kdfConfig_kdfConfig"):         f.KDF,
		u("masterPasswordUnlock_masterPasswordUnlockKey"): map[string]any{
			"salt": f.Email, "kdf": f.KDF, "masterKeyWrappedUserKey": wrapped.String(),
		},
		u("masterPassword_masterKeyEncryptedUserKey"):                wrapped.String(),
		u("userDecryptionOptions_decryptionOptions"):                 map[string]any{"hasMasterPassword": true},
		u("crypto_accountCryptographicState"):                        map[string]any{"V1": map[string]any{"private_key": encPriv.String()}},
		u("crypto_organizationKeys"):                                 map[string]any{f.OrgID: map[string]any{"type": "organization", "key": encOrg}},
		u("organizations_organizations"):                             map[string]any{f.OrgID: map[string]any{"id": f.OrgID, "name": f.OrgName, "enabled": true}},
		u("biometricSettings_biometricUnlockEnabled"):                f.Biometric,
		u("sync_lastSync"):                                           "2026-10-19T08:00:00.000Z",
		u("folder_folders"):                                          f.folders,
		u("collection_collections"):                                  f.collections,
		u("ciphers_ciphers"):                                         f.ciphers,
		u("vaultTimeoutSettings_vaultTimeout"):                       "onLocked",
		u("sharedUnlockSettings_allowSharingUnlockStateWithDesktop"): false,
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		panic(err)
	}
	return b
}

// Write writes the data file in a temporary directory of t, returning its
// path.
func (f *Fixture) Write(t testing.TB) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(p, f.JSON(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// Sample returns an account with a few items of every kind, as a person
// might have.
func Sample() *Fixture {
	f := New()
	work := f.Folder("Work")
	f.Folder("Personal")
	eng := f.Collection("Engineering")
	m := func(n int) *int { return &n }
	f.Add(Item{Type: bw.TypeLogin, Name: "GitHub", Favorite: true, FolderID: work, Login: &Login{
		Username: "ada", Password: "gh-s3cret!", TOTP: "JBSWY3DPEHPK3PXP",
		URIs: []bw.URI{{URI: "https://github.com/login"}},
	}}, true)
	f.Add(Item{Type: bw.TypeLogin, Name: "Google", Login: &Login{
		Username: "ada@gmail.com", Password: "g00gle-pass",
		URIs:     []bw.URI{{URI: "https://accounts.google.com"}},
		Passkeys: []bw.Passkey{{RpID: "google.com", RpName: "Google", UserName: "ada@gmail.com"}},
	}}, false)
	f.Add(Item{Type: bw.TypeLogin, Name: "AWS Console", OrganizationID: f.OrgID, CollectionIDs: []string{eng}, Login: &Login{
		Username: "ada.admin", Password: "aws-pa55",
		URIs: []bw.URI{{URI: "https://signin.aws.amazon.com", Match: m(1)}},
	}}, true)
	f.Add(Item{Type: bw.TypeLogin, Name: "Router", Notes: "In the hallway closet", Login: &Login{
		Username: "admin", Password: "router-admin", URIs: []bw.URI{{URI: "192.168.1.1"}},
	}}, false)
	f.Add(Item{Type: bw.TypeCard, Name: "Visa", Favorite: true, Card: &Card{
		CardholderName: "Ada Lovelace", Brand: "Visa", Number: "4111111111111111", ExpMonth: "8", ExpYear: "2029", Code: "123",
	}}, true)
	f.Add(Item{Type: bw.TypeSecureNote, Name: "Wi-Fi", Notes: "Network: Analytical\nPassword: engine-1843"}, false)
	f.Add(Item{Type: bw.TypeIdentity, Name: "Ada Lovelace", Identity: &Identity{
		FirstName: "Ada", LastName: "Lovelace", Email: "ada@example.com", Phone: "+44 20 7946 0000", City: "London", Country: "GB",
	}}, false)
	f.Add(Item{Type: bw.TypeSSHKey, Name: "Deploy key", OrganizationID: f.OrgID, CollectionIDs: []string{eng}, SSHKey: &SSHKey{
		PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\n-----END OPENSSH PRIVATE KEY-----",
		PublicKey:  "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIA deploy", Fingerprint: "SHA256:4k3yF1ngerPr1nt",
	}}, true)
	f.Add(Item{Type: bw.TypeLogin, Name: "Old account", Deleted: true, Login: &Login{Username: "gone"}}, false)
	return f
}
