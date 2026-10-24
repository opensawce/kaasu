package webhooks

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/secrets"
	"github.com/opensawce/kaasu/internal/store"
	"github.com/opensawce/kaasu/internal/testutil"
)

var t0 = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

func TestSignVerifyRoundTrip(t *testing.T) {
	body := []byte(`{"event_id":"evt_1"}`)
	h := Sign("s3cret", t0, body)
	if err := Verify("s3cret", h, body, t0, 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := Verify("other", h, body, t0, 5*time.Minute); err == nil {
		t.Error("wrong secret must fail")
	}
	if err := Verify("s3cret", h, []byte(`{"event_id":"evt_2"}`), t0, 5*time.Minute); err == nil {
		t.Error("tampered body must fail")
	}
	if err := Verify("s3cret", h, body, t0.Add(6*time.Minute), 5*time.Minute); err == nil {
		t.Error("outside tolerance must fail")
	}
	if err := Verify("s3cret", h, body, t0.Add(4*time.Minute), 5*time.Minute); err != nil {
		t.Errorf("inside tolerance should pass: %v", err)
	}
	if err := Verify("s3cret", "garbage", body, t0, 5*time.Minute); err == nil {
		t.Error("bad format must fail")
	}
	tampered := strings.Replace(h, "t=", "t=9", 1)
	if err := Verify("s3cret", tampered, body, t0, 5*time.Minute); err == nil {
		t.Error("tampered timestamp must fail")
	}
}

type fixture struct {
	st    *store.Store
	clk   *clock.Fake
	d     *Deliverer
	vault *secrets.Vault
	srv   *httptest.Server
	mu    sync.Mutex
	hits  []http.Header
	codes []int
}

func setup(t *testing.T, codes ...int) *fixture {
	t.Helper()
	f := &fixture{st: testutil.OpenStore(t), clk: clock.NewFake(t0), codes: codes}
	f.vault = secrets.NewVault(f.st, secrets.MasterKey(make([]byte, 32)), f.clk)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		_, _ = io.ReadAll(r.Body)
		f.hits = append(f.hits, r.Header.Clone())
		code := http.StatusOK
		if len(f.codes) > 0 {
			code = f.codes[0]
			if len(f.codes) > 1 {
				f.codes = f.codes[1:]
			}
		}
		w.WriteHeader(code)
	}))
	t.Cleanup(f.srv.Close)
	f.setSetting(t, "webhook.url", f.srv.URL)
	f.setSetting(t, "webhook.allow_private", "1")
	f.d = New(f.st, f.clk, f.vault, true, nil)
	return f
}

