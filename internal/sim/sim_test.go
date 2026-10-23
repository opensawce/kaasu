package sim

import (
	"context"
	"crypto/ed25519"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/devices"
	"github.com/opensawce/kaasu/internal/evidence"
	"github.com/opensawce/kaasu/internal/ingest"
	"github.com/opensawce/kaasu/internal/testutil"
)

func TestScenarioListMatchesRegistry(t *testing.T) {
	want := []string{"exact-match", "edited-amount", "late", "duplicate-sources", "replay", "forged-sender", "unknown-format", "never-arrives", "pool-full", "refund"}
	got := Names()
	if len(got) != len(want) {
		t.Fatalf("names=%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("scenario %d: %s want %s", i, got[i], want[i])
		}
	}
	if _, ok := Lookup("exact-match"); !ok {
		t.Error("exact-match must exist")
	}
	if _, ok := Lookup("nope"); ok {
		t.Error("unknown scenario must not exist")
	}
}

func TestSendIsAcceptedByRealIngest(t *testing.T) {
	st := testutil.OpenStore(t)
	clk := clock.NewFake(time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC))
	dev := devices.New(st, clk, "h")
	key := testutil.Key(5)
	tok, _, _ := dev.NewPairing(context.Background())
	d, err := dev.Pair(context.Background(), tok, key.Public().(ed25519.PublicKey), "sim", "android")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(ingest.Handler(ingest.Deps{Store: st, Clock: clk, Devices: dev, Evidence: evidence.New(clk, &testutil.Seq{})}))
	defer srv.Close()
	body := []byte(`{"source":"sms","sender":"AD-HDFCBK","text":"Rs.499.07 credited to a/c XX1234 on 23-10-26 by VPA s@okicici (UPI Ref No 629812345678).","received_at":"2026-01-01T10:00:00Z"}`)
	code, _, err := Send(context.Background(), srv.URL, d.ID, key, clk.Now(), "nonce_sim_aaaaaaaaaaaa", body)
	if err != nil || code != http.StatusAccepted {
		t.Fatalf("code=%d err=%v", code, err)
	}
	code, _, _ = Send(context.Background(), srv.URL, d.ID, key, clk.Now(), "nonce_sim_aaaaaaaaaaaa", body)
	if code != http.StatusConflict {
		t.Errorf("replay should be refused, got %d", code)
	}
}

func TestGuardBlocksOutboundExceptLocal(t *testing.T) {
	if err := CheckOutbound("https://example.com/x"); err == nil {
		t.Error("external host must be blocked in dev")
	}
	if err := CheckOutbound("http://127.0.0.1:9100/hook"); err != nil {
		t.Errorf("localhost must be allowed: %v", err)
	}
	if err := CheckOutbound("http://localhost:9100/hook"); err != nil {
		t.Errorf("localhost name must be allowed: %v", err)
	}
}
