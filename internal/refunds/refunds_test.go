package refunds

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opensawce/kaasu/internal/accounts"
	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/orders"
	"github.com/opensawce/kaasu/internal/pool"
	"github.com/opensawce/kaasu/internal/states"
	"github.com/opensawce/kaasu/internal/store"
	"github.com/opensawce/kaasu/internal/testutil"
)

var t0 = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

type env struct {
	st  *store.Store
	ctx context.Context
	ord *orders.Service
	sm  *states.Machine
	svc *Service
}

func setup(t *testing.T) env {
	t.Helper()
	st := testutil.OpenStore(t)
	clk := clock.NewFake(t0)
	ids := &testutil.Seq{}
	accs := accounts.New(st, clk, ids)
	ord := orders.New(st, clk, ids, accs)
	sm := states.New(clk, pool.New(st, clk))
	if _, err := accs.Create(context.Background(), accounts.Input{VPA: "shop@okbank", Bank: "B", DisplayName: "Shop"}); err != nil {
		t.Fatal(err)
	}
	return env{st: st, ctx: context.Background(), ord: ord, sm: sm, svc: New(st, clk, sm, ids)}
}

// paid creates an order and moves it to confirmed, the state refunds apply to.
func (e env) paid(t *testing.T, amount int64) string {
	t.Helper()
	o, _, err := e.ord.Create(e.ctx, orders.CreateInput{AmountPaise: amount, ExpiresIn: time.Hour}, fmt.Sprintf("pay-%d-%d", amount, time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.Tx(e.ctx, func(tx *sql.Tx) error {
		if _, err := e.sm.Transition(e.ctx, tx, o.ID, "matched", nil); err != nil {
			return err
		}
		_, err := e.sm.Transition(e.ctx, tx, o.ID, "confirmed", nil)
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

func (e env) count(t *testing.T, q string) int {
	t.Helper()
	var n int
	if err := e.st.Read().QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPartialThenFullRefund(t *testing.T) {
	e := setup(t)
	id := e.paid(t, 49900)
	if _, err := e.svc.Record(e.ctx, id, 10000, "", "part one"); err != nil {
		t.Fatal(err)
	}
	if s := e.state(t, id); s != "confirmed" {
		t.Fatalf("partial refund keeps the order confirmed, got %s", s)
	}
	if _, err := e.svc.Record(e.ctx, id, 39900, "123456789012", "rest"); err != nil {
		t.Fatal(err)
	}
	if s := e.state(t, id); s != "refunded" {
		t.Fatalf("full refund: state=%s", s)
	}
	if n := e.count(t, `SELECT count(*) FROM outbox WHERE kind='order.refunded'`); n != 1 {
		t.Errorf("order.refunded webhook rows=%d", n)
	}
	list, err := e.svc.List(e.ctx, id)
	if err != nil || len(list) != 2 {
		t.Errorf("list=%d err=%v", len(list), err)
	}
}

func TestOverRefundRejected(t *testing.T) {
	e := setup(t)
	id := e.paid(t, 49900)
	if _, err := e.svc.Record(e.ctx, id, 49901, "", ""); !errors.Is(err, ErrOverRefund) {
		t.Errorf("want ErrOverRefund, got %v", err)
	}
}

func TestWrongStateRejected(t *testing.T) {
	e := setup(t)
	o, _, _ := e.ord.Create(e.ctx, orders.CreateInput{AmountPaise: 49900, ExpiresIn: time.Hour}, "w1")
	if _, err := e.svc.Record(e.ctx, o.ID, 100, "", ""); !errors.Is(err, ErrNotRefundable) {
		t.Errorf("awaiting order: %v", err)
	}
	if _, err := e.svc.Record(e.ctx, "ord_nope", 100, "", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown order: %v", err)
	}
}

func TestInvalidInputRejected(t *testing.T) {
	e := setup(t)
	id := e.paid(t, 49900)
	for _, tc := range []struct {
		amount int64
		utr    string
	}{{0, ""}, {-5, ""}, {100, "12345"}, {100, "abcdefghijkl"}} {
		if _, err := e.svc.Record(e.ctx, id, tc.amount, tc.utr, ""); !errors.Is(err, ErrInvalid) {
			t.Errorf("amount=%d utr=%q: want ErrInvalid, got %v", tc.amount, tc.utr, err)
		}
	}
}

func TestDuplicateUTRRejected(t *testing.T) {
	e := setup(t)
	id := e.paid(t, 49900)
	if _, err := e.svc.Record(e.ctx, id, 100, "111111111111", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Record(e.ctx, id, 100, "111111111111", ""); !errors.Is(err, ErrDuplicate) {
		t.Errorf("want ErrDuplicate, got %v", err)
	}
}

// Concurrent refunds must never exceed what was paid: ten of twenty 10% refunds fit.
func TestConcurrentRefundsNeverExceedPaid(t *testing.T) {
	e := setup(t)
	id := e.paid(t, 100000)
	var ok atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.svc.Record(e.ctx, id, 10000, "", ""); err == nil {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 10 {
		t.Errorf("succeeded=%d want 10", ok.Load())
	}
	var total int64
	_ = e.st.Read().QueryRow(`SELECT COALESCE(SUM(amount_paise),0) FROM refunds WHERE order_id=?`, id).Scan(&total)
	if total != 100000 {
		t.Errorf("refunded total=%d want 100000", total)
	}
}
