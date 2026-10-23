package web

import (
	"context"
	"database/sql"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/opensawce/kaasu/i18n"
	"github.com/opensawce/kaasu/internal/accounts"
	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/orders"
	"github.com/opensawce/kaasu/internal/store"
	"github.com/opensawce/kaasu/internal/testutil"
)

type env struct {
	st  *store.Store
	ord *orders.Service
	h   http.Handler
	ctx context.Context
}

func setup(t *testing.T) env {
	t.Helper()
	st := testutil.OpenStore(t)
	clk := clock.NewFake(time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC))
	ids := &testutil.Seq{}
	accs := accounts.New(st, clk, ids)
	ord := orders.New(st, clk, ids, accs)
	ctx := context.Background()
	if _, err := accs.Create(ctx, accounts.Input{VPA: "sharma.sweets@okhdfcbank", Bank: "HDFC", DisplayName: "Sharma Sweets"}); err != nil {
		t.Fatal(err)
	}
	p := &Pay{Orders: ord, ShopName: "Sharma Sweets", ShopLang: "en"}
	return env{st: st, ord: ord, h: p.Handler(), ctx: ctx}
}

func (e env) get(t *testing.T, path string, header map[string]string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	b, _ := io.ReadAll(rec.Body)
	return rec, string(b)
}

func (e env) order(t *testing.T, amount int64, key string) string {
	t.Helper()
	o, _, err := e.ord.Create(e.ctx, orders.CreateInput{AmountPaise: amount, ExpiresIn: time.Hour}, key)
	if err != nil {
		t.Fatal(err)
	}
	return o.ID
}

func TestPageShowsExactAmountAndOnePrimaryButton(t *testing.T) {
	e := setup(t)
	id := e.order(t, 49907, "p1")
	rec, body := e.get(t, "/pay/"+id, nil)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(body, "₹499.07") {
		t.Error("exact amount missing")
	}
	if n := strings.Count(body, `class="btn primary"`); n != 1 {
		t.Errorf("primary buttons=%d want 1", n)
	}
	if !strings.Contains(html.UnescapeString(body), "Pay ₹499.07 with any UPI app") {
		t.Error("button must name the amount")
	}
	if !strings.Contains(body, "upi://pay?pa=sharma.sweets%40okhdfcbank") {
		t.Error("UPI link missing or escaped wrongly")
	}
}

// The waiting copy must never say the payment failed while the outcome is unknown.
func TestNeverSaysFailedWhileWaiting(t *testing.T) {
	e := setup(t)
	id := e.order(t, 49907, "p2")
	_, body := e.get(t, "/pay/"+id, nil)
	for _, bad := range []string{"failed", "Failed", "error", "Error", "declined", "Declined"} {
		if strings.Contains(body, bad) {
			t.Errorf("waiting page says %q", bad)
		}
	}
	if !strings.Contains(html.UnescapeString(body), "Already paid? Don't pay again.") {
		t.Error("already-paid reassurance missing")
	}
}

func TestUnknownOrderIs404(t *testing.T) {
	e := setup(t)
	if rec, _ := e.get(t, "/pay/ord_missing", nil); rec.Code != 404 {
		t.Errorf("status %d", rec.Code)
	}
}

func TestQueuedAndExpiredCopy(t *testing.T) {
	e := setup(t)
	id := e.order(t, 49907, "p3")
	if _, err := e.st.Read().Exec(`SELECT 1`); err != nil {
		t.Fatal(err)
	}
	if err := e.st.Tx(e.ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE orders SET state='queued' WHERE id=?`, id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	_, body := e.get(t, "/pay/"+id, nil)
	if !strings.Contains(body, "Waiting for a free slot") {
		t.Error("queued copy missing")
	}
}

func TestStatusJSONCarriesStateOnly(t *testing.T) {
	e := setup(t)
	id := e.order(t, 49907, "p4")
	rec, body := e.get(t, "/pay/"+id+"/status", nil)
	if rec.Code != 200 || body != `{"state":"awaiting"}`+"\n" {
		t.Errorf("code=%d body=%q", rec.Code, body)
	}
}

func TestLanguageFromBrowserAndOverride(t *testing.T) {
	e := setup(t)
	id := e.order(t, 49907, "p5")
	_, body := e.get(t, "/pay/"+id, map[string]string{"Accept-Language": "hi-IN,hi;q=0.9"})
	if !strings.Contains(body, `lang="hi"`) {
		t.Error("browser language not used")
	}
	rec, _ := e.get(t, "/pay/"+id+"?lang=ta", nil)
	found := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == "kaasu_lang" && c.Value == "ta" {
			found = true
		}
	}
	if !found {
		t.Error("?lang= must set the cookie")
	}
}

func TestPageBudgetAndNoThirdParty(t *testing.T) {
	e := setup(t)
	id := e.order(t, 49907, "p6")
	_, body := e.get(t, "/pay/"+id, nil)
	if len(body) > 30*1024 {
		t.Errorf("page is %d bytes, budget is 30 KB", len(body))
	}
	third := regexp.MustCompile(`(src|href)="https?://`)
	if third.MatchString(body) {
		t.Error("third-party request on the pay page")
	}
	if strings.Contains(body, "<script>") {
		t.Error("inline script is not allowed (CSP)")
	}
}

func TestStaticAssetsServed(t *testing.T) {
	e := setup(t)
	for _, p := range []string{"/static/pay.js", "/static/pay.css", "/static/tokens.css"} {
		if rec, _ := e.get(t, p, nil); rec.Code != 200 {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
}

var _ = i18n.Languages
