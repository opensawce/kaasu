package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/opensawce/kaasu/internal/accounts"
	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/orders"
	"github.com/opensawce/kaasu/internal/secrets"
	"github.com/opensawce/kaasu/internal/testutil"
)

type fixture struct {
	srv   *httptest.Server
	key   string
	vault *secrets.Vault
}

func setup(t *testing.T) fixture {
	t.Helper()
	st := testutil.OpenStore(t)
	clk := clock.NewFake(time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC))
	ids := &testutil.Seq{}
	accs := accounts.New(st, clk, ids)
	if _, err := accs.Create(context.Background(), accounts.Input{VPA: "shop@okbank", Bank: "B", DisplayName: "Shop"}); err != nil {
		t.Fatal(err)
	}
	vault := secrets.NewVault(st, secrets.MasterKey(make([]byte, 32)), clk)
	key, _, err := vault.NewAPIKey(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	h := New(Deps{Orders: orders.New(st, clk, ids, accs), Vault: vault})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return fixture{srv: srv, key: key, vault: vault}
}

func (f fixture) do(t *testing.T, method, path, key, idem, body string) (*http.Response, map[string]any) {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), method, f.srv.URL+path, strings.NewReader(body))
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	return resp, m
}

func TestAuthRequired(t *testing.T) {
	f := setup(t)
	for _, key := range []string{"", "nope", "kaasu_live_wrong"} {
		resp, body := f.do(t, "GET", "/v1/orders/ord_1", key, "", "")
		if resp.StatusCode != 401 {
			t.Errorf("key %q: status %d", key, resp.StatusCode)
		}
		if e, _ := body["error"].(map[string]any); e["code"] != "unauthorized" {
			t.Errorf("error body: %v", body)
		}
	}
	// A key in the query string must never work.
	resp, _ := f.do(t, "GET", "/v1/orders/ord_1?api_key="+f.key, "", "", "")
	if resp.StatusCode != 401 {
		t.Errorf("query-string key accepted: %d", resp.StatusCode)
	}
}

func TestRevokedKeyIsRejected(t *testing.T) {
	f := setup(t)
	_, rec, _ := f.vault.NewAPIKey(context.Background(), "x")
	plain, rec2, _ := f.vault.NewAPIKey(context.Background(), "y")
	_ = rec
	_ = f.vault.RevokeAPIKey(context.Background(), rec2.ID)
	if resp, _ := f.do(t, "GET", "/v1/orders/ord_1", plain, "", ""); resp.StatusCode != 401 {
		t.Errorf("revoked key status %d", resp.StatusCode)
	}
}

func TestCreateGetCancel(t *testing.T) {
	f := setup(t)
	resp, body := f.do(t, "POST", "/v1/orders", f.key, "k1", `{"amount_paise":49900,"reference":"INV-1","note":"Order 1"}`)
	if resp.StatusCode != 201 {
		t.Fatalf("status %d body %v", resp.StatusCode, body)
	}
	id, _ := body["id"].(string)
	if body["state"] != "awaiting" || body["amount_paise"] != float64(49900) || body["currency"] != "INR" || !strings.HasPrefix(body["upi_link"].(string), "upi://pay?pa=shop%40okbank") {
		t.Errorf("body=%v", body)
	}
	resp, body = f.do(t, "POST", "/v1/orders", f.key, "k1", `{"amount_paise":49900,"reference":"INV-1","note":"Order 1"}`)
	if resp.StatusCode != 200 || body["id"] != id || resp.Header.Get("Idempotent-Replayed") != "true" {
		t.Errorf("replay: %d %v", resp.StatusCode, body)
	}
	if resp, body = f.do(t, "POST", "/v1/orders", f.key, "k1", `{"amount_paise":50000}`); resp.StatusCode != 422 {
		t.Errorf("conflict: %d %v", resp.StatusCode, body)
	}
	if resp, _ = f.do(t, "GET", "/v1/orders/"+id, f.key, "", ""); resp.StatusCode != 200 {
		t.Errorf("get: %d", resp.StatusCode)
	}
	if resp, _ = f.do(t, "GET", "/v1/orders/ord_missing", f.key, "", ""); resp.StatusCode != 404 {
		t.Errorf("missing: %d", resp.StatusCode)
	}
	if resp, body = f.do(t, "POST", "/v1/orders/"+id+"/cancel", f.key, "", ""); resp.StatusCode != 200 || body["state"] != "cancelled" {
		t.Errorf("cancel: %d %v", resp.StatusCode, body)
	}
	if resp, _ = f.do(t, "POST", "/v1/orders/"+id+"/cancel", f.key, "", ""); resp.StatusCode != 409 {
		t.Errorf("second cancel: %d", resp.StatusCode)
	}
}

func TestCreateRejectsBadInput(t *testing.T) {
	f := setup(t)
	tests := []struct{ name, idem, body string }{
		{"missing idempotency key", "", `{"amount_paise":100}`},
		{"float amount", "a", `{"amount_paise":499.5}`},
		{"string amount", "b", `{"amount_paise":"499"}`},
		{"rupees instead of paise", "c", `{"amount":499}`},
		{"unknown field", "d", `{"amount_paise":100,"extra":1}`},
		{"not json", "e", `amount=100`},
		{"empty body", "f", ``},
		{"trailing data", "g", `{"amount_paise":100}{"amount_paise":200}`},
		{"too small", "h", `{"amount_paise":50}`},
		{"huge body", "i", `{"amount_paise":100,"note":"` + strings.Repeat("x", 70_000) + `"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, body := f.do(t, "POST", "/v1/orders", f.key, tt.idem, tt.body)
			if resp.StatusCode != 400 && resp.StatusCode != 413 {
				t.Errorf("status %d body %v", resp.StatusCode, body)
			}
		})
	}
}

// Every route the handler serves must be documented in openapi.yaml.
func TestOpenAPIDocumentsEveryRoute(t *testing.T) {
	spec, err := os.ReadFile("../../openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range Routes() {
		method, path, _ := strings.Cut(r, " ")
		if !strings.Contains(string(spec), "  "+path+":") || !strings.Contains(string(spec), strings.ToLower(method)+":") {
			t.Errorf("openapi.yaml does not document %s", r)
		}
	}
}
