package review

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

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

type env struct {
	ctx context.Context
	st  *store.Store
	clk *clock.Fake
	ord *orders.Service
	sm  *states.Machine
	ev  *evidence.Service
	svc *Service
	acc string
}

func setup(t *testing.T) env {
	t.Helper()
	st := testutil.OpenStore(t)
	clk := clock.NewFake(t0)
	ids := &testutil.Seq{}
	accs := accounts.New(st, clk, ids)
	ord := orders.New(st, clk, ids, accs)
	sm := states.New(clk, pool.New(st, clk))
	acc, _ := accs.Create(context.Background(), accounts.Input{VPA: "shop@okbank", Bank: "B", DisplayName: "Shop"})
	return env{ctx: context.Background(), st: st, clk: clk, ord: ord, sm: sm, ev: evidence.New(clk, ids), svc: New(st, clk, sm), acc: acc.ID}
}

// held makes an order sit in review with a review match, as the matcher would leave it.
func (e env) held(t *testing.T, key string, expires time.Duration) string {
	t.Helper()
	o, _, err := e.ord.Create(e.ctx, orders.CreateInput{AmountPaise: 49907, AccountID: e.acc, ExpiresIn: expires}, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.Tx(e.ctx, func(tx *sql.Tx) error {
		if _, err := e.sm.Transition(e.ctx, tx, o.ID, "review", map[string]any{"reason": "amount_mismatch"}); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO devices(id,label,kind,public_key,paired_at) VALUES('dev_r','P','android',x'00',?) ON CONFLICT(id) DO NOTHING`, t0.Format(time.RFC3339))
		if err != nil {
			return err
		}
		evID, _, err := e.ev.SaveEvidence(e.ctx, tx, "device", "dev_r", []byte("Rs.499.07 credited "+key))
		if err != nil {
			return err
		}
		alertID, _, err := e.ev.SaveAlert(e.ctx, tx, evID, evidence.Parsed{AmountPaise: 49908, UTR: "", PayerMasked: "ra***@okbank", OccurredAt: t0})
		if err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO matches(id, alert_id, order_id, score, decision, decided_by, created_at, breakdown, reason)
			VALUES(?,?,?,?,?,?,?,?,?)`, "mat_"+key, alertID, o.ID, 0, "review", "matcher", t0.Format(time.RFC3339), `{}`, "amount_mismatch")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return o.ID
}

func (e env) state(t *testing.T, id string) string {
	t.Helper()
	var s string
	_ = e.st.Read().QueryRow(`SELECT state FROM orders WHERE id=?`, id).Scan(&s)
	return s
}

func TestQueueShowsHeldPayments(t *testing.T) {
	e := setup(t)
	id := e.held(t, "q1", time.Hour)
	items, err := e.svc.Queue(e.ctx)
	if err != nil || len(items) != 1 || items[0].OrderID != id {
		t.Fatalf("queue=%+v err=%v", items, err)
	}
	if items[0].ExpectedPaise != 49907 || items[0].SaidPaise != 49908 || items[0].Reason != "amount_mismatch" {
		t.Errorf("expected vs said: %+v", items[0])
	}
	if n, _ := e.svc.Badge(e.ctx); n != 1 {
		t.Errorf("badge=%d", n)
	}
}

func TestConfirmInsideWindowIsConfirmed(t *testing.T) {
	e := setup(t)
	id := e.held(t, "c1", time.Hour)
	if err := e.svc.Confirm(e.ctx, id, "admin:priya"); err != nil {
		t.Fatal(err)
	}
	if s := e.state(t, id); s != "confirmed" {
		t.Errorf("state=%s", s)
	}
	var by string
	_ = e.st.Read().QueryRow(`SELECT decided_by FROM matches WHERE order_id=?`, id).Scan(&by)
	if by != "admin:priya" {
		t.Errorf("decided_by=%s", by)
	}
	var actor string
	_ = e.st.Read().QueryRow(`SELECT actor FROM ledger WHERE kind='order.confirmed' AND order_id=?`, id).Scan(&actor)
	if actor != "admin:priya" {
		t.Errorf("ledger actor=%s", actor)
	}
}

func TestConfirmAfterExpiryIsLatePaid(t *testing.T) {
	e := setup(t)
	id := e.held(t, "l1", time.Minute)
	e.clk.Advance(5 * time.Minute)
	if err := e.svc.Confirm(e.ctx, id, "admin:priya"); err != nil {
		t.Fatal(err)
	}
	if s := e.state(t, id); s != "late_paid" {
		t.Errorf("state=%s want late_paid", s)
	}
}

func TestConfirmTwiceIsRefused(t *testing.T) {
	e := setup(t)
	id := e.held(t, "t1", time.Hour)
	if err := e.svc.Confirm(e.ctx, id, "admin:priya"); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Confirm(e.ctx, id, "admin:priya"); !errors.Is(err, ErrNotInReview) {
		t.Errorf("second confirm: %v", err)
	}
}

func TestRejectNeedsReason(t *testing.T) {
	e := setup(t)
	id := e.held(t, "r1", time.Hour)
	if err := e.svc.Reject(e.ctx, id, "", "admin:priya"); !errors.Is(err, ErrNoReason) {
		t.Errorf("empty reason: %v", err)
	}
	if err := e.svc.Reject(e.ctx, id, "because", "admin:priya"); !errors.Is(err, ErrNoReason) {
		t.Errorf("unknown reason: %v", err)
	}
	if err := e.svc.Reject(e.ctx, id, "wrong_amount", "admin:priya"); err != nil {
		t.Fatal(err)
	}
	if s := e.state(t, id); s != "rejected" {
		t.Errorf("state=%s", s)
	}
}

func TestDetailHasRawAlert(t *testing.T) {
	e := setup(t)
	id := e.held(t, "d1", time.Hour)
	it, err := e.svc.Detail(e.ctx, id)
	if err != nil || it.RawAlert == "" {
		t.Fatalf("detail: %+v %v", it, err)
	}
	if _, err := e.svc.Detail(e.ctx, "ord_none"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
}

func TestDecisionWithoutAHeldMatchChangesNothing(t *testing.T) {
	e := setup(t)
	o, _, err := e.ord.Create(e.ctx, orders.CreateInput{AmountPaise: 49907, AccountID: e.acc, ExpiresIn: time.Hour}, "nomatch")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.Tx(e.ctx, func(tx *sql.Tx) error {
		_, err := e.sm.Transition(e.ctx, tx, o.ID, "review", nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Confirm(e.ctx, o.ID, "admin:priya"); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("confirm without a held match: %v", err)
	}
	if err := e.svc.Reject(e.ctx, o.ID, "other", "admin:priya"); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("reject without a held match: %v", err)
	}
	if got := e.state(t, o.ID); got != "review" {
		t.Errorf("state=%s, the failed decisions must roll back", got)
	}
}
