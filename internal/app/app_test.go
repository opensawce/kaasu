package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/opensawce/kaasu/internal/accounts"
	"github.com/opensawce/kaasu/internal/config"
	"github.com/opensawce/kaasu/internal/ledger"
)

func TestEndToEndOrderOverHTTP(t *testing.T) {
	cfg := config.Config{PublicAddr: "127.0.0.1:0", IngestAddr: "127.0.0.1:0", AdminAddr: "127.0.0.1:0", DatabasePath: filepath.Join(t.TempDir(), "k.db"), LogLevel: "info"}
	a, err := New(cfg, make([]byte, 32), slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := a.Accounts.Create(ctx, accounts.Input{VPA: "shop@okbank", Bank: "B", DisplayName: "Shop"}); err != nil {
		t.Fatal(err)
	}
	key, _, err := a.Vault.NewAPIKey(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = a.Shutdown(c)
	})
	addrs := a.Server.Addrs()

	req, _ := http.NewRequestWithContext(ctx, "POST", "http://"+addrs.Public+"/v1/orders", strings.NewReader(`{"amount_paise":49900}`))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Idempotency-Key", "k1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	// The API is on the public listener only.
	for name, addr := range map[string]string{"ingest": addrs.Ingest, "admin": addrs.Admin} {
		pr, _ := http.NewRequestWithContext(ctx, "POST", "http://"+addr+"/v1/orders", strings.NewReader(`{}`))
		pr.Header.Set("Content-Type", "application/json")
		r, err := http.DefaultClient.Do(pr)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != 404 {
			t.Errorf("%s serves the public API: %d", name, r.StatusCode)
		}
	}
	if res, err := ledger.Verify(ctx, a.Store.Read()); err != nil || !res.OK || res.Rows < 3 {
		t.Errorf("ledger after order: %+v %v", res, err)
	}
}

func TestIngestOnlyOnIngestListener(t *testing.T) {
	cfg := config.Config{PublicAddr: "127.0.0.1:0", IngestAddr: "127.0.0.1:0", AdminAddr: "127.0.0.1:0", DatabasePath: filepath.Join(t.TempDir(), "k.db"), LogLevel: "info"}
	a, err := New(cfg, make([]byte, 32), slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = a.Shutdown(c)
	})
	addrs := a.Server.Addrs()
	for name, addr := range map[string]string{"public": addrs.Public, "admin": addrs.Admin} {
		r, err := http.Post("http://"+addr+"/ingest/v1/alerts", "application/json", strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != 404 {
			t.Errorf("%s serves ingest: %d", name, r.StatusCode)
		}
	}
	r, err := http.Post("http://"+addrs.Ingest+"/ingest/v1/alerts", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 401 {
		t.Errorf("ingest listener should reply 401 to an unsigned request, got %d", r.StatusCode)
	}
}
