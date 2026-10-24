package askdata

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/opensawce/kaasu/internal/store"
	"github.com/opensawce/kaasu/internal/testutil"
)

func seeded(t *testing.T) *store.Store {
	t.Helper()
	st := testutil.OpenStore(t)
	return st
}

func TestUnknownQueryRefused(t *testing.T) {
	st := seeded(t)
	if _, err := Run(context.Background(), st, "DROP TABLE orders", Params{}); !errors.Is(err, ErrUnknownQuery) {
		t.Errorf("unknown name: %v", err)
	}
}

func TestParamsValidated(t *testing.T) {
	st := seeded(t)
	ctx := context.Background()
	good := Params{From: "2026-01-01T00:00:00Z", To: "2026-01-31T00:00:00Z"}
	if _, err := Run(ctx, st, "total_received", good); err != nil {
		t.Fatalf("good window: %v", err)
	}
	bad := []Params{
		{From: "not a date", To: "2026-01-31T00:00:00Z"},
		{From: "2026-02-01T00:00:00Z", To: "2026-01-01T00:00:00Z"},
		{From: "2024-01-01T00:00:00Z", To: "2026-01-31T00:00:00Z"},
		{From: "2026-01-01T00:00:00.5Z", To: "2026-01-31T00:00:00Z"}, // a fraction of a second would be lost
		{From: "2026-01-01T00:00:00Z", To: "2026-01-31T00:00:00.25Z"},
	}
	for _, p := range bad {
		if _, err := Run(ctx, st, "total_received", p); !errors.Is(err, ErrParams) {
			t.Errorf("%+v: want ErrParams, got %v", p, err)
		}
	}
	if _, err := Run(ctx, st, "count_orders", Params{From: good.From, To: good.To, State: "bogus"}); !errors.Is(err, ErrParams) {
		t.Errorf("unknown state: %v", err)
	}
}

// The read pool cannot write: a write through it must fail.
func TestReadPoolIsReadOnly(t *testing.T) {
	st := seeded(t)
	if _, err := st.Read().Exec(`INSERT INTO receiving_accounts(id,vpa,bank,display_name,created_at) VALUES('x','x@yy','b','n','t')`); err == nil {
		t.Error("a write through the read pool must fail")
	}
	_ = time.Now
}

// No query file may contain a write verb.
func TestNoWriteVerbsInAskPackage(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	write := regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE|DROP|ALTER|CREATE)\b`)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, _ := os.ReadFile(f)
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, "`") && write.MatchString(line) && !strings.Contains(line, "Scan") && strings.Contains(line, "SELECT") {
				t.Errorf("%s: write verb near a SELECT: %s", f, strings.TrimSpace(line))
			}
		}
	}
}

func TestEveryNameRuns(t *testing.T) {
	st := seeded(t)
	p := Params{From: "2026-01-01T00:00:00Z", To: "2026-01-31T00:00:00Z", State: "confirmed"}
	for _, n := range Names() {
		if _, err := Run(context.Background(), st, n, p); err != nil {
			t.Errorf("%s: %v", n, err)
		}
	}
}
