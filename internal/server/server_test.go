package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/opensawce/kaasu/internal/config"
)

func start(t *testing.T) *Server {
	t.Helper()
	cfg := config.Config{PublicAddr: "127.0.0.1:0", IngestAddr: "127.0.0.1:0", AdminAddr: "127.0.0.1:0"}
	s := New(cfg, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
	})
	return s
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestHealthzOnEveryListener(t *testing.T) {
	s := start(t)
	a := s.Addrs()
	for name, addr := range map[string]string{"public": a.Public, "ingest": a.Ingest, "admin": a.Admin} {
		code, body := get(t, "http://"+addr+"/healthz")
		if code != 200 || body != `{"status":"ok"}`+"\n" {
			t.Errorf("%s: code=%d body=%q", name, code, body)
		}
	}
}

// Threat T6: admin routes must not exist on the public or ingest listeners.
func TestAdminRoutesOnlyOnAdminListener(t *testing.T) {
	s := start(t)
	a := s.Addrs()
	if code, _ := get(t, "http://"+a.Admin+"/admin/ping"); code != 200 {
		t.Errorf("admin listener should serve /admin/ping, got %d", code)
	}
	for name, addr := range map[string]string{"public": a.Public, "ingest": a.Ingest} {
		if code, _ := get(t, "http://"+addr+"/admin/ping"); code != 404 {
			t.Errorf("%s listener must not serve admin routes, got %d", name, code)
		}
	}
}

func TestShutdownStopsListeners(t *testing.T) {
	s := start(t)
	addr := s.Addrs().Public
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := http.Get("http://" + addr + "/healthz"); err == nil {
		t.Error("public listener still accepting after shutdown")
	}
}

func TestStartFailsWhenPortTaken(t *testing.T) {
	s := start(t)
	cfg := config.Config{PublicAddr: s.Addrs().Public, IngestAddr: "127.0.0.1:0", AdminAddr: "127.0.0.1:0"}
	s2 := New(cfg, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err := s2.Start(); err == nil {
		_ = s2.Shutdown(context.Background())
		t.Fatal("expected bind error")
	}
}
