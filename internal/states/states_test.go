package states

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/opensawce/kaasu/internal/accounts"
	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/pool"
	"github.com/opensawce/kaasu/internal/store"
	"github.com/opensawce/kaasu/internal/testutil"
)

var t0 = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

var all = []string{"created", "queued", "awaiting", "matched", "confirmed", "review", "rejected",
	"cancelled", "expired", "late_paid", "refunded", "disputed"}

// spec is the lifecycle from the issue, copied here independently of the code under test.
var spec = map[string][]string{
	"created":   {"awaiting", "queued", "cancelled"},
	"queued":    {"awaiting", "cancelled", "expired"},
	"awaiting":  {"matched", "review", "cancelled", "expired"},
	"matched":   {"confirmed", "review"},
	"review":    {"confirmed", "late_paid", "rejected", "awaiting"},
	"expired":   {"review"},
	"confirmed": {"refunded", "disputed"},
	"late_paid": {"refunded", "disputed"},
	"disputed":  {"confirmed", "refunded"},
	"rejected":  nil,
	"cancelled": nil,
	"refunded":  nil,
}

type env struct {
	st  *store.Store
	clk *clock.Fake
	m   *Machine
	p   *pool.Pool
	acc accounts.Account
	ctx context.Context
}

func setup(t testing.TB) env {
	t.Helper()
	st := testutil.OpenStore(t)
	clk := clock.NewFake(t0)
	ctx := context.Background()
	acc, err := accounts.New(st, clk, &testutil.Seq{}).Create(ctx, accounts.Input{VPA: "shop@okhdfcbank", Bank: "HDFC", DisplayName: "Shop"})
	if err != nil {
		t.Fatal(err)
	}
	p := pool.New(st, clk)
	return env{st: st, clk: clk, m: New(clk, p), p: p, acc: acc, ctx: ctx}
}

// mk inserts an order directly in the given state.
func mk(e env, id, state string, expires time.Time) error {
	return e.st.Tx(e.ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(e.ctx, `INSERT INTO orders(id,account_id,base_amount_paise,amount_paise,state,expires_at,created_at,updated_at)
			VALUES(?,?,49900,49900,?,?,?,?)`, id, e.acc.ID, state, expires.Format(time.RFC3339), t0.Format(time.RFC3339), t0.Format(time.RFC3339))
		return err
	})
}

// tr runs Transition in its own transaction and returns the state it found.
func tr(e env, id, to string, why map[string]any) (string, error) {
	var from string
	err := e.st.Tx(e.ctx, func(tx *sql.Tx) error {
		var err error
		from, err = e.m.Transition(e.ctx, tx, id, to, why)
		return err
	})
	return from, err
}

