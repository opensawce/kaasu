package testutil

import (
	"context"
	"crypto/ed25519"
	"testing"
)

func TestKeyIsDeterministic(t *testing.T) {
	a, b := Key(1), Key(1)
	if !a.Equal(b) {
		t.Error("same seed must give the same key")
	}
	if a.Equal(Key(2)) {
		t.Error("different seeds must differ")
	}
	msg := []byte("hello")
	if !ed25519.Verify(a.Public().(ed25519.PublicKey), msg, ed25519.Sign(a, msg)) {
		t.Error("key does not sign")
	}
}

func TestSeqIDs(t *testing.T) {
	s := &Seq{}
	if got := s.New("ord"); got != "ord_000001" {
		t.Errorf("got %q", got)
	}
	if got := s.New("evt"); got != "evt_000002" {
		t.Errorf("got %q", got)
	}
}

func TestOpenStoreIsMigrated(t *testing.T) {
	s := OpenStore(t)
	var n int
	if err := s.Read().QueryRowContext(context.Background(), `SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil || n == 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestGoldenRoundTrip(t *testing.T) {
	Golden(t, "greeting", []byte("hello\n"))
}
