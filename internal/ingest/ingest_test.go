package ingest

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/devices"
	"github.com/opensawce/kaasu/internal/evidence"
	"github.com/opensawce/kaasu/internal/store"
	"github.com/opensawce/kaasu/internal/testutil"
)

var t0 = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

type fixture struct {
	st    *store.Store
	srv   *httptest.Server
	clk   *clock.Fake
	dev   *devices.Service
	key   ed25519.PrivateKey
	devID string
}

func setup(t *testing.T) fixture {
	t.Helper()
	st := testutil.OpenStore(t)
	clk := clock.NewFake(t0)
	dev := devices.New(st, clk, "shop.example")
	ev := evidence.New(clk, &testutil.Seq{})
	h := Handler(Deps{Store: st, Clock: clk, Devices: dev, Evidence: ev, Matcher: nil})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	key := testutil.Key(7)
	tok, _, err := dev.NewPairing(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	d, err := dev.Pair(context.Background(), tok, key.Public().(ed25519.PublicKey), "Shop phone", "android")
	if err != nil {
		t.Fatal(err)
	}
	return fixture{st: st, srv: srv, clk: clk, dev: dev, key: key, devID: d.ID}
}

const sampleAlert = `{"source":"sms","sender":"AD-HDFCBK","text":"Rs.499.07 credited to a/c XX1234 on 23-10-26 by VPA sample.payer@okicici (UPI Ref No 629812345678).","received_at":"2026-01-01T10:00:00Z"}`

func (f fixture) post(t *testing.T, path string, h http.Header, body []byte) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", f.srv.URL+path, bytes.NewReader(body))
	for k, v := range h {
		req.Header[k] = v
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func (f fixture) signed(body string, ts time.Time, nonce string) http.Header {
	return Sign(f.key, f.devID, ts, nonce, []byte(body))
}

const nonce1 = "nonce_aaaaaaaaaaaaaaaa"

func TestValidSignatureAccepted(t *testing.T) {
	f := setup(t)
	resp := f.post(t, "/ingest/v1/alerts", f.signed(sampleAlert, t0, nonce1), []byte(sampleAlert))
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestRejectsBadRequests(t *testing.T) {
	f := setup(t)
	other := testutil.Key(9)
	tests := []struct {
		name string
		h    func() http.Header
		body string
	}{
		{"wrong key", func() http.Header { return Sign(other, f.devID, t0, nonce1, []byte(sampleAlert)) }, sampleAlert},
		{"tampered body", func() http.Header { return f.signed(sampleAlert, t0, nonce1) }, strings.Replace(sampleAlert, "499.07", "999.07", 1)},
		{"tampered timestamp", func() http.Header {
			h := f.signed(sampleAlert, t0, nonce1)
			h.Set("X-Kaasu-Timestamp", "1767261601")
			return h
		}, sampleAlert},
		{"unknown device", func() http.Header {
			h := f.signed(sampleAlert, t0, nonce1)
			h.Set("X-Kaasu-Device", "dev_nope")
			return h
		}, sampleAlert},
		{"missing signature", func() http.Header { h := f.signed(sampleAlert, t0, nonce1); h.Del("X-Kaasu-Signature"); return h }, sampleAlert},
		{"bad base64", func() http.Header {
			h := f.signed(sampleAlert, t0, nonce1)
			h.Set("X-Kaasu-Signature", "!!!")
			return h
		}, sampleAlert},
		{"short nonce", func() http.Header { return f.signed(sampleAlert, t0, "short") }, sampleAlert},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := f.post(t, "/ingest/v1/alerts", tt.h(), []byte(tt.body))
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("status %d want 401", resp.StatusCode)
			}
		})
	}
}

// Review focus 3: the window is 60 seconds either way, inclusive.
func TestTimestampWindow(t *testing.T) {
	cases := []struct {
		offset time.Duration
		want   int
	}{
		{-60 * time.Second, http.StatusAccepted},
		{-61 * time.Second, http.StatusUnauthorized},
		{60 * time.Second, http.StatusAccepted},
		{61 * time.Second, http.StatusUnauthorized},
	}
	for i, c := range cases {
		f := setup(t)
		nonce := "nonce_window_" + string(rune('a'+i)) + "aaaaaaaaaaaa"
		resp := f.post(t, "/ingest/v1/alerts", f.signed(sampleAlert, t0.Add(c.offset), nonce), []byte(sampleAlert))
		if resp.StatusCode != c.want {
			t.Errorf("offset %s: status %d want %d", c.offset, resp.StatusCode, c.want)
		}
	}
}

