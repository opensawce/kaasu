// Package testutil holds helpers shared by tests: deterministic keys and ids, a migrated store, and golden files.
package testutil

import (
	"context"
	"crypto/ed25519"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/opensawce/kaasu/internal/store"
)

var update = flag.Bool("update", false, "rewrite golden files")

// Key returns a deterministic Ed25519 key for a small seed. Test use only.
func Key(seed byte) ed25519.PrivateKey {
	s := make([]byte, ed25519.SeedSize)
	for i := range s {
		s[i] = seed
	}
	return ed25519.NewKeyFromSeed(s)
}

// Seq hands out predictable ids: prefix_000001, prefix_000002, ...
type Seq struct {
	mu sync.Mutex
	n  int
}

func (s *Seq) New(prefix string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	return fmt.Sprintf("%s_%06d", prefix, s.n)
}

// OpenStore returns a migrated SQLite store in a temp dir, closed when the test ends.
func OpenStore(t testing.TB) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "kaasu.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

// Golden compares got with testdata/<name>.golden; run with -update to rewrite it.
func Golden(t testing.TB, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden file %s (run with -update): %v", path, err)
	}
	if string(want) != string(got) {
		t.Errorf("golden mismatch for %s\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}
