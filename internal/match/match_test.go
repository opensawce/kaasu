package match

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/opensawce/kaasu/internal/accounts"
	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/evidence"
	"github.com/opensawce/kaasu/internal/orders"
	"github.com/opensawce/kaasu/internal/pool"
	"github.com/opensawce/kaasu/internal/states"
	"github.com/opensawce/kaasu/internal/store"
	"github.com/opensawce/kaasu/internal/testutil"
)

var t0 = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

// Score weights are part of the contract; these tests pin them.
func TestScoreWeights(t *testing.T) {
	all := Signals{true, true, true, true, true, true, true}
	if s, _ := Score(all); s != 100 {
		t.Errorf("all signals = %d want 100", s)
	}
	singles := map[string]struct {
		s    Signals
		want int
	}{
		"exact":     {Signals{ExactAmount: true}, 40},
		"authentic": {Signals{AuthenticSource: true}, 20},
		"utr":       {Signals{UTRPresentUnique: true}, 10},
		"note":      {Signals{NoteEcho: true}, 10},
		"timing":    {Signals{TimingOK: true}, 10},
		"payer":     {Signals{PayerKnown: true}, 5},
		"multi":     {Signals{MultiSource: true}, 5},
	}
	for name, c := range singles {
		if got, _ := Score(c.s); got != c.want {
			t.Errorf("%s = %d want %d", name, got, c.want)
		}
	}
}

func TestDecideTable(t *testing.T) {
	good := Signals{ExactAmount: true, AuthenticSource: true, UTRPresentUnique: true, TimingOK: true}
	tests := []struct {
		name    string
		s       Signals
		amount  int64
		limit   int64
		outcome string
		reason  string
	}{
		{"auto", good, 49907, 200000, "auto", ""},
		{"over limit", good, 49907, 100, "review", "over_limit"},
		{"no limit set", good, 49907, 0, "review", "over_limit"},
		{"unauthentic", Signals{ExactAmount: true, UTRPresentUnique: true, TimingOK: true}, 49907, 200000, "review", "unauthentic"},
		{"low score", Signals{ExactAmount: true, AuthenticSource: true}, 49907, 200000, "review", "low_score"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc, _ := Score(tt.s)
			d := Decide(sc, tt.s, tt.amount, tt.limit)
			if d.Outcome != tt.outcome || d.Reason != tt.reason {
				t.Errorf("got %+v", d)
			}
		})
	}
}

// Property: an automatic confirmation never happens without an exact amount from an authentic source.
func TestNoAutoWithoutExactAmount(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		s := Signals{
			ExactAmount: rapid.Bool().Draw(rt, "exact"), AuthenticSource: rapid.Bool().Draw(rt, "auth"),
			UTRPresentUnique: rapid.Bool().Draw(rt, "utr"), NoteEcho: rapid.Bool().Draw(rt, "note"),
			TimingOK: rapid.Bool().Draw(rt, "time"), PayerKnown: rapid.Bool().Draw(rt, "payer"), MultiSource: rapid.Bool().Draw(rt, "multi"),
		}
		sc, _ := Score(s)
		d := Decide(sc, s, rapid.Int64Range(100, 1000000).Draw(rt, "amt"), rapid.Int64Range(0, 1000000).Draw(rt, "limit"))
		if d.Outcome == "auto" && (!s.ExactAmount || !s.AuthenticSource) {
			rt.Fatalf("auto without exact+authentic: %+v", s)
		}
	})
}

type env struct {
	st  *store.Store
	clk *clock.Fake
	ctx context.Context
	ord *orders.Service
	ev  *evidence.Service
	acc *accounts.Service
	m   *Matcher
	dev string
	seq *testutil.Seq
}

