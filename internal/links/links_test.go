package links

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/opensawce/kaasu/internal/accounts"
	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/orders"
	"github.com/opensawce/kaasu/internal/testutil"
)

type env struct {
	ctx context.Context
	clk *clock.Fake
	svc *Service
	acc string
	ord *orders.Service
}

func setup(t *testing.T) env {
	t.Helper()
	st := testutil.OpenStore(t)
	clk := clock.NewFake(time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC))
	ids := &testutil.Seq{}
	accs := accounts.New(st, clk, ids)
	ord := orders.New(st, clk, ids, accs)
	acc, err := accs.Create(context.Background(), accounts.Input{VPA: "sharma.sweets@okhdfcbank", Bank: "HDFC", DisplayName: "Sharma Sweets"})
	if err != nil {
		t.Fatal(err)
	}
	return env{ctx: context.Background(), clk: clk, svc: New(st, clk, ids, ord), acc: acc.ID, ord: ord}
}

func TestLinkCreatesOrderPerVisit(t *testing.T) {
	e := setup(t)
	l, err := e.svc.Create(e.ctx, Input{AccountID: e.acc, Kind: "link", AmountPaise: 49907, Note: "Order 1042"})
	if err != nil {
		t.Fatal(err)
	}
	o1, err := e.svc.Open(e.ctx, l.ID, "browser-a")
	if err != nil {
		t.Fatal(err)
	}
	o2, err := e.svc.Open(e.ctx, l.ID, "browser-b")
	if err != nil {
		t.Fatal(err)
	}
	if o1.ID == o2.ID {
		t.Error("a different visitor gets a different order")
	}
}

// A refresh by the same visitor reuses the order rather than making a new one.
func TestRefreshReusesOrder(t *testing.T) {
	e := setup(t)
	l, _ := e.svc.Create(e.ctx, Input{AccountID: e.acc, Kind: "link", AmountPaise: 49907})
	a, err := e.svc.Open(e.ctx, l.ID, "same")
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.svc.Open(e.ctx, l.ID, "same")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID {
		t.Errorf("refresh made a new order: %s %s", a.ID, b.ID)
	}
}

// After the first order is no longer open, the same visitor starts a fresh, payable attempt.
func TestVisitAfterExpiryStartsANewAttempt(t *testing.T) {
	e := setup(t)
	l, _ := e.svc.Create(e.ctx, Input{AccountID: e.acc, Kind: "link", AmountPaise: 49907})
	a, err := e.svc.Open(e.ctx, l.ID, "same")
	if err != nil {
		t.Fatal(err)
	}
	e.clk.Advance(2 * time.Hour)
	b, err := e.svc.Open(e.ctx, l.ID, "same")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Fatal("a visit after the order expired must get a new order")
	}
}

func TestExpiredAndDisabledLinksAreGone(t *testing.T) {
	e := setup(t)
	l, _ := e.svc.Create(e.ctx, Input{AccountID: e.acc, Kind: "link", AmountPaise: 100, ExpiresIn: time.Hour})
	e.clk.Advance(2 * time.Hour)
	if _, err := e.svc.Open(e.ctx, l.ID, "v"); err != ErrGone {
		t.Errorf("expired link: %v", err)
	}
	l2, _ := e.svc.Create(e.ctx, Input{AccountID: e.acc, Kind: "link", AmountPaise: 100})
	if err := e.svc.Disable(e.ctx, l2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Open(e.ctx, l2.ID, "v"); err != ErrGone {
		t.Errorf("disabled link: %v", err)
	}
}

func TestCounterAmountParsing(t *testing.T) {
	tests := map[string]int64{"499": 49900, "499.07": 49907, "1,200": 120000, "1,00,000": 10000000}
	for in, want := range tests {
		got, err := ParseCounterAmount(in)
		if err != nil || got != want {
			t.Errorf("%q: got %d err=%v want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"0.5", "abc", "1e3", "100000.01", "", "-5", "1.234"} {
		if _, err := ParseCounterAmount(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestWhatsAppLinkEncoding(t *testing.T) {
	got := WhatsAppURL("https://shop.example/p/lnk_1", "Pay ₹499.07 here & now")
	if got != "https://wa.me/?text=Pay%20%E2%82%B9499.07%20here%20%26%20now%0Ahttps%3A%2F%2Fshop.example%2Fp%2Flnk_1" {
		t.Errorf("got %s", got)
	}
}

func TestStandeeIsA6AndDeterministic(t *testing.T) {
	a := Standee("Sharma Sweets", "https://shop.example/c/lnk_2")
	if !containsAll(a, "@page", "105mm", "148mm", "Scan, enter the amount, pay with any UPI app") {
		t.Error("standee missing A6 page size or copy")
	}
	if a != Standee("Sharma Sweets", "https://shop.example/c/lnk_2") {
		t.Error("standee must be deterministic")
	}
	if containsAll(a, "http://", "https://fonts") {
		t.Error("standee must not load external assets")
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

func TestCounterOrderMatchesAlertPath(t *testing.T) {
	e := setup(t)
	c, err := e.svc.Create(e.ctx, Input{AccountID: e.acc, Kind: "counter"})
	if err != nil {
		t.Fatal(err)
	}
	o, err := e.svc.OpenCounter(e.ctx, c.ID, 49907, "counter-visitor")
	if err != nil {
		t.Fatal(err)
	}
	if o.AmountPaise != 49907 || o.State != "awaiting" {
		t.Errorf("counter order: %+v", o)
	}
}
