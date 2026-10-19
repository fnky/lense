package bw_test

import (
	"errors"
	"testing"

	"bitwarden-mini/internal/bw"
	"bitwarden-mini/internal/bw/bwtest"
)

func TestUnlockAndDecrypt(t *testing.T) {
	f := bwtest.Sample()
	s, err := bw.Open(f.Write(t))
	if err != nil {
		t.Fatal(err)
	}
	accs := s.Accounts()
	if len(accs) != 1 || accs[0].Email != f.Email || !accs[0].Active || !accs[0].BiometricUnlock || accs[0].Server.Host() != "bitwarden.com" {
		t.Fatalf("accounts %+v", accs)
	}
	if _, err := s.UnlockWithPassword(f.UserID, "wrong password"); !errors.Is(err, bw.ErrWrongPassword) {
		t.Fatalf("wrong password: %v", err)
	}
	key, err := s.UnlockWithPassword(f.UserID, f.Password)
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.Decrypt(f.UserID, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Items) != 8 {
		t.Fatalf("%d items, the deleted one included?", len(v.Items))
	}
	for _, it := range v.Items {
		if it.Err != nil {
			t.Errorf("%s: %v", it.Name, it.Err)
		}
	}
	gh := find(v, "GitHub")
	if gh == nil || gh.Login.Username != "ada" || gh.Login.Password != "gh-s3cret!" || gh.Login.TOTP != "JBSWY3DPEHPK3PXP" || !gh.Favorite {
		t.Errorf("GitHub %+v", gh)
	}
	if v.FolderName(gh.FolderID) != "Work" {
		t.Errorf("folder %q", v.FolderName(gh.FolderID))
	}
	aws := find(v, "AWS Console")
	if aws == nil || aws.OrganizationID != f.OrgID || aws.Login.Password != "aws-pa55" || v.CollectionName(aws.CollectionIDs[0]) != "Engineering" {
		t.Errorf("AWS %+v", aws)
	}
	if v.OrganizationName(f.OrgID) != "Acme Inc." {
		t.Errorf("organizations %+v", v.Organizations)
	}
	if c := find(v, "Visa"); c == nil || c.Card.Number != "4111111111111111" {
		t.Errorf("card %+v", c)
	}
	if g := find(v, "Google"); g == nil || len(g.Login.Passkeys) != 1 || g.Login.Passkeys[0].RpID != "google.com" {
		t.Errorf("passkeys %+v", g)
	}
	if k := find(v, "Deploy key"); k == nil || k.SSHKey.Fingerprint != "SHA256:4k3yF1ngerPr1nt" {
		t.Errorf("ssh key %+v", k)
	}
}

func TestDecryptWithWrongKey(t *testing.T) {
	f := bwtest.Sample()
	s, _ := bw.Open(f.Write(t))
	if _, err := s.Decrypt(f.UserID, bw.GenerateSymmetricKey()); err == nil {
		t.Fatal("a wrong user key decrypted the vault")
	}
}

func TestServerOfAccount(t *testing.T) {
	f := bwtest.New()
	f.Server = bw.Server{Region: bw.RegionSelfHosted, URL: "https://vault.example.org"}
	s, _ := bw.Open(f.Write(t))
	a, ok := s.FindAccount("ADA@example.com")
	if !ok || a.Server.Host() != "vault.example.org" {
		t.Fatalf("account %+v", a)
	}
	if a.Server.Same(bw.Server{Region: bw.RegionUS}) || !a.Server.Same(bw.Server{Region: bw.RegionSelfHosted, URL: "https://VAULT.example.org/"}) {
		t.Error("Same")
	}
}

func TestOpenMissing(t *testing.T) {
	if _, err := bw.Open(t.TempDir() + "/data.json"); !errors.Is(err, bw.ErrNoData) {
		t.Fatalf("err %v", err)
	}
}

func find(v *bw.Vault, name string) *bw.Item {
	for _, it := range v.Items {
		if it.Name == name {
			return it
		}
	}
	return nil
}
