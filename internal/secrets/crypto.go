// Package secrets protects stored secrets with envelope encryption and issues API keys.
//
// Each secret gets its own random data key. The data key is wrapped by the master key, which never
// lives in the database. Rotating the master key re-wraps data keys without touching the data.
package secrets

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
)

const (
	blobPrefix = "v1."
	keyIDLen   = 8
)

// MasterKey is the 32-byte key that wraps data keys.
type MasterKey []byte

// ParseMasterKey decodes a base64 (std or URL) 32-byte key.
func ParseMasterKey(s string) (MasterKey, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("master key is empty")
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		b, err = base64.URLEncoding.DecodeString(s)
	}
	if err != nil {
		return nil, fmt.Errorf("master key is not valid base64: %w", err)
	}
	if len(b) != chacha20poly1305.KeySize {
		return nil, fmt.Errorf("master key must be %d bytes, got %d", chacha20poly1305.KeySize, len(b))
	}
	return MasterKey(b), nil
}

func (k MasterKey) id() []byte {
	h := sha256.Sum256(append([]byte("kaasu-key-id:"), k...))
	return h[:keyIDLen]
}

func (k MasterKey) aead() (cipherAEAD, error) { return chacha20poly1305.NewX(k) }

type cipherAEAD interface {
	Seal(dst, nonce, plaintext, additionalData []byte) []byte
	Open(dst, nonce, ciphertext, additionalData []byte) ([]byte, error)
	NonceSize() int
	Overhead() int
}

var dekAAD = []byte("kaasu:dek")

// layout: keyID | wrapNonce | wrappedDEK | dataNonce | ciphertext
func random(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("secrets: crypto/rand failed: " + err.Error())
	}
	return b
}

// Seal encrypts plaintext. aad (for example the secret's name) is bound to the ciphertext.
func (k MasterKey) Seal(plaintext, aad []byte) (string, error) {
	kek, err := k.aead()
	if err != nil {
		return "", err
	}
	dek := random(chacha20poly1305.KeySize)
	data, err := chacha20poly1305.NewX(dek)
	if err != nil {
		return "", err
	}
	wrapNonce, dataNonce := random(kek.NonceSize()), random(data.NonceSize())
	var out bytes.Buffer
	out.Write(k.id())
	out.Write(wrapNonce)
	out.Write(kek.Seal(nil, wrapNonce, dek, dekAAD))
	out.Write(dataNonce)
	out.Write(data.Seal(nil, dataNonce, plaintext, aad))
	return blobPrefix + base64.RawURLEncoding.EncodeToString(out.Bytes()), nil
}

type parsed struct {
	keyID, wrapNonce, wrapped, dataNonce, ct []byte
}

func parse(blob string) (parsed, error) {
	var p parsed
	if !strings.HasPrefix(blob, blobPrefix) {
		return p, errors.New("not a kaasu secret blob")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(blob, blobPrefix))
	if err != nil {
		return p, fmt.Errorf("bad secret blob encoding: %w", err)
	}
	n := chacha20poly1305.NonceSizeX
	wrapped := chacha20poly1305.KeySize + chacha20poly1305.Overhead
	if len(raw) < keyIDLen+n+wrapped+n+chacha20poly1305.Overhead {
		return p, errors.New("secret blob too short")
	}
	p.keyID, raw = raw[:keyIDLen], raw[keyIDLen:]
	p.wrapNonce, raw = raw[:n], raw[n:]
	p.wrapped, raw = raw[:wrapped], raw[wrapped:]
	p.dataNonce, p.ct = raw[:n], raw[n:]
	return p, nil
}

func (k MasterKey) unwrap(p parsed) ([]byte, error) {
	if !bytes.Equal(p.keyID, k.id()) {
		return nil, errors.New("secret was sealed with a different master key")
	}
	kek, err := k.aead()
	if err != nil {
		return nil, err
	}
	dek, err := kek.Open(nil, p.wrapNonce, p.wrapped, dekAAD)
	if err != nil {
		return nil, errors.New("cannot unwrap data key (wrong master key or corrupted)")
	}
	return dek, nil
}

// Open decrypts a blob made by Seal with the same aad.
func (k MasterKey) Open(blob string, aad []byte) ([]byte, error) {
	p, err := parse(blob)
	if err != nil {
		return nil, err
	}
	dek, err := k.unwrap(p)
	if err != nil {
		return nil, err
	}
	data, err := chacha20poly1305.NewX(dek)
	if err != nil {
		return nil, err
	}
	pt, err := data.Open(nil, p.dataNonce, p.ct, aad)
	if err != nil {
		return nil, errors.New("cannot decrypt secret (tampered, or bound to another name)")
	}
	return pt, nil
}

// Rewrap moves a blob to another master key without decrypting the data.
func (k MasterKey) Rewrap(blob string, to MasterKey) (string, error) {
	p, err := parse(blob)
	if err != nil {
		return "", err
	}
	dek, err := k.unwrap(p)
	if err != nil {
		return "", err
	}
	kek, err := to.aead()
	if err != nil {
		return "", err
	}
	wrapNonce := random(kek.NonceSize())
	var out bytes.Buffer
	out.Write(to.id())
	out.Write(wrapNonce)
	out.Write(kek.Seal(nil, wrapNonce, dek, dekAAD))
	out.Write(p.dataNonce)
	out.Write(p.ct)
	return blobPrefix + base64.RawURLEncoding.EncodeToString(out.Bytes()), nil
}
