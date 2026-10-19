package bw

import (
	"bytes"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ItemType is the kind of an item, as Bitwarden numbers them.
type ItemType int

const (
	TypeLogin      ItemType = 1
	TypeSecureNote ItemType = 2
	TypeCard       ItemType = 3
	TypeIdentity   ItemType = 4
	TypeSSHKey     ItemType = 5
)

func (t ItemType) String() string {
	switch t {
	case TypeLogin:
		return "Login"
	case TypeSecureNote:
		return "Secure Note"
	case TypeCard:
		return "Card"
	case TypeIdentity:
		return "Identity"
	case TypeSSHKey:
		return "SSH Key"
	}
	return "Item"
}

// Item is a decrypted item of the vault: a cipher, in Bitwarden's words.
type Item struct {
	ID             string
	Type           ItemType
	Name           string
	Notes          string
	OrganizationID string // empty in the personal vault
	FolderID       string
	CollectionIDs  []string
	Favorite       bool
	Reprompt       bool // asks for the master password before showing secrets
	Edit           bool // the user may change it
	ViewPassword   bool
	Login          *Login
	Card           *Card
	Identity       *Identity
	SSHKey         *SSHKey
	Fields         []Field
	Revised        time.Time
	Created        time.Time
	Deleted        bool
	Archived       bool
	Err            error // a part of it could not be decrypted
}

type Login struct {
	Username string
	Password string
	TOTP     string
	URIs     []URI
	Passkeys []Passkey
}

// URI is a website or app of a login, with how it matches: nil for the
// default, else 0 base domain, 1 host, 2 starts with, 3 exact, 4 regular
// expression, 5 never.
type URI struct {
	URI   string
	Match *int
}

type Passkey struct {
	RpID     string
	RpName   string
	UserName string
	Created  time.Time
}

type Card struct {
	CardholderName string
	Brand          string
	Number         string
	ExpMonth       string
	ExpYear        string
	Code           string
}

type Identity struct {
	Title, FirstName, MiddleName, LastName string
	Address1, Address2, Address3           string
	City, State, PostalCode, Country       string
	Company, Email, Phone, SSN, Username   string
	PassportNumber, LicenseNumber          string
}

// FullName joins the parts of the identity's name.
func (i *Identity) FullName() string {
	var parts []string
	for _, p := range []string{i.Title, i.FirstName, i.MiddleName, i.LastName} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " ")
}

type SSHKey struct {
	PrivateKey  string
	PublicKey   string
	Fingerprint string
}

// FieldType is the kind of a custom field.
type FieldType int

const (
	FieldText    FieldType = 0
	FieldHidden  FieldType = 1
	FieldBoolean FieldType = 2
	FieldLinked  FieldType = 3
)

type Field struct {
	Type  FieldType
	Name  string
	Value string
}

// Folder is a folder of the personal vault.
type Folder struct {
	ID   string
	Name string
}

// Organization is a vault shared with others.
type Organization struct {
	ID   string
	Name string
}

// Collection groups items of an organization.
type Collection struct {
	ID             string
	OrganizationID string
	Name           string
	ReadOnly       bool
}

// Vault is the decrypted vault of an account, and its keys, for changing
// it.
type Vault struct {
	UserID        string
	Items         []*Item
	Folders       []Folder
	Organizations []Organization
	Collections   []Collection
	Synced        time.Time

	userKey SymmetricKey
	orgKeys map[string]SymmetricKey
}

// Lock forgets the keys of the vault.
func (v *Vault) Lock() {
	if v == nil {
		return
	}
	v.userKey.Wipe()
	for _, k := range v.orgKeys {
		k.Wipe()
	}
	v.orgKeys = nil
}

// UserKey returns the key of the personal vault.
func (v *Vault) UserKey() SymmetricKey { return v.userKey }

// OrganizationKey returns the key of an organization's vault.
func (v *Vault) OrganizationKey(id string) (SymmetricKey, bool) {
	k, ok := v.orgKeys[id]
	return k, ok
}

// Item returns the item with id, or nil.
func (v *Vault) Item(id string) *Item {
	for _, it := range v.Items {
		if it.ID == id {
			return it
		}
	}
	return nil
}

// FolderName returns the name of a folder, or "".
func (v *Vault) FolderName(id string) string {
	for _, f := range v.Folders {
		if f.ID == id {
			return f.Name
		}
	}
	return ""
}

// OrganizationName returns the name of an organization, or "".
func (v *Vault) OrganizationName(id string) string {
	for _, o := range v.Organizations {
		if o.ID == id {
			return o.Name
		}
	}
	return ""
}

// CollectionName returns the name of a collection, or "".
func (v *Vault) CollectionName(id string) string {
	for _, c := range v.Collections {
		if c.ID == id {
			return c.Name
		}
	}
	return ""
}

