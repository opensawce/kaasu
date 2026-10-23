package admin

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/opensawce/kaasu/i18n"
	"github.com/opensawce/kaasu/internal/adminauth"
	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/secrets"
	"github.com/opensawce/kaasu/internal/store"
	"github.com/opensawce/kaasu/internal/testutil"
)

var seed = []byte("12345678901234567890")

type env struct {
	st  *store.Store
	h   http.Handler
	clk *clock.Fake
	ctx context.Context
	db  *sql.DB
}

func setup(t *testing.T) env {
	t.Helper()
	st := testutil.OpenStore(t)
	clk := clock.NewFake(time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC))
	vault := secrets.NewVault(st, secrets.MasterKey(make([]byte, 32)), clk)
	auth := adminauth.New(st, clk, vault)
	ctx := context.Background()
	if _, err := auth.CreateAdmin(ctx, "priya", "owner", seed); err != nil {
		t.Fatal(err)
	}
	d := &Dashboard{Auth: auth, DB: st.Read(), Lang: i18n.Lang("en"), Now: clk.Now}
	m := http.NewServeMux()
	d.Register(m)
	return env{st: st, h: m, clk: clk, ctx: ctx, db: st.Read()}
}

func (e env) signIn(t *testing.T) string {
	t.Helper()
	form := url.Values{"name": {"priya"}, "code": {adminauth.CodeAt(seed, e.clk.Now())}}
	req := httptest.NewRequest("POST", "/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == adminauth.CookieName {
			return c.Value
		}
	}
	t.Fatal("no session cookie")
	return ""
}

func (e env) get(t *testing.T, path, token string) (int, string, string) {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	if token != "" {
		req.AddCookie(&http.Cookie{Name: adminauth.CookieName, Value: token})
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	b, _ := io.ReadAll(rec.Body)
	return rec.Code, string(b), rec.Header().Get("Location")
}

func TestDashboardNeedsSignIn(t *testing.T) {
	e := setup(t)
	code, _, loc := e.get(t, "/admin/", "")
	if code != http.StatusSeeOther || loc != "/admin/login" {
		t.Errorf("code=%d loc=%s", code, loc)
	}
	if code, _, _ := e.get(t, "/admin/orders", ""); code != http.StatusSeeOther {
		t.Errorf("orders without session: %d", code)
	}
}

func TestWrongCodeIsRefused(t *testing.T) {
	e := setup(t)
	form := url.Values{"name": {"priya"}, "code": {"000000"}}
	req := httptest.NewRequest("POST", "/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "sign-in failed") {
		t.Errorf("code=%d", rec.Code)
	}
}

// With no active phone the Today page must say so: one way or none to see payments is a warning.
func TestTodayWarnsWithOneOrNoSource(t *testing.T) {
	e := setup(t)
	tok := e.signIn(t)
	code, body, _ := e.get(t, "/admin/", tok)
	if code != 200 || !strings.Contains(body, `role="status"`) || !strings.Contains(body, "No source is sending payments") {
		t.Errorf("code=%d body=%s", code, body)
	}
}

// The payer's full VPA must never appear on an admin page, even when it sits in stored evidence.
func TestPayerNeverShownInFull(t *testing.T) {
	e := setup(t)
	if err := e.st.Tx(e.ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO devices(id,label,kind,public_key,paired_at) VALUES('dev_1','Phone','android',x'00',?)`, "2026-01-01T10:00:00Z")
		if err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO evidence(id,source,device_id,received_at,raw_hash,body) VALUES('evd_1','device','dev_1',?, 'h1', ?)`,
			"2026-01-01T10:00:00Z", []byte("paid by ravi.kumar@okicici"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	tok := e.signIn(t)
	for _, p := range []string{"/admin/", "/admin/orders"} {
		_, body, _ := e.get(t, p, tok)
		if strings.Contains(body, "ravi.kumar@okicici") {
			t.Errorf("%s shows a full VPA", p)
		}
	}
}
