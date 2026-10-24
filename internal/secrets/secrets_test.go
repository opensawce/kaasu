package secrets

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/testutil"
)

func key(b byte) MasterKey { return MasterKey(bytes.Repeat([]byte{b}, 32)) }

func TestSealOpenRoundTrip(t *testing.T) {
	k := key(1)
	plain := []byte("imap-password-123")
	a, err := k.Seal(plain, []byte("imap"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := k.Seal(plain, []byte("imap"))
	if a == b {
		t.Error("two seals of the same plaintext must differ (random nonce and data key)")
	}
	if strings.Contains(a, "imap-password") || strings.Contains(a, base64.StdEncoding.EncodeToString(plain)) {
		t.Error("blob leaks plaintext")
	}
	got, err := k.Open(a, []byte("imap"))
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestOpenRejectsWrongKeyAADAndTamper(t *testing.T) {
	blob, _ := key(1).Seal([]byte("s3cret"), []byte("webhook"))
	if _, err := key(2).Open(blob, []byte("webhook")); err == nil {
		t.Error("wrong master key must fail")
	}
	if _, err := key(1).Open(blob, []byte("imap")); err == nil {
		t.Error("wrong AAD (secret moved to another name) must fail")
	}
	raw, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(blob, "v1."))
	raw[len(raw)-1] ^= 1
	if _, err := key(1).Open("v1."+base64.RawURLEncoding.EncodeToString(raw), []byte("webhook")); err == nil {
		t.Error("tampered ciphertext must fail")
	}
	if _, err := key(1).Open("garbage", nil); err == nil {
		t.Error("garbage must fail")
	}
}

func TestRewrapMovesToNewKey(t *testing.T) {
	old, nu := key(1), key(2)
	blob, _ := old.Seal([]byte("ai-key"), []byte("ai"))
	re, err := old.Rewrap(blob, nu)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := nu.Open(re, []byte("ai")); err != nil || string(got) != "ai-key" {
		t.Fatalf("new key cannot open: %v", err)
	}
	if _, err := old.Open(re, []byte("ai")); err == nil {
		t.Error("old key must no longer open rewrapped blob")
	}
}

func TestParseMasterKey(t *testing.T) {
	good := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	if _, err := ParseMasterKey(good); err != nil {
		t.Errorf("good key rejected: %v", err)
	}
	for _, bad := range []string{"", "not base64!", base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, err := ParseMasterKey(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestLoadMasterKeyPrecedence(t *testing.T) {
	envKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	fileKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	kcKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "kaasu-master-key"), []byte(fileKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	keychain := func() (string, error) { return kcKey, nil }
	noKeychain := func() (string, error) { return "", os.ErrNotExist }
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

	got, src, err := LoadMasterKey(env(map[string]string{"KAASU_MASTER_KEY": envKey, "CREDENTIALS_DIRECTORY": dir}), keychain)
	if err != nil || src != "env" || got[0] != 1 {
		t.Errorf("env should win: src=%s err=%v", src, err)
	}
	got, src, err = LoadMasterKey(env(map[string]string{"CREDENTIALS_DIRECTORY": dir}), keychain)
	if err != nil || src != "systemd" || got[0] != 2 {
		t.Errorf("systemd next: src=%s err=%v", src, err)
	}
	got, src, err = LoadMasterKey(env(nil), keychain)
	if err != nil || src != "keychain" || got[0] != 3 {
		t.Errorf("keychain last: src=%s err=%v", src, err)
	}
	_, _, err = LoadMasterKey(env(nil), noKeychain)
	if err == nil || !strings.Contains(err.Error(), "KAASU_MASTER_KEY") {
		t.Errorf("no key anywhere should explain the three sources, got %v", err)
	}
}

func newVault(t *testing.T, k MasterKey) *Vault {
	t.Helper()
	return NewVault(testutil.OpenStore(t), k, clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))
}

func TestVaultStoresNoPlaintext(t *testing.T) {
	v := newVault(t, key(1))
	ctx := context.Background()
	if err := v.Put(ctx, "imap_password", []byte("hunter2")); err != nil {
		t.Fatal(err)
	}
	var blob string
	if err := v.st.Read().QueryRow(`SELECT blob FROM secrets WHERE name='imap_password'`).Scan(&blob); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(blob, "hunter2") {
		t.Error("plaintext in database")
	}
	got, err := v.Get(ctx, "imap_password")
	if err != nil || string(got) != "hunter2" {
		t.Fatalf("got %q err=%v", got, err)
	}
	if _, err := v.Get(ctx, "missing"); err != ErrNotFound {
		t.Errorf("want ErrNotFound, got %v", err)
	}
	if err := v.Put(ctx, "imap_password", []byte("new")); err != nil {
		t.Fatal(err)
	}
	if got, _ := v.Get(ctx, "imap_password"); string(got) != "new" {
		t.Error("put should overwrite")
	}
}

func TestRotateAllIsAtomic(t *testing.T) {
	v := newVault(t, key(1))
	ctx := context.Background()
	for _, n := range []string{"a", "b", "c"} {
		if err := v.Put(ctx, n, []byte("val-"+n)); err != nil {
			t.Fatal(err)
		}
	}
	// Corrupt one row: rotation must fail and change nothing.
	_ = v.st.Tx(ctx, func(tx *sql.Tx) error { _, e := tx.Exec(`UPDATE secrets SET blob='v1.AAAA' WHERE name='b'`); return e })
	if n, err := v.RotateAll(ctx, key(2)); err == nil || n != 0 {
		t.Fatalf("rotation over a corrupt row should fail with 0 changed, got n=%d err=%v", n, err)
	}
	if got, err := v.Get(ctx, "a"); err != nil || string(got) != "val-a" {
		t.Errorf("row a changed by failed rotation: %q %v", got, err)
	}
	// Repair and rotate for real.
	_ = v.Put(ctx, "b", []byte("val-b"))
	n, err := v.RotateAll(ctx, key(2))
	if err != nil || n != 3 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if got, err := v.Get(ctx, "c"); err != nil || string(got) != "val-c" {
		t.Errorf("after rotation Get uses new key: %q %v", got, err)
	}
}

func TestAPIKeys(t *testing.T) {
	v := newVault(t, key(1))
	ctx := context.Background()
	plain, rec, err := v.NewAPIKey(ctx, "WooCommerce")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(plain, "kaasu_live_") || len(plain) < 40 {
		t.Errorf("odd key %q", plain)
	}
	var hash string
	_ = v.st.Read().QueryRow(`SELECT key_hash FROM api_keys WHERE id=?`, rec.ID).Scan(&hash)
	if hash == "" || strings.Contains(hash, plain) || len(hash) != 64 {
		t.Errorf("must store a sha-256 hex hash, got %q", hash)
	}
	got, err := v.VerifyAPIKey(ctx, plain)
	if err != nil || got.ID != rec.ID || got.Label != "WooCommerce" {
		t.Fatalf("verify: %+v %v", got, err)
	}
	if _, err := v.VerifyAPIKey(ctx, plain+"x"); err != ErrInvalidKey {
		t.Errorf("wrong key: %v", err)
	}
	if err := v.RevokeAPIKey(ctx, rec.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := v.VerifyAPIKey(ctx, plain); err != ErrInvalidKey {
		t.Errorf("revoked key must fail: %v", err)
	}
}

func TestSecretAndKeyChangesAreLedgeredWithoutValues(t *testing.T) {
	v := newVault(t, key(1))
	ctx := context.Background()
	_ = v.Put(ctx, "imap_password", []byte("hunter2"))
	plain, rec, _ := v.NewAPIKey(ctx, "Woo")
	_ = v.RevokeAPIKey(ctx, rec.ID)
	_, _ = v.RotateAll(ctx, key(2))
	rows, err := v.st.Read().Query(`SELECT kind, payload FROM ledger ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var kinds []string
	for rows.Next() {
		var k, p string
		_ = rows.Scan(&k, &p)
		kinds = append(kinds, k)
		if strings.Contains(p, "hunter2") || strings.Contains(p, plain) {
			t.Errorf("ledger payload leaks a secret: %s", p)
		}
	}
	want := "[secret.updated apikey.created apikey.revoked secret.master_key_rotated]"
	if fmt.Sprint(kinds) != want {
		t.Errorf("kinds=%v want %s", kinds, want)
	}
}