// The ciphers as the desktop app stores them: every string encrypted.
type cipherData struct {
	ID             string   `json:"id"`
	OrganizationID string   `json:"organizationId"`
	FolderID       string   `json:"folderId"`
	Edit           *bool    `json:"edit"`
	ViewPassword   *bool    `json:"viewPassword"`
	Favorite       bool     `json:"favorite"`
	Type           ItemType `json:"type"`
	Name           string   `json:"name"`
	Notes          string   `json:"notes"`
	Key            string   `json:"key"`
	Reprompt       int      `json:"reprompt"`
	CollectionIDs  []string `json:"collectionIds"`
	RevisionDate   string   `json:"revisionDate"`
	CreationDate   string   `json:"creationDate"`
	DeletedDate    string   `json:"deletedDate"`
	ArchivedDate   string   `json:"archivedDate"`
	Login          *struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TOTP     string `json:"totp"`
		URIs     []struct {
			URI   string `json:"uri"`
			Match *int   `json:"match"`
		} `json:"uris"`
		Fido2Credentials []struct {
			RpID         string `json:"rpId"`
			RpName       string `json:"rpName"`
			UserName     string `json:"userName"`
			CreationDate string `json:"creationDate"`
		} `json:"fido2Credentials"`
	} `json:"login"`
	Card *struct {
		CardholderName string `json:"cardholderName"`
		Brand          string `json:"brand"`
		Number         string `json:"number"`
		ExpMonth       string `json:"expMonth"`
		ExpYear        string `json:"expYear"`
		Code           string `json:"code"`
	} `json:"card"`
	Identity *map[string]string `json:"identity"`
	SSHKey   *struct {
		PrivateKey     string `json:"privateKey"`
		PublicKey      string `json:"publicKey"`
		KeyFingerprint string `json:"keyFingerprint"`
	} `json:"sshKey"`
	Fields []struct {
		Type  FieldType `json:"type"`
		Name  string    `json:"name"`
		Value string    `json:"value"`
	} `json:"fields"`
}

// Decrypt decrypts the vault of the account with its user key. Items that
// fail to decrypt in part keep the error in Err; a wrong user key fails it
// all.
func (s *Store) Decrypt(userID string, key SymmetricKey) (*Vault, error) {
	if _, ok := s.Account(userID); !ok {
		return nil, ErrNoAccount
	}
	if err := s.CheckUserKey(userID, key); err != nil {
		return nil, fmt.Errorf("bw: the key does not unlock this account: %w", err)
	}
	v := &Vault{UserID: userID, userKey: SymmetricKey{Enc: bytes.Clone(key.Enc), MAC: bytes.Clone(key.MAC)}, orgKeys: map[string]SymmetricKey{}}
	var last string
	if s.get(userKey(userID, "sync_lastSync"), &last) {
		v.Synced, _ = time.Parse(time.RFC3339Nano, last)
	}

	// Organizations, and their keys, encrypted to the account's RSA key.
	var priv *rsa.PrivateKey
	if enc := s.privateKey(userID); enc != "" {
		priv, _ = DecryptPrivateKey(key, enc)
	}
	var orgKeys map[string]json.RawMessage
	s.get(userKey(userID, "crypto_organizationKeys"), &orgKeys)
	for id, raw := range orgKeys {
		var enc string
		var obj struct {
			Type string `json:"type"`
			Key  string `json:"key"`
		}
		if json.Unmarshal(raw, &obj) == nil && obj.Key != "" {
			if obj.Type != "" && obj.Type != "organization" {
				continue // through a provider, whose keys are not read
			}
			enc = obj.Key
		} else if json.Unmarshal(raw, &enc) != nil {
			continue
		}
		if priv == nil {
			continue
		}
		b, err := DecryptRSA(priv, enc)
		if err != nil {
			continue
		}
		if k, err := NewSymmetricKey(b); err == nil {
			v.orgKeys[id] = k
		}
		clear(b)
	}
	var orgs map[string]struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Enabled *bool  `json:"enabled"`
	}
	s.get(userKey(userID, "organizations_organizations"), &orgs)
	for id, o := range orgs {
		if o.Enabled != nil && !*o.Enabled {
			continue
		}
		v.Organizations = append(v.Organizations, Organization{ID: id, Name: o.Name})
	}
	sort.Slice(v.Organizations, func(i, j int) bool {
		return strings.ToLower(v.Organizations[i].Name) < strings.ToLower(v.Organizations[j].Name)
	})

	var folders map[string]struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	s.get(userKey(userID, "folder_folders"), &folders)
	for id, f := range folders {
		name, err := key.DecryptString(f.Name)
		if err != nil {
			name = "[error: cannot decrypt]"
		}
		v.Folders = append(v.Folders, Folder{ID: id, Name: name})
	}
	sort.Slice(v.Folders, func(i, j int) bool { return strings.ToLower(v.Folders[i].Name) < strings.ToLower(v.Folders[j].Name) })

	var cols map[string]struct {
		ID             string `json:"id"`
		OrganizationID string `json:"organizationId"`
		Name           string `json:"name"`
		ReadOnly       bool   `json:"readOnly"`
	}
	s.get(userKey(userID, "collection_collections"), &cols)
	for id, c := range cols {
		k, ok := v.orgKeys[c.OrganizationID]
		if !ok {
			continue
		}
		name, err := k.DecryptString(c.Name)
		if err != nil {
			name = "[error: cannot decrypt]"
		}
		v.Collections = append(v.Collections, Collection{ID: id, OrganizationID: c.OrganizationID, Name: name, ReadOnly: c.ReadOnly})
	}
	sort.Slice(v.Collections, func(i, j int) bool {
		return strings.ToLower(v.Collections[i].Name) < strings.ToLower(v.Collections[j].Name)
	})

	var ciphers map[string]cipherData
	s.get(userKey(userID, "ciphers_ciphers"), &ciphers)
	for _, c := range ciphers {
		if c.DeletedDate != "" {
			continue // in the trash
		}
		v.Items = append(v.Items, v.decryptCipher(c))
	}
	sort.Slice(v.Items, func(i, j int) bool {
		a, b := strings.ToLower(v.Items[i].Name), strings.ToLower(v.Items[j].Name)
		if a != b {
			return a < b
		}
		return v.Items[i].ID < v.Items[j].ID
	})
	return v, nil
}