func stateOf(t testing.TB, e env, id string) string {
	t.Helper()
	var s string
	if err := e.st.Read().QueryRow(`SELECT state FROM orders WHERE id = ?`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAllowedMatchesSpec(t *testing.T) {
	for _, from := range all {
		for _, to := range all {
			want := slices.Contains(spec[from], to)
			if got := Allowed(from, to); got != want {
				t.Errorf("Allowed(%s, %s) = %v, spec says %v", from, to, got, want)
			}
		}
	}
}

func TestRejectsEveryIllegalTransition(t *testing.T) {
	e := setup(t)
	n := 0
	rapid.Check(t, func(rt *rapid.T) {
		from := rapid.SampledFrom(all).Draw(rt, "from")
		to := rapid.SampledFrom(all).Draw(rt, "to")
		n++
		id := fmt.Sprintf("s_%05d", n)
		if err := mk(e, id, from, t0.Add(time.Hour)); err != nil {
			rt.Fatal(err)
		}
		_, err := tr(e, id, to, nil)
		if Allowed(from, to) {
			if err != nil {
				rt.Fatalf("%s -> %s is allowed but failed: %v", from, to, err)
			}
			if got := stateOf(t, e, id); got != to {
				rt.Fatalf("%s -> %s: state is %s", from, to, got)
			}
			return
		}
		if !errors.Is(err, ErrIllegal) {
			rt.Fatalf("%s -> %s must be ErrIllegal, got %v", from, to, err)
		}
		if got := stateOf(t, e, id); got != from {
			rt.Fatalf("illegal %s -> %s changed the state to %s", from, to, got)
		}
	})
}

func TestTerminalStatesHaveNoExits(t *testing.T) {
	e := setup(t)
	for _, term := range []string{"rejected", "cancelled", "refunded"} {
		id := "t_" + term
		if err := mk(e, id, term, t0.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		for _, to := range all {
			if _, err := tr(e, id, to, nil); !errors.Is(err, ErrIllegal) {
				t.Errorf("%s -> %s must be illegal, got %v", term, to, err)
			}
		}
	}
}

func TestExpiredGoesOnlyToReview(t *testing.T) {
	e := setup(t)
	if err := mk(e, "x1", "expired", t0.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	for _, to := range []string{"awaiting", "confirmed", "matched", "late_paid"} {
		if _, err := tr(e, "x1", to, nil); !errors.Is(err, ErrIllegal) {
			t.Errorf("expired -> %s must be illegal, got %v", to, err)
		}
	}
	if _, err := tr(e, "x1", "review", nil); err != nil {
		t.Fatalf("late alert must reach review: %v", err)
	}
}

func TestReviewBackToAwaitingOnlyBeforeExpiry(t *testing.T) {
	e := setup(t)
	exp := t0.Add(15 * time.Minute)
	if err := mk(e, "r1", "review", exp); err != nil {
		t.Fatal(err)
	}
	e.clk.Set(exp) // at the expiry instant it is already too late
	if _, err := tr(e, "r1", "awaiting", nil); !errors.Is(err, ErrIllegal) {
		t.Errorf("review -> awaiting at expiry must be illegal, got %v", err)
	}
	if err := mk(e, "r2", "review", exp); err != nil {
		t.Fatal(err)
	}
	e.clk.Set(exp.Add(-time.Second))
	if _, err := tr(e, "r2", "awaiting", nil); err != nil {
		t.Errorf("review -> awaiting before expiry must work: %v", err)
	}
}

func TestConfirmedSetsConfirmedAt(t *testing.T) {
	e := setup(t)
	if err := mk(e, "c1", "awaiting", t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := tr(e, "c1", "matched", nil); err != nil {
		t.Fatal(err)
	}
	e.clk.Advance(5 * time.Minute)
	if _, err := tr(e, "c1", "confirmed", nil); err != nil {
		t.Fatal(err)
	}
	var at string
	if err := e.st.Read().QueryRow(`SELECT confirmed_at FROM orders WHERE id = 'c1'`).Scan(&at); err != nil {
		t.Fatal(err)
	}
	if at != "2026-01-01T10:05:00Z" {
		t.Errorf("confirmed_at = %s", at)
	}
}

func TestTransitionWritesLedgerRowInSameTx(t *testing.T) {
	e := setup(t)
	if err := mk(e, "l1", "awaiting", t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	// Roll back after a successful Transition: neither the state nor the ledger row may survive.
	err := e.st.Tx(e.ctx, func(tx *sql.Tx) error {
		if _, err := e.m.Transition(e.ctx, tx, "l1", "matched", nil); err != nil {
			return err
		}
		return errors.New("caller failed later")
	})
	if err == nil {
		t.Fatal("expected the rollback")
	}
	var n int
	if err := e.st.Read().QueryRow(`SELECT count(*) FROM ledger WHERE order_id = 'l1'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 || stateOf(t, e, "l1") != "awaiting" {
		t.Errorf("rollback left state=%s ledger rows=%d", stateOf(t, e, "l1"), n)
	}
}

func TestLedgerKindAndPayload(t *testing.T) {
	e := setup(t)
	if err := mk(e, "p1", "awaiting", t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := tr(e, "p1", "matched", map[string]any{"alert_id": "al_1", "from": "forged"}); err != nil {
		t.Fatal(err)
	}
	var kind, payload string
	if err := e.st.Read().QueryRow(`SELECT kind, payload FROM ledger WHERE order_id = 'p1'`).Scan(&kind, &payload); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatal(err)
	}
	if kind != "order.matched" || got["from"] != "awaiting" || got["to"] != "matched" || got["alert_id"] != "al_1" {
		t.Errorf("kind=%s payload=%s", kind, payload)
	}
}

func TestCreationKeepsItsLedgerName(t *testing.T) {
	e := setup(t)
	if err := mk(e, "k1", "created", t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := tr(e, "k1", "awaiting", nil); err != nil {
		t.Fatal(err)
	}
	var kind string
	if err := e.st.Read().QueryRow(`SELECT kind FROM ledger WHERE order_id = 'k1'`).Scan(&kind); err != nil {
		t.Fatal(err)
	}
	if kind != "order.created" {
		t.Errorf("kind=%s", kind)
	}
}

func TestUnknownOrder(t *testing.T) {
	e := setup(t)
	if _, err := tr(e, "ord_nope", "awaiting", nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v", err)
	}
}

// Expiry does not free the amount at once: a late alert inside the grace window must not match a new order.
func TestExpiryKeepsSlotThroughGrace(t *testing.T) {
	e := setup(t)
	exp := t0.Add(15 * time.Minute)
	if err := mk(e, "e1", "awaiting", exp); err != nil {
		t.Fatal(err)
	}
	reserve := func(id string) (int64, error) {
		var amt int64
		err := e.st.Tx(e.ctx, func(tx *sql.Tx) error {
			var err error
			amt, err = e.p.Reserve(e.ctx, tx, e.acc.ID, id, 49900, exp)
			return err
		})
		return amt, err
	}
	if amt, err := reserve("e1"); err != nil || amt != 49900 {
		t.Fatalf("setup: %d %v", amt, err)
	}
	e.clk.Set(exp)
	if _, err := tr(e, "e1", "expired", nil); err != nil {
		t.Fatal(err)
	}
	if err := mk(e, "e2", "awaiting", exp); err != nil {
		t.Fatal(err)
	}
	if amt, _ := reserve("e2"); amt != 49901 {
		t.Fatalf("inside grace the expired slot must stay held, got %d", amt)
	}
	e.clk.Set(exp.Add(10 * time.Minute))
	if err := mk(e, "e3", "awaiting", exp); err != nil {
		t.Fatal(err)
	}
	if amt, _ := reserve("e3"); amt != 49900 {
		t.Fatalf("after grace the slot is free again, got %d", amt)
	}
}

// A slot freed by a terminal state promotes the oldest queued order on that account.
func TestTerminalFreesSlotAndPromotes(t *testing.T) {
	e := setup(t)
	exp := t0.Add(time.Hour)
	for i := range 100 {
		id := fmt.Sprintf("f_%03d", i)
		if err := mk(e, id, "awaiting", exp); err != nil {
			t.Fatal(err)
		}
		if err := e.st.Tx(e.ctx, func(tx *sql.Tx) error {
			_, err := e.p.Reserve(e.ctx, tx, e.acc.ID, id, 49900, exp)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.st.Tx(e.ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(e.ctx, `INSERT INTO orders(id,account_id,base_amount_paise,amount_paise,state,expires_at,created_at,updated_at,queued_at)
			VALUES('q_1',?,49900,49900,'queued',?,?,?,?)`, e.acc.ID, exp.Format(time.RFC3339), t0.Format(time.RFC3339), t0.Format(time.RFC3339), t0.Format(time.RFC3339))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// awaiting -> rejected is not an edge; an alert puts the order in review first.
	if _, err := tr(e, "f_000", "review", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := tr(e, "f_000", "rejected", nil); err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, e, "q_1"); got != "queued" {
		t.Fatalf("rejected order's amount stays held inside grace, queued order state=%s", got)
	}
	// Only a refund frees the amount at once.
	if _, err := tr(e, "f_000", "refunded", nil); err == nil {
		t.Fatal("rejected is terminal; refunded must be illegal from it")
	}
}

func TestWriteFailuresChangeNothing(t *testing.T) {
	for name, ddl := range map[string]string{
		"order update": `CREATE TRIGGER no_update BEFORE UPDATE ON orders BEGIN SELECT RAISE(ABORT, 'boom'); END`,
		"ledger write": `CREATE TRIGGER no_ledger BEFORE INSERT ON ledger BEGIN SELECT RAISE(ABORT, 'boom'); END`,
	} {
		t.Run(name, func(t *testing.T) {
			e := setup(t)
			if err := mk(e, "w1", "awaiting", t0.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			if err := e.st.Tx(e.ctx, func(tx *sql.Tx) error {
				_, err := tx.ExecContext(e.ctx, ddl)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := tr(e, "w1", "matched", nil); err == nil {
				t.Fatal("expected an error")
			}
			if got := stateOf(t, e, "w1"); got != "awaiting" {
				t.Errorf("state changed to %s despite the failure", got)
			}
		})
	}
}

func TestTransitionOnFinishedTxFails(t *testing.T) {
	e := setup(t)
	if err := mk(e, "d1", "awaiting", t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	tx, err := e.st.Read().Begin()
	if err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()
	if _, err := e.m.Transition(e.ctx, tx, "d1", "matched", nil); err == nil {
		t.Error("a finished tx must not transition")
	}
}
