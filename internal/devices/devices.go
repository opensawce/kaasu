// Package devices pairs phones and mail sources with Kaasu and checks that their requests are current.
package devices

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/id"
	"github.com/opensawce/kaasu/internal/ledger"
	"github.com/opensawce/kaasu/internal/store"
)

var (
	ErrNotFound  = errors.New("device not found")
	ErrBadToken  = errors.New("pairing token is invalid, expired or already used")
	ErrInvalid   = errors.New("invalid device")
	ErrRevoked   = errors.New("device is revoked")
	pairValidity = 10 * time.Minute
	validKinds   = map[string]bool{"android": true, "apple": true, "windows": true, "email": true}
)

type Device struct {
	ID         string
	Label      string
	Kind       string
	PublicKey  ed25519.PublicKey
	PairedAt   string
	LastSeenAt string
	RevokedAt  string
}

type Service struct {
	st          *store.Store
	clk         clock.Clock
	pairingHost string
}

// New makes the device service. pairingHost is the public host that goes into pairing links.
func New(st *store.Store, clk clock.Clock, pairingHost string) *Service {
	return &Service{st: st, clk: clk, pairingHost: pairingHost}
}

func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// NewPairing makes a single-use token valid for ten minutes and returns the payload for the QR code.
// Only the token's hash is stored.
func (s *Service) NewPairing(ctx context.Context) (token, payload string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	now := s.clk.Now().UTC()
	err = s.st.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO pair_tokens(token_hash, expires_at, created_at) VALUES(?,?,?)`,
			tokenHash(token), now.Add(pairValidity).Format(time.RFC3339), now.Format(time.RFC3339))
		return err
	})
	if err != nil {
		return "", "", err
	}
	q := url.Values{"host": {s.pairingHost}, "token": {token}}
	return token, "kaasu://pair?" + q.Encode(), nil
}

// Pair turns a token into a device. The token works once. The public key is 32 bytes of Ed25519.
func (s *Service) Pair(ctx context.Context, token string, pub ed25519.PublicKey, label, kind string) (Device, error) {
	if len(pub) != ed25519.PublicKeySize || label == "" || !validKinds[kind] {
		return Device{}, ErrInvalid
	}
	now := s.clk.Now().UTC()
	d := Device{ID: id.New("dev"), Label: label, Kind: kind, PublicKey: pub, PairedAt: now.Format(time.RFC3339)}
	err := s.st.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE pair_tokens SET used_at = ? WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?`,
			now.Format(time.RFC3339), tokenHash(token), now.Format(time.RFC3339))
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrBadToken
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO devices(id,label,kind,public_key,paired_at) VALUES(?,?,?,?,?)`,
			d.ID, d.Label, d.Kind, []byte(pub), d.PairedAt); err != nil {
			return err
		}
		_, err = ledger.Append(ctx, tx, s.clk, ledger.Entry{Kind: "device.paired", Payload: map[string]any{"device_id": d.ID, "kind": kind}})
		return err
	})
	return d, err
}

// Authenticate returns the device for an id only if it is paired and not revoked.
func (s *Service) Authenticate(ctx context.Context, deviceID string) (Device, error) {
	var d Device
	var key []byte
	var lastSeen, revoked sql.NullString
	err := s.st.Read().QueryRowContext(ctx, `SELECT id,label,kind,public_key,paired_at,last_seen_at,revoked_at FROM devices WHERE id = ?`, deviceID).
		Scan(&d.ID, &d.Label, &d.Kind, &key, &d.PairedAt, &lastSeen, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	if err != nil {
		return Device{}, err
	}
	if revoked.Valid {
		return Device{}, ErrRevoked
	}
	d.PublicKey = key
	d.LastSeenAt = lastSeen.String
	return d, nil
}

// Revoke stops a device at once. Its next request is refused.
func (s *Service) Revoke(ctx context.Context, deviceID string) error {
	return s.st.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE devices SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
			s.clk.Now().UTC().Format(time.RFC3339), deviceID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		_, err = ledger.Append(ctx, tx, s.clk, ledger.Entry{Kind: "device.revoked", Payload: map[string]any{"device_id": deviceID}})
		return err
	})
}

// TouchSeen records the last accepted request.
func (s *Service) TouchSeen(ctx context.Context, tx *sql.Tx, deviceID string) error {
	_, err := tx.ExecContext(ctx, `UPDATE devices SET last_seen_at = ? WHERE id = ?`, s.clk.Now().UTC().Format(time.RFC3339), deviceID)
	if err != nil {
		return fmt.Errorf("touch device: %w", err)
	}
	return nil
}

// List returns every device, newest first.
func (s *Service) List(ctx context.Context) ([]Device, error) {
	rows, err := s.st.Read().QueryContext(ctx, `SELECT id,label,kind,public_key,paired_at,COALESCE(last_seen_at,''),COALESCE(revoked_at,'') FROM devices ORDER BY paired_at DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var d Device
		var key []byte
		if err := rows.Scan(&d.ID, &d.Label, &d.Kind, &key, &d.PairedAt, &d.LastSeenAt, &d.RevokedAt); err != nil {
			return nil, err
		}
		d.PublicKey = key
		out = append(out, d)
	}
	return out, rows.Err()
}