func setup(t *testing.T) env {
	t.Helper()
	st := testutil.OpenStore(t)
	clk := clock.NewFake(t0)
	ids := &testutil.Seq{}
	accs := accounts.New(st, clk, ids)
	ord := orders.New(st, clk, ids, accs)
	ev := evidence.New(clk, ids)
	ctx := context.Background()
	if err := st.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO devices(id,label,kind,public_key,paired_at) VALUES('dev_1','Phone','android',x'00',?)`, t0.Format(time.RFC3339))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return env{st: st, clk: clk, ctx: ctx, ord: ord, ev: ev, acc: accs, m: New(st, clk, states.New(clk, pool.New(st, clk))), dev: "dev_1", seq: ids}
}

// makeAlert stores evidence and an alert the way ingest does. source is "device" or "email" or "manual".
func (e env) makeAlert(t *testing.T, source string, p evidence.Parsed, raw string) string {
	t.Helper()
	var alertID string
	if err := e.st.Tx(e.ctx, func(tx *sql.Tx) error {
		dev := ""
		if source == "device" {
			dev = e.dev
		}
		evID, _, err := e.ev.SaveEvidence(e.ctx, tx, source, dev, []byte(raw+e.seq.New("raw")))
		if err != nil {
			return err
		}
		alertID, _, err = e.ev.SaveAlert(e.ctx, tx, evID, p)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return alertID
}

func (e env) state(t *testing.T, id string) string {
	t.Helper()
	var s string
	if err := e.st.Read().QueryRow(`SELECT state FROM orders WHERE id=?`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func (e env) matches(t *testing.T, alertID string) int {
	t.Helper()
	var n int
	if err := e.st.Read().QueryRow(`SELECT count(*) FROM matches WHERE alert_id=?`, alertID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e env) count(t *testing.T, q string) int {
	t.Helper()
	var n int
	if err := e.st.Read().QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func goodParsed(amount int64, utr string) evidence.Parsed {
	return evidence.Parsed{Bank: "HDFC", AmountPaise: amount, UTR: utr, PayerMasked: "sa***@okicici", OccurredAt: t0, Parser: "HDFC"}
}

func TestProcessAutoConfirms(t *testing.T) {
	e := setup(t)
	acc, _ := e.acc.Create(e.ctx, accounts.Input{VPA: "shop@okbank", Bank: "B", DisplayName: "Shop", AutoConfirmLimitPaise: 1000000})
	o, _, _ := e.ord.Create(e.ctx, orders.CreateInput{AmountPaise: 49907, AccountID: acc.ID, Reference: "INV-1", ExpiresIn: time.Hour}, "k1")
	alert := e.makeAlert(t, "device", goodParsed(49907, "629812345678"), "Rs.499.07 credited")
	if err := e.m.Process(e.ctx, alert); err != nil {
		t.Fatal(err)
	}
	if got := e.state(t, o.ID); got != "confirmed" {
		t.Fatalf("state=%s want confirmed", got)
	}
	if n := e.count(t, `SELECT count(*) FROM ledger WHERE kind='order.confirmed'`); n != 1 {
		t.Errorf("confirmed ledger rows=%d", n)
	}
	var reason, breakdown, by string
	_ = e.st.Read().QueryRow(`SELECT reason, breakdown, decided_by FROM matches WHERE alert_id=?`, alert).Scan(&reason, &breakdown, &by)
	if reason != "" || !strings.Contains(breakdown, "exact") || by != "matcher" {
		t.Errorf("match row: reason=%q breakdown=%s by=%s", reason, breakdown, by)
	}
}

func TestOverLimitGoesToReview(t *testing.T) {
	e := setup(t)
	acc, _ := e.acc.Create(e.ctx, accounts.Input{VPA: "shop@okbank", Bank: "B", DisplayName: "Shop", AutoConfirmLimitPaise: 100})
	o, _, _ := e.ord.Create(e.ctx, orders.CreateInput{AmountPaise: 49907, AccountID: acc.ID, ExpiresIn: time.Hour}, "k2")
	alert := e.makeAlert(t, "device", goodParsed(49907, "111111111111"), "x")
	if err := e.m.Process(e.ctx, alert); err != nil {
		t.Fatal(err)
	}
	if got := e.state(t, o.ID); got != "review" {
		t.Errorf("state=%s want review", got)
	}
}

func TestUnauthenticSourceGoesToReview(t *testing.T) {
	e := setup(t)
	acc, _ := e.acc.Create(e.ctx, accounts.Input{VPA: "shop@okbank", Bank: "B", DisplayName: "Shop", AutoConfirmLimitPaise: 1000000})
	o, _, _ := e.ord.Create(e.ctx, orders.CreateInput{AmountPaise: 49907, AccountID: acc.ID, ExpiresIn: time.Hour}, "k3")
	alert := e.makeAlert(t, "manual", goodParsed(49907, "222222222222"), "typed by customer")
	if err := e.m.Process(e.ctx, alert); err != nil {
		t.Fatal(err)
	}
	if got := e.state(t, o.ID); got != "review" {
		t.Errorf("manual evidence must never confirm; state=%s", got)
	}
}

func TestOffByOnePaiseGoesToReview(t *testing.T) {
	e := setup(t)
	acc, _ := e.acc.Create(e.ctx, accounts.Input{VPA: "shop@okbank", Bank: "B", DisplayName: "Shop", AutoConfirmLimitPaise: 1000000})
	o, _, _ := e.ord.Create(e.ctx, orders.CreateInput{AmountPaise: 49907, AccountID: acc.ID, ExpiresIn: time.Hour}, "k4")
	alert := e.makeAlert(t, "device", goodParsed(49908, "333333333333"), "x")
	if err := e.m.Process(e.ctx, alert); err != nil {
		t.Fatal(err)
	}
	if got := e.state(t, o.ID); got != "review" {
		t.Fatalf("state=%s want review: one paisa off must never confirm", got)
	}
	var reason string
	_ = e.st.Read().QueryRow(`SELECT reason FROM matches WHERE alert_id=?`, alert).Scan(&reason)
	if reason != "amount_mismatch" {
		t.Errorf("reason=%q want amount_mismatch", reason)
	}
}

func TestLateAlertGoesToReview(t *testing.T) {
	e := setup(t)
	acc, _ := e.acc.Create(e.ctx, accounts.Input{VPA: "shop@okbank", Bank: "B", DisplayName: "Shop", AutoConfirmLimitPaise: 1000000})
	o, _, _ := e.ord.Create(e.ctx, orders.CreateInput{AmountPaise: 49907, AccountID: acc.ID, ExpiresIn: 5 * time.Minute}, "k5")
	e.clk.Advance(30 * time.Minute)
	alert := e.makeAlert(t, "device", goodParsed(49907, "444444444444"), "x")
	if err := e.m.Process(e.ctx, alert); err != nil {
		t.Fatal(err)
	}
	if got := e.state(t, o.ID); got != "review" {
		t.Errorf("late alert: state=%s want review", got)
	}
}

func TestClosedOrderAmountGoesToReview(t *testing.T) {
	e := setup(t)
	acc, _ := e.acc.Create(e.ctx, accounts.Input{VPA: "shop@okbank", Bank: "B", DisplayName: "Shop", AutoConfirmLimitPaise: 1000000})
	o, _, _ := e.ord.Create(e.ctx, orders.CreateInput{AmountPaise: 49907, AccountID: acc.ID, ExpiresIn: time.Hour}, "k6")
	if _, err := e.ord.Cancel(e.ctx, o.ID); err != nil {
		t.Fatal(err)
	}
	alert := e.makeAlert(t, "device", goodParsed(49907, "555555555555"), "x")
	if err := e.m.Process(e.ctx, alert); err != nil {
		t.Fatal(err)
	}
	if got := e.state(t, o.ID); got != "cancelled" {
		t.Errorf("cancelled order must stay cancelled, got %s", got)
	}
	var reason string
	_ = e.st.Read().QueryRow(`SELECT reason FROM matches WHERE alert_id=?`, alert).Scan(&reason)
	if reason != "order_closed" {
		t.Errorf("reason=%q want order_closed", reason)
	}
}

func TestAmbiguousGoesToReview(t *testing.T) {
	e := setup(t)
	acc, _ := e.acc.Create(e.ctx, accounts.Input{VPA: "shop@okbank", Bank: "B", DisplayName: "Shop", AutoConfirmLimitPaise: 1000000})
	a, _, _ := e.ord.Create(e.ctx, orders.CreateInput{AmountPaise: 49907, AccountID: acc.ID, ExpiresIn: time.Hour}, "k7")
	b, _, _ := e.ord.Create(e.ctx, orders.CreateInput{AmountPaise: 49907, AccountID: acc.ID, ExpiresIn: time.Hour}, "k8")
	_ = b
	// Two open orders with the same amount on one account only happen if the pool is bypassed; force it.
	if err := e.st.Tx(e.ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(e.ctx, `UPDATE orders SET amount_paise=49907 WHERE id=?`, b.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	alert := e.makeAlert(t, "device", goodParsed(49907, "666666666666"), "x")
	if err := e.m.Process(e.ctx, alert); err != nil {
		t.Fatal(err)
	}
	if e.state(t, a.ID) == "confirmed" || e.state(t, b.ID) == "confirmed" {
		t.Fatal("ambiguous alert must not confirm")
	}
}

// Review focus 2: concurrent duplicates confirm once.
func TestProcessIsIdempotent(t *testing.T) {
	e := setup(t)
	acc, _ := e.acc.Create(e.ctx, accounts.Input{VPA: "shop@okbank", Bank: "B", DisplayName: "Shop", AutoConfirmLimitPaise: 1000000})
	o, _, _ := e.ord.Create(e.ctx, orders.CreateInput{AmountPaise: 49907, AccountID: acc.ID, ExpiresIn: time.Hour}, "k9")
	alert := e.makeAlert(t, "device", goodParsed(49907, "777777777777"), "x")
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = e.m.Process(e.ctx, alert) }()
	}
	wg.Wait()
	if e.matches(t, alert) != 1 {
		t.Errorf("matches=%d want 1", e.matches(t, alert))
	}
	if n := e.count(t, fmt.Sprintf(`SELECT count(*) FROM ledger WHERE kind='order.confirmed' AND order_id='%s'`, o.ID)); n != 1 {
		t.Errorf("confirmed rows=%d want 1", n)
	}
}

func TestUnmatchedAlertStaysUnmatched(t *testing.T) {
	e := setup(t)
	alert := e.makeAlert(t, "device", goodParsed(123400, "888888888888"), "x")
	if err := e.m.Process(e.ctx, alert); err != nil {
		t.Fatal(err)
	}
	if e.matches(t, alert) != 0 {
		t.Error("no order means no match row")
	}
}

// Only internal/match and internal/states may move an order to confirmed. Review (card 25) must use its own path.
func TestOnlyMatchConfirms(t *testing.T) {
	root := filepath.Join("..", "..")
	// internal/review confirms only on an owner decision, behind owner role and fresh sign-in.
	allowed := map[string]bool{"internal/match": true, "internal/states": true, "internal/review": true}
	call := regexp.MustCompile(`Transition\(.*"confirmed"`)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel, _ := filepath.Rel(root, filepath.Dir(path))
		if allowed[filepath.ToSlash(rel)] {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if call.Match(b) {
			t.Errorf("%s confirms orders; only internal/states, internal/match and the owner-decision path in internal/review may", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAccountHintSelectsAccount(t *testing.T) {
	e := setup(t)
	a1, _ := e.acc.Create(e.ctx, accounts.Input{VPA: "one@okbank", Bank: "B", DisplayName: "One", AutoConfirmLimitPaise: 1000000})
	a2, _ := e.acc.Create(e.ctx, accounts.Input{VPA: "two@okbank", Bank: "B", DisplayName: "Two", AutoConfirmLimitPaise: 1000000})
	if err := e.st.Tx(e.ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(e.ctx, `INSERT INTO settings(key,value) VALUES('account.'||?||'.last4','1234'),('account.'||?||'.last4','9999')`, a1.ID, a2.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	o1, _, _ := e.ord.Create(e.ctx, orders.CreateInput{AmountPaise: 49907, AccountID: a1.ID, ExpiresIn: time.Hour}, "h1")
	o2, _, _ := e.ord.Create(e.ctx, orders.CreateInput{AmountPaise: 49907, AccountID: a2.ID, ExpiresIn: time.Hour}, "h2")
	_ = o2
	p := goodParsed(49907, "121212121212")
	p.AccountHint = "9999"
	alert := e.makeAlert(t, "device", p, "hint 9999")
	if err := e.m.Process(e.ctx, alert); err != nil {
		t.Fatal(err)
	}
	if e.state(t, o1.ID) != "awaiting" {
		t.Errorf("order on the other account must not be touched, state=%s", e.state(t, o1.ID))
	}
	if e.state(t, o2.ID) != "confirmed" {
		t.Errorf("hinted account's order should confirm, state=%s", e.state(t, o2.ID))
	}
}

func TestAmbiguousWithAwaitingAndExpiredCandidates(t *testing.T) {
	e := setup(t)
	acc, _ := e.acc.Create(e.ctx, accounts.Input{VPA: "shop@okbank", Bank: "B", DisplayName: "Shop", AutoConfirmLimitPaise: 1000000})
	a, _, _ := e.ord.Create(e.ctx, orders.CreateInput{AmountPaise: 49907, AccountID: acc.ID, ExpiresIn: time.Hour}, "am1")
	b, _, _ := e.ord.Create(e.ctx, orders.CreateInput{AmountPaise: 49907, AccountID: acc.ID, ExpiresIn: time.Hour}, "am2")
	if err := e.st.Tx(e.ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(e.ctx, `UPDATE orders SET amount_paise=49907 WHERE id=?`, b.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	alert := e.makeAlert(t, "device", goodParsed(49907, "131313131313"), "amb2")
	if err := e.m.Process(e.ctx, alert); err != nil {
		t.Fatal(err)
	}
	if e.state(t, a.ID) != "review" || e.state(t, b.ID) != "review" {
		t.Errorf("both candidates should be held for review: %s %s", e.state(t, a.ID), e.state(t, b.ID))
	}
	var reason string
	_ = e.st.Read().QueryRow(`SELECT reason FROM matches WHERE alert_id=?`, alert).Scan(&reason)
	if reason != "ambiguous" {
		t.Errorf("reason=%q", reason)
	}
}

func TestMismatchWithNoOrderStaysUnmatchedAndMultipleIsNotAttached(t *testing.T) {
	e := setup(t)
	acc, _ := e.acc.Create(e.ctx, accounts.Input{VPA: "shop@okbank", Bank: "B", DisplayName: "Shop", AutoConfirmLimitPaise: 1000000})
	e.ord.Create(e.ctx, orders.CreateInput{AmountPaise: 49900, AccountID: acc.ID, ExpiresIn: time.Hour}, "mm1")
	e.ord.Create(e.ctx, orders.CreateInput{AmountPaise: 49901, AccountID: acc.ID, ExpiresIn: time.Hour}, "mm2")
	// Alert 49905 sits within 99 paise above both bases: two candidates for a mismatch, so none is chosen.
	alert := e.makeAlert(t, "device", goodParsed(49905, "141414141414"), "mm")
	if err := e.m.Process(e.ctx, alert); err != nil {
		t.Fatal(err)
	}
	if e.matches(t, alert) != 0 {
		t.Error("two possible mismatch orders must not attach the alert")
	}
}

func TestAlertsWithUnknownParseFieldsDoNotPanic(t *testing.T) {
	e := setup(t)
	alert := e.makeAlert(t, "device", goodParsed(100, ""), "nope")
	if err := e.m.Process(e.ctx, alert); err != nil {
		t.Fatal(err)
	}
	if err := e.m.Process(e.ctx, "alt_missing"); err == nil {
		t.Error("unknown alert id should be an error")
	}
}

func TestOldClosedOrderWithSameAmountDoesNotBlockNewOrder(t *testing.T) {
	e := setup(t)
	acc, _ := e.acc.Create(e.ctx, accounts.Input{VPA: "shop@okbank", Bank: "B", DisplayName: "Shop", AutoConfirmLimitPaise: 1000000})
	o, _, _ := e.ord.Create(e.ctx, orders.CreateInput{AmountPaise: 49907, AccountID: acc.ID, ExpiresIn: time.Hour}, "k9")
	old := t0.Add(-3 * time.Hour).Format(time.RFC3339)
	if err := e.st.Tx(e.ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(e.ctx, `INSERT INTO orders(id,account_id,base_amount_paise,amount_paise,state,expires_at,created_at,updated_at)
			VALUES('ord_old',?,49907,49907,'expired',?,?,?)`, acc.ID, old, old, old)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	alert := e.makeAlert(t, "device", goodParsed(49907, "777777777777"), "x")
	if err := e.m.Process(e.ctx, alert); err != nil {
		t.Fatal(err)
	}
	if got := e.state(t, o.ID); got != "confirmed" {
		t.Fatalf("new awaiting order must confirm despite an old expired one, got %s", got)
	}
}