// ErrNoOrganizationKey means the key of the item's organization is
// unknown, as when the account left it.
var ErrNoOrganizationKey = errors.New("bw: no key for the item's organization")

func (v *Vault) decryptCipher(c cipherData) *Item {
	it := &Item{
		ID: c.ID, Type: c.Type, OrganizationID: c.OrganizationID, FolderID: c.FolderID,
		CollectionIDs: c.CollectionIDs, Favorite: c.Favorite, Reprompt: c.Reprompt == 1,
		Edit: c.Edit == nil || *c.Edit, ViewPassword: c.ViewPassword == nil || *c.ViewPassword,
		Deleted: c.DeletedDate != "", Archived: c.ArchivedDate != "",
	}
	it.Revised, _ = time.Parse(time.RFC3339Nano, c.RevisionDate)
	it.Created, _ = time.Parse(time.RFC3339Nano, c.CreationDate)

	key := v.userKey
	if c.OrganizationID != "" {
		k, ok := v.orgKeys[c.OrganizationID]
		if !ok {
			it.Name, it.Err = "[error: cannot decrypt]", ErrNoOrganizationKey
			return it
		}
		key = k
	}
	if c.Key != "" {
		// The item has a key of its own, which its key encrypts.
		e, err := ParseEncString(c.Key)
		var b []byte
		if err == nil {
			b, err = key.Decrypt(e)
		}
		if err == nil {
			key, err = NewSymmetricKey(b)
			clear(b)
		}
		if err != nil {
			it.Name, it.Err = "[error: cannot decrypt]", err
			return it
		}
		defer key.Wipe()
	}
	dec := func(s string) string {
		if s == "" {
			return ""
		}
		out, err := key.DecryptString(s)
		if err != nil {
			if it.Err == nil {
				it.Err = err
			}
			return "[error: cannot decrypt]"
		}
		return out
	}

	it.Name = dec(c.Name)
	it.Notes = dec(c.Notes)
	if l := c.Login; l != nil {
		it.Login = &Login{Username: dec(l.Username), Password: dec(l.Password), TOTP: dec(l.TOTP)}
		for _, u := range l.URIs {
			it.Login.URIs = append(it.Login.URIs, URI{URI: dec(u.URI), Match: u.Match})
		}
		for _, p := range l.Fido2Credentials {
			pk := Passkey{RpID: dec(p.RpID), RpName: dec(p.RpName), UserName: dec(p.UserName)}
			pk.Created, _ = time.Parse(time.RFC3339Nano, p.CreationDate)
			it.Login.Passkeys = append(it.Login.Passkeys, pk)
		}
	}
	if cd := c.Card; cd != nil {
		it.Card = &Card{
			CardholderName: dec(cd.CardholderName), Brand: dec(cd.Brand), Number: dec(cd.Number),
			ExpMonth: dec(cd.ExpMonth), ExpYear: dec(cd.ExpYear), Code: dec(cd.Code),
		}
	}
	if c.Identity != nil {
		m := *c.Identity
		g := func(k string) string { return dec(m[k]) }
		it.Identity = &Identity{
			Title: g("title"), FirstName: g("firstName"), MiddleName: g("middleName"), LastName: g("lastName"),
			Address1: g("address1"), Address2: g("address2"), Address3: g("address3"),
			City: g("city"), State: g("state"), PostalCode: g("postalCode"), Country: g("country"),
			Company: g("company"), Email: g("email"), Phone: g("phone"), SSN: g("ssn"), Username: g("username"),
			PassportNumber: g("passportNumber"), LicenseNumber: g("licenseNumber"),
		}
	}
	if k := c.SSHKey; k != nil {
		it.SSHKey = &SSHKey{PrivateKey: dec(k.PrivateKey), PublicKey: dec(k.PublicKey), Fingerprint: dec(k.KeyFingerprint)}
	}
	for _, f := range c.Fields {
		it.Fields = append(it.Fields, Field{Type: f.Type, Name: dec(f.Name), Value: dec(f.Value)})
	}
	return it
}