func TestReplayRejected(t *testing.T) {
	f := setup(t)
	h := f.signed(sampleAlert, t0, nonce1)
	if r := f.post(t, "/ingest/v1/alerts", h, []byte(sampleAlert)); r.StatusCode != http.StatusAccepted {
		t.Fatalf("first: %d", r.StatusCode)
	}
	if r := f.post(t, "/ingest/v1/alerts", h, []byte(sampleAlert)); r.StatusCode != http.StatusConflict {
		t.Errorf("replay: %d want 409", r.StatusCode)
	}
}

func TestRevokeIsImmediate(t *testing.T) {
	f := setup(t)
	if r := f.post(t, "/ingest/v1/alerts", f.signed(sampleAlert, t0, nonce1), []byte(sampleAlert)); r.StatusCode != http.StatusAccepted {
		t.Fatal(r.StatusCode)
	}
	if err := f.dev.Revoke(context.Background(), f.devID); err != nil {
		t.Fatal(err)
	}
	if r := f.post(t, "/ingest/v1/alerts", f.signed(sampleAlert, t0, "nonce_after_revoke_aaaa"), []byte(sampleAlert)); r.StatusCode != http.StatusUnauthorized {
		t.Errorf("revoked device: %d", r.StatusCode)
	}
}

func TestPairTokenSingleUseAndExpiry(t *testing.T) {
	f := setup(t)
	tok, payload, err := f.dev.NewPairing(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(payload, "kaasu://pair?host=shop.example&token=") {
		t.Errorf("payload %s", payload)
	}
	pub := testutil.Key(3).Public().(ed25519.PublicKey)
	if _, err := f.dev.Pair(context.Background(), tok, pub, "a", "android"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.dev.Pair(context.Background(), tok, pub, "a", "android"); err == nil {
		t.Error("token must work once")
	}
	tok2, _, _ := f.dev.NewPairing(context.Background())
	f.clk.Advance(10*time.Minute + time.Second)
	if _, err := f.dev.Pair(context.Background(), tok2, pub, "a", "android"); err == nil {
		t.Error("expired token must fail")
	}
	if _, err := f.dev.Pair(context.Background(), "wrong", pub, "a", "android"); err == nil {
		t.Error("wrong token must fail")
	}
}

func TestPairEndpoint(t *testing.T) {
	f := setup(t)
	tok, _, _ := f.dev.NewPairing(context.Background())
	body, _ := json.Marshal(map[string]string{
		"token": tok, "public_key": base64.StdEncoding.EncodeToString(testutil.Key(4).Public().(ed25519.PublicKey)),
		"label": "Second phone", "kind": "android",
	})
	resp := f.post(t, "/ingest/v1/pair", nil, body)
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("pair: %d", resp.StatusCode)
	}
}

func TestRawBodyStoredAsReceived(t *testing.T) {
	f := setup(t)
	body := sampleAlert + "\n\t "
	f.post(t, "/ingest/v1/alerts", f.signed(body, t0, nonce1), []byte(body))
	st := f.st
	var got []byte
	if err := st.Read().QueryRow(`SELECT body FROM evidence LIMIT 1`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Errorf("stored body differs")
	}
}

func TestUnknownSenderIsIgnoredAndNoAlert(t *testing.T) {
	f := setup(t)
	body := `{"source":"sms","sender":"EVIL-BANK","text":"Rs.1000 credited UTR 111111111111","received_at":"2026-01-01T10:00:00Z"}`
	resp := f.post(t, "/ingest/v1/alerts", f.signed(body, t0, nonce1), []byte(body))
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var n int
	_ = f.st.Read().QueryRow(`SELECT count(*) FROM alerts`).Scan(&n)
	if n != 0 {
		t.Error("unknown sender must not create an alert")
	}
}

func TestBodyLimit(t *testing.T) {
	f := setup(t)
	big := bytes.Repeat([]byte("a"), 65<<10)
	resp := f.post(t, "/ingest/v1/alerts", f.signed(string(big), t0, nonce1), big)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status %d want 413", resp.StatusCode)
	}
}

func TestSignaturesAreStable(t *testing.T) {
	f := setup(t)
	a := f.signed(sampleAlert, t0, nonce1).Get("X-Kaasu-Signature")
	b := f.signed(sampleAlert, t0, nonce1).Get("X-Kaasu-Signature")
	if a == "" || a != b {
		t.Errorf("signature must be deterministic for identical input: %q %q", a, b)
	}
}
