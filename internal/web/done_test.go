package web

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/opensawce/kaasu/i18n"
	"github.com/opensawce/kaasu/internal/accounts"
	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/orders"
	"github.com/opensawce/kaasu/internal/pool"
	"github.com/opensawce/kaasu/internal/states"
	"github.com/opensawce/kaasu/internal/testutil"
)

type doneEnv struct {
	ctx context.Context
	ord *orders.Service
	sm  *states.Machine
	h   http.Handler
	c   *Confirmed
}

func setupDone(t *testing.T) doneEnv {
	t.Helper()
	st := testutil.OpenStore(t)
	clk := clock.NewFake(time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC))
	ids := &testutil.Seq{}
	accs := accounts.New(st, clk, ids)
	ord := orders.New(st, clk, ids, accs)
	sm := states.New(clk, pool.New(st, clk))
	ctx := context.Background()
	if _, err := accs.Create(ctx, accounts.Input{VPA: "shop@okbank", Bank: "B", DisplayName: "Sharma Sweets"}); err != nil {
		t.Fatal(err)
	}
	c, err := NewConfirmed(ord, st, "Sharma Sweets", "en", clk.Now)
	if err != nil {
		t.Fatal(err)
	}
	return doneEnv{ctx: ctx, ord: ord, sm: sm, h: c.Handler(), c: c}
}

func (e doneEnv) order(t *testing.T, amount int64, key string) string {
	t.Helper()
	o, _, err := e.ord.Create(e.ctx, orders.CreateInput{AmountPaise: amount, Reference: "INV-7", ExpiresIn: time.Hour}, key)
	if err != nil {
		t.Fatal(err)
	}
	return o.ID
}

func (e doneEnv) confirm(t *testing.T, id string) {
	t.Helper()
	// Confirm through the state machine, the same way the matcher does.
	st := e.c.Store
	if err := st.Tx(e.ctx, func(tx *sql.Tx) error {
		if _, err := e.sm.Transition(e.ctx, tx, id, "matched", nil); err != nil {
			return err
		}
		_, err := e.sm.Transition(e.ctx, tx, id, "confirmed", nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func (e doneEnv) get(t *testing.T, path string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec, rec.Body.String()
}

func TestDoneRedirectsUnlessConfirmed(t *testing.T) {
	e := setupDone(t)
	id := e.order(t, 49907, "d1")
	for _, path := range []string{"/pay/" + id + "/done", "/pay/" + id + "/done?amount=1"} {
		rec, _ := e.get(t, path)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/pay/"+id {
			t.Errorf("%s: code=%d location=%s", path, rec.Code, rec.Header().Get("Location"))
		}
	}
}

func TestDoneRendersForConfirmed(t *testing.T) {
	e := setupDone(t)
	id := e.order(t, 49907, "d2")
	e.confirm(t, id)
	rec, body := e.get(t, "/pay/"+id+"/done")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(body, "₹499.07") || !strings.Contains(body, `data-since=`) || !strings.Contains(body, `class="tick"`) {
		t.Errorf("missing amount, live clock or tick")
	}
	if strings.Contains(strings.ToLower(body), "paisu") {
		t.Error("Paisu must not appear on customer pages")
	}
}

// The amount comes from the server, never from the query string.
func TestAmountFromServerNotQuery(t *testing.T) {
	e := setupDone(t)
	id := e.order(t, 49907, "d3")
	e.confirm(t, id)
	_, body := e.get(t, "/pay/"+id+"/done?amount=1")
	if !strings.Contains(body, "₹499.07") || strings.Contains(body, "₹1.00") {
		t.Error("amount must come from the order")
	}
}

func TestCheckCodeStableAndPerOrder(t *testing.T) {
	e := setupDone(t)
	a := e.c.Code("ord_1", "2026-01-01T10:00:00Z", 49907)
	if a != e.c.Code("ord_1", "2026-01-01T10:00:00Z", 49907) {
		t.Error("same inputs must give the same code")
	}
	if a == e.c.Code("ord_2", "2026-01-01T10:00:00Z", 49907) {
		t.Error("different orders should differ")
	}
	if len(a) != 6 {
		t.Errorf("code length %d", len(a))
	}
}

func TestReceiptContentAndLanguages(t *testing.T) {
	e := setupDone(t)
	id := e.order(t, 9999950, "d4")
	e.confirm(t, id)
	_, body := e.get(t, "/pay/"+id+"/receipt")
	if !strings.Contains(body, "₹99,999.50") || !strings.Contains(body, "Check code") || !strings.Contains(body, "INV-7") {
		t.Errorf("receipt missing amount, code or reference")
	}
	rec, txt := e.get(t, "/pay/"+id+"/receipt?format=txt")
	if rec.Code != 200 || !strings.Contains(txt, "Amount: ₹99,999.50") {
		t.Errorf("text receipt: %d %s", rec.Code, txt)
	}
	for _, l := range i18n.Languages {
		if i18n.Rupees(9999950, l) != "₹99,999.50" {
			t.Errorf("%s money format", l)
		}
	}
}

func TestClockAndReducedMotionCSS(t *testing.T) {
	css, err := os.ReadFile("static/done.css")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(css), "prefers-reduced-motion") {
		t.Error("tick animation must respect reduced motion")
	}
	js, _ := os.ReadFile("static/done.js")
	if !strings.Contains(string(js), "setInterval") {
		t.Error("clock must tick")
	}
}

func TestCheckCodeSurvivesARestart(t *testing.T) {
	e := setupDone(t)
	again, err := NewConfirmed(e.c.Orders, e.c.Store, "Sharma Sweets", "en", e.c.now)
	if err != nil {
		t.Fatal(err)
	}
	if e.c.Code("ord_1", "2026-01-01T10:00:00Z", 100) != again.Code("ord_1", "2026-01-01T10:00:00Z", 100) {
		t.Error("a second start on the same database must produce the same check codes")
	}
}