func (f *fixture) setSetting(t *testing.T, k, v string) {
	t.Helper()
	if err := f.st.Tx(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) queue(t *testing.T, event string) string {
	t.Helper()
	id := "obx_" + event
	if err := f.st.Tx(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO outbox(id,event_id,kind,payload,status,attempts,next_attempt_at,created_at)
			VALUES(?,?,?,?, 'pending', 0, ?, ?)`, id, "evt_"+event, event,
			`{"event_id":"evt_`+event+`","type":"`+event+`","data":{"order":{"id":"ord_1","state":"confirmed","amount_paise":49907}}}`,
			t0.Format(time.RFC3339), t0.Format(time.RFC3339))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *fixture) status(t *testing.T, id string) (string, int) {
	t.Helper()
	var s string
	var n int
	if err := f.st.Read().QueryRow(`SELECT status, attempts FROM outbox WHERE id=?`, id).Scan(&s, &n); err != nil {
		t.Fatal(err)
	}
	return s, n
}

func TestAtLeastOnceDelivery(t *testing.T) {
	f := setup(t, 500, 500, 200)
	id := f.queue(t, "order.confirmed")
	gaps := []time.Duration{10 * time.Second, 30 * time.Second}
	for i := 0; i < 3; i++ {
		if _, err := f.d.Deliver(context.Background()); err != nil {
			t.Fatal(err)
		}
		if i < len(gaps) {
			f.clk.Advance(gaps[i])
		}
	}
	if s, n := f.status(t, id); s != "delivered" || n != 3 {
		t.Fatalf("status=%s attempts=%d", s, n)
	}
	if len(f.hits) != 3 {
		t.Fatalf("hits=%d", len(f.hits))
	}
	for _, h := range f.hits {
		if h.Get("X-Kaasu-Event-Id") != "evt_order.confirmed" {
			t.Errorf("event id changed between attempts: %s", h.Get("X-Kaasu-Event-Id"))
		}
	}
	var logged int
	_ = f.st.Read().QueryRow(`SELECT count(*) FROM webhook_deliveries`).Scan(&logged)
	if logged != 3 {
		t.Errorf("deliveries logged=%d", logged)
	}
}

func TestDeadLetterAfterEight(t *testing.T) {
	f := setup(t, 500)
	id := f.queue(t, "order.review")
	for i := 0; i < 8; i++ {
		if _, err := f.d.Deliver(context.Background()); err != nil {
			t.Fatal(err)
		}
		f.clk.Advance(25 * time.Hour)
	}
	if s, n := f.status(t, id); s != "dead" || n != 8 {
		t.Fatalf("status=%s attempts=%d", s, n)
	}
	var n int
	_ = f.st.Read().QueryRow(`SELECT count(*) FROM ledger WHERE kind='webhook.dead'`).Scan(&n)
	if n != 1 {
		t.Errorf("dead-letter ledger rows=%d", n)
	}
}

func TestResendRequeuesDeadRow(t *testing.T) {
	f := setup(t, 500)
	id := f.queue(t, "order.refunded")
	for i := 0; i < 8; i++ {
		_, _ = f.d.Deliver(context.Background())
		f.clk.Advance(25 * time.Hour)
	}
	f.codes = []int{200}
	if err := f.d.Resend(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.Deliver(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s, _ := f.status(t, id); s != "delivered" {
		t.Errorf("status=%s after resend", s)
	}
	if err := f.d.Resend(context.Background(), "obx_nope"); err == nil {
		t.Error("unknown row must fail")
	}
}

func TestSignatureHeaderVerifiesAtReceiver(t *testing.T) {
	f := setup(t, 200)
	f.setSetting(t, "webhook.url", f.srv.URL)
	f.queue(t, "order.disputed")
	if _, err := f.d.Deliver(context.Background()); err != nil {
		t.Fatal(err)
	}
	secret, err := f.vault.Get(context.Background(), "webhook_secret")
	if err != nil {
		t.Fatal(err)
	}
	h := f.hits[0]
	if err := Verify(string(secret), h.Get("X-Kaasu-Signature"), []byte(`{"event_id":"evt_order.disputed","type":"order.disputed","data":{"order":{"id":"ord_1","state":"confirmed","amount_paise":49907}}}`), f.clk.Now(), 5*time.Minute); err != nil {
		t.Errorf("receiver could not verify: %v", err)
	}
}

func TestSSRFGuardBlocksPrivateTargets(t *testing.T) {
	f := setup(t, 200)
	f.setSetting(t, "webhook.allow_private", "0")
	d := New(f.st, f.clk, f.vault, false, nil)
	for _, u := range []string{f.srv.URL, "https://10.0.0.5/hook", "http://169.254.169.254/latest", "https://localhost/x"} {
		if err := d.checkURL(u); err == nil {
			t.Errorf("%s should be blocked", u)
		}
	}
	if err := d.checkURL("https://example.com/hook"); err != nil {
		t.Errorf("public https should pass the URL check: %v", err)
	}
	f.setSetting(t, "webhook.url", f.srv.URL)
	id := f.queue(t, "order.expired")
	if _, err := d.Deliver(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s, _ := f.status(t, id); s == "delivered" {
		t.Error("private target must not receive the webhook")
	}
}

func TestPayloadHasNoPayerData(t *testing.T) {
	f := setup(t, 200)
	id := f.queue(t, "order.confirmed")
	var payload string
	_ = f.st.Read().QueryRow(`SELECT payload FROM outbox WHERE id=?`, id).Scan(&payload)
	for _, bad := range []string{"@", "payer", "utr", "text"} {
		if strings.Contains(strings.ToLower(payload), bad) {
			t.Errorf("payload leaks %q", bad)
		}
	}
}
