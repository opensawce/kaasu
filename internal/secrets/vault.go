package secrets

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/id"
	"github.com/opensawce/kaasu/internal/store"
)

var (
	ErrNotFound   = errors.New("secret not found")
	ErrInvalidKey = errors.New("invalid or revoked API key")
)

// Vault stores encrypted secrets and API key hashes in the database.
type Vault struct {
	st  *store.Store
	clk clock.Clock
	mu  sync.RWMutex
	key MasterKey
}

func NewVault(st *store.Store, key MasterKey, clk clock.Clock) *Vault {
	return &Vault{st: st, key: key, clk: clk}
}

func (v *Vault) masterKey() MasterKey {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.key
}

func (v *Vault) now() string { return v.clk.Now().UTC().Format(time.RFC3339) }

// Put encrypts and stores a secret under name, replacing any previous value.
func (v *Vault) Put(ctx context.Context, name string, plaintext []byte) error {
	blob, err := v.masterKey().Seal(plaintext, []byte(name))
	if err != nil {
		return fmt.Errorf("seal %s: %w", name, err)
	}
	return v.st.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO secrets(name,blob,updated_at) VALUES(?,?,?)
			ON CONFLICT(name) DO UPDATE SET blob=excluded.blob, updated_at=excluded.updated_at`, name, blob, v.now())
		return err
	})
}

// Get decrypts the secret stored under name.
func (v *Vault) Get(ctx context.Context, name string) ([]byte, error) {
	var blob string
	err := v.st.Read().QueryRowContext(ctx, `SELECT blob FROM secrets WHERE name = ?`, name).Scan(&blob)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return v.masterKey().Open(blob, []byte(name))
}

// RotateAll re-wraps every secret under next in one transaction. It changes nothing if any row fails.
func (v *Vault) RotateAll(ctx context.Context, next MasterKey) (int, error) {
	cur := v.masterKey()
	count := 0
	err := v.st.Tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT name, blob FROM secrets`)
		if err != nil {
			return err
		}
		type row struct{ name, blob string }
		var all []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.name, &r.blob); err != nil {
				_ = rows.Close()
				return err
			}
			all = append(all, r)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, r := range all {
			nb, err := cur.Rewrap(r.blob, next)
			if err != nil {
				return fmt.Errorf("rewrap %s: %w", r.name, err)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE secrets SET blob=?, updated_at=? WHERE name=?`, nb, v.now(), r.name); err != nil {
				return err
			}
		}
		count = len(all)
		return nil
	})
	if err != nil {
		return 0, err
	}
	v.mu.Lock()
	v.key = next
	v.mu.Unlock()
	return count, nil
}

// APIKey is the stored record. The key itself is never stored and cannot be shown again.
type APIKey struct {
	ID, Label, Prefix string
}

var b32 = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

func hashKey(k string) string {
	h := sha256.Sum256([]byte(k))
	return hex.EncodeToString(h[:])
}

// NewAPIKey creates a key and returns it once. Only its SHA-256 hash is stored.
func (v *Vault) NewAPIKey(ctx context.Context, label string) (string, APIKey, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", APIKey{}, err
	}
	plain := "kaasu_live_" + b32.EncodeToString(raw)
	rec := APIKey{ID: id.New("key"), Label: label, Prefix: plain[:15]}
	err := v.st.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO api_keys(id,label,prefix,key_hash,created_at) VALUES(?,?,?,?,?)`,
			rec.ID, rec.Label, rec.Prefix, hashKey(plain), v.now())
		return err
	})
	if err != nil {
		return "", APIKey{}, err
	}
	return plain, rec, nil
}

// VerifyAPIKey looks the key up by its hash (a 256-bit hash, so lookup timing reveals nothing usable).
func (v *Vault) VerifyAPIKey(ctx context.Context, plain string) (APIKey, error) {
	if !strings.HasPrefix(plain, "kaasu_live_") {
		return APIKey{}, ErrInvalidKey
	}
	var k APIKey
	err := v.st.Read().QueryRowContext(ctx,
		`SELECT id,label,prefix FROM api_keys WHERE key_hash = ? AND revoked_at IS NULL`, hashKey(plain)).Scan(&k.ID, &k.Label, &k.Prefix)
	if errors.Is(err, sql.ErrNoRows) {
		return APIKey{}, ErrInvalidKey
	}
	return k, err
}

func (v *Vault) RevokeAPIKey(ctx context.Context, keyID string) error {
	return v.st.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE api_keys SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, v.now(), keyID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrInvalidKey
		}
		return nil
	})
}
