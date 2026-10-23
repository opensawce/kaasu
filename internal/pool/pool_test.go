package pool

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/opensawce/kaasu/internal/accounts"
	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/store"
	"github.com/opensawce/kaasu/internal/testutil"
)

var t0 = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

type env struct {
	st   *store.Store
	clk  *clock.Fake
	p    *Pool
	acc  accounts.Account
	acc2 accounts.Account
	ctx  context.Context
}

func setup(t testing.TB) env {
	t.Helper()
	st := testutil.OpenStore(t)
	clk := clock.NewFake(t0)
	ids := &testutil.Seq{}
	accs := accounts.New(st, clk, ids)
	ctx := context.Background()
	a, err := accs.Create(ctx, accounts.Input{VPA: "sharma@okhdfcbank", Bank: "HDFC", DisplayName: "Sharma"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := accs.Create(ctx, accounts.Input{VPA: "other@okicici", Bank: "ICICI", DisplayName: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	return env{st: st, clk: clk, p: New(st, clk), acc: a, acc2: b, ctx: ctx}
}

// mkOrder inserts an order row directly; the pool only needs the row to exist (foreign key).
func mkOrder(t testing.TB, e env, id, accountID, state string, expiresAt time.Time, queuedAt string) {
	t.Helper()
	err := e.st.Tx(e.ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(e.ctx, `INSERT INTO orders(id,account_id,base_amount_paise,amount_paise,state,expires_at,created_at,updated_at,queued_at)
			VALUES(?,?,49900,49900,?,?,?,?,?)`, id, accountID, state, expiresAt.Format(time.RFC3339), t0.Format(time.RFC3339), t0.Format(time.RFC3339), nullStr(queuedAt))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// reserve runs Reserve in its own transaction.
func reserve(e env, accountID, orderID string, base int64, expires time.Time) (int64, error) {
	var amt int64
	err := e.st.Tx(e.ctx, func(tx *sql.Tx) error {
		var err error
		amt, err = e.p.Reserve(e.ctx, tx, accountID, orderID, base, expires)
		return err
	})
	return amt, err
}

func release(e env, orderID string) error {
	return e.st.Tx(e.ctx, func(tx *sql.Tx) error { return e.p.Release(e.ctx, tx, orderID) })
}

func TestReserveTakesNearestFreeAmount(t *testing.T) {
	e := setup(t)
	exp := t0.Add(15 * time.Minute)
	mkOrder(t, e, "ord_1", e.acc.ID, "awaiting", exp, "")
	mkOrder(t, e, "ord_2", e.acc.ID, "awaiting", exp, "")
	a, err := reserve(e, e.acc.ID, "ord_1", 49900, exp)
	if err != nil || a != 49900 {
		t.Fatalf("first: amount=%d err=%v", a, err)
	}
	b, err := reserve(e, e.acc.ID, "ord_2", 49900, exp)
	if err != nil || b != 49901 {
		t.Fatalf("second: amount=%d err=%v", b, err)
	}
}

func TestAccountsDoNotShareSlots(t *testing.T) {
	e := setup(t)
	exp := t0.Add(15 * time.Minute)
	mkOrder(t, e, "ord_1", e.acc.ID, "awaiting", exp, "")
	mkOrder(t, e, "ord_2", e.acc2.ID, "awaiting", exp, "")
	a, _ := reserve(e, e.acc.ID, "ord_1", 49900, exp)
	b, _ := reserve(e, e.acc2.ID, "ord_2", 49900, exp)
	if a != 49900 || b != 49900 {
		t.Errorf("different accounts may share an amount: %d %d", a, b)
	}
}

func TestPoolFillsThenFull(t *testing.T) {
	e := setup(t)
	exp := t0.Add(15 * time.Minute)
	for i := range 101 {
		mkOrder(t, e, fmt.Sprintf("ord_%03d", i), e.acc.ID, "awaiting", exp, "")
	}
	for i := range 100 {
		amt, err := reserve(e, e.acc.ID, fmt.Sprintf("ord_%03d", i), 49900, exp)
		if err != nil || amt != 49900+int64(i) {
			t.Fatalf("order %d: amount=%d err=%v", i, amt, err)
		}
	}
	if _, err := reserve(e, e.acc.ID, "ord_100", 49900, exp); !errors.Is(err, ErrPoolFull) {
		t.Fatalf("101st order must get ErrPoolFull, got %v", err)
	}
}

func TestReleaseFreesAmount(t *testing.T) {
	e := setup(t)
	exp := t0.Add(15 * time.Minute)
	for i := range 101 {
		mkOrder(t, e, fmt.Sprintf("ord_%03d", i), e.acc.ID, "awaiting", exp, "")
	}
	for i := range 100 {
		if _, err := reserve(e, e.acc.ID, fmt.Sprintf("ord_%03d", i), 49900, exp); err != nil {
			t.Fatal(err)
		}
	}
	if err := release(e, "ord_042"); err != nil {
		t.Fatal(err)
	}
	amt, err := reserve(e, e.acc.ID, "ord_100", 49900, exp)
	if err != nil || amt != 49942 {
		t.Fatalf("released slot 42 should be reused: amount=%d err=%v", amt, err)
	}
}

func TestExpiredReservationIsReusableAfterGrace(t *testing.T) {
	e := setup(t)
	exp := t0.Add(15 * time.Minute)
	mkOrder(t, e, "ord_1", e.acc.ID, "awaiting", exp, "")
	mkOrder(t, e, "ord_2", e.acc.ID, "awaiting", exp, "")
	if _, err := reserve(e, e.acc.ID, "ord_1", 49900, exp); err != nil {
		t.Fatal(err)
	}
	// Still held at expiry: grace is 10 minutes.
	e.clk.Set(exp.Add(9 * time.Minute))
	if amt, _ := reserve(e, e.acc.ID, "ord_2", 49900, exp); amt != 49901 {
		t.Fatalf("inside grace the slot must stay held, got %d", amt)
	}
	mkOrder(t, e, "ord_3", e.acc.ID, "awaiting", exp, "")
	e.clk.Set(exp.Add(10 * time.Minute))
	if amt, err := reserve(e, e.acc.ID, "ord_3", 49900, exp); err != nil || amt != 49900 {
		t.Fatalf("after reserved_until the slot is free again: amount=%d err=%v", amt, err)
	}
}

func TestPromoteQueuedIsFIFO(t *testing.T) {
	e := setup(t)
	exp := t0.Add(time.Hour)
	mkOrder(t, e, "q_late", e.acc.ID, "queued", exp, "2026-01-01T10:02:00Z")
	mkOrder(t, e, "q_early", e.acc.ID, "queued", exp, "2026-01-01T10:01:00Z")
	mkOrder(t, e, "q_expired", e.acc.ID, "queued", t0.Add(-time.Minute), "2026-01-01T09:59:00Z")
	var got []string
	err := e.st.Tx(e.ctx, func(tx *sql.Tx) error {
		var err error
		got, err = e.p.PromoteQueued(e.ctx, tx, e.acc.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got) != "[q_early q_late]" {
		t.Fatalf("promoted %v, want FIFO by queued_at with expired orders skipped", got)
	}
	var state string
	var amt int64
	if err := e.st.Read().QueryRow(`SELECT state, amount_paise FROM orders WHERE id='q_early'`).Scan(&state, &amt); err != nil {
		t.Fatal(err)
	}
	if state != "awaiting" || amt != 49900 {
		t.Errorf("promoted order: state=%s amount=%d", state, amt)
	}
}

func TestPromoteQueuedStopsWhenPoolFull(t *testing.T) {
	e := setup(t)
	exp := t0.Add(time.Hour)
	for i := range 100 {
		mkOrder(t, e, fmt.Sprintf("ord_%03d", i), e.acc.ID, "awaiting", exp, "")
		if _, err := reserve(e, e.acc.ID, fmt.Sprintf("ord_%03d", i), 49900, exp); err != nil {
			t.Fatal(err)
		}
	}
	mkOrder(t, e, "q_1", e.acc.ID, "queued", exp, "2026-01-01T10:01:00Z")
	var got []string
	_ = e.st.Tx(e.ctx, func(tx *sql.Tx) error {
		var err error
		got, err = e.p.PromoteQueued(e.ctx, tx, e.acc.ID)
		return err
	})
	if len(got) != 0 {
		t.Errorf("no slot is free, nothing may be promoted: %v", got)
	}
}

func TestConcurrentReservationsNeverDoubleBook(t *testing.T) {
	e := setup(t)
	exp := t0.Add(15 * time.Minute)
	const n = 50
	for i := range n {
		mkOrder(t, e, fmt.Sprintf("ord_%02d", i), e.acc.ID, "awaiting", exp, "")
	}
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		seen = map[int64]bool{}
		ok   int
	)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			amt, err := reserve(e, e.acc.ID, fmt.Sprintf("ord_%02d", i), 49900, exp)
			if err != nil {
				t.Errorf("reserve %d: %v", i, err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if seen[amt] {
				t.Errorf("amount %d given twice", amt)
			}
			seen[amt] = true
			ok++
		}()
	}
	wg.Wait()
	if ok != n || len(seen) != n {
		t.Fatalf("succeeded=%d distinct=%d, want %d of each", ok, len(seen), n)
	}
}

// TestNeverSameAmountTwice drives random reserve and release calls over two accounts and checks
// every answer against a model of what is held.
func TestNeverSameAmountTwice(t *testing.T) {
	e := setup(t)
	exp := t0.Add(time.Hour)
	accounts := []string{e.acc.ID, e.acc2.ID}
	const orders = 40
	for i := range orders {
		mkOrder(t, e, fmt.Sprintf("p_%02d", i), accounts[i%2], "awaiting", exp, "")
	}
	rapid.Check(t, func(rt *rapid.T) {
		held := map[string]map[int64]string{} // account -> amount -> order
		owner := map[string]string{}          // order -> account it reserved on
		ops := rapid.IntRange(1, 80).Draw(rt, "ops")
		for range ops {
			idx := rapid.IntRange(0, orders-1).Draw(rt, "order")
			oid := fmt.Sprintf("p_%02d", idx)
			acc := accounts[idx%2]
			if rapid.Bool().Draw(rt, "release") {
				if err := release(e, oid); err != nil {
					rt.Fatal(err)
				}
				if a, ok := owner[oid]; ok {
					for amt, o := range held[a] {
						if o == oid {
							delete(held[a], amt)
						}
					}
					delete(owner, oid)
				}
				continue
			}
			if _, ok := owner[oid]; ok {
				continue // already holds a slot
			}
			base := int64(49900 + rapid.IntRange(0, 2).Draw(rt, "base"))
			amt, err := reserve(e, acc, oid, base, exp)
			if errors.Is(err, ErrPoolFull) {
				continue
			}
			if err != nil {
				rt.Fatal(err)
			}
			if amt < base || amt > base+99 {
				rt.Fatalf("amount %d outside base %d + 0..99", amt, base)
			}
			if other, dup := held[acc][amt]; dup {
				rt.Fatalf("amount %d on %s already held by %s", amt, acc, other)
			}
			if held[acc] == nil {
				held[acc] = map[int64]string{}
			}
			held[acc][amt] = oid
			owner[oid] = acc
		}
		// The table must match the model exactly.
		n := 0
		for _, byAmt := range held {
			n += len(byAmt)
		}
		var rows int
		if err := e.st.Read().QueryRow(`SELECT count(*) FROM amount_reservations`).Scan(&rows); err != nil {
			rt.Fatal(err)
		}
		if rows != n {
			rt.Fatalf("table has %d reservations, model has %d", rows, n)
		}
		// Release everything so the next case starts empty.
		for oid := range owner {
			if err := release(e, oid); err != nil {
				rt.Fatal(err)
			}
		}
	})
}

// Database failures must come back as errors, never as a made-up slot.
func TestDatabaseErrorsAreReturned(t *testing.T) {
	e := setup(t)
	exp := t0.Add(time.Hour)
	mkOrder(t, e, "ord_1", e.acc.ID, "awaiting", exp, "")
	tx, err := e.st.Read().Begin()
	if err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback() // every statement on a finished tx fails
	if _, err := e.p.Reserve(e.ctx, tx, e.acc.ID, "ord_1", 49900, exp); err == nil {
		t.Error("reserve on a finished tx must fail")
	}
	if err := e.p.Release(e.ctx, tx, "ord_1"); err == nil {
		t.Error("release on a finished tx must fail")
	}
	if _, err := e.p.PromoteQueued(e.ctx, tx, e.acc.ID); err == nil {
		t.Error("promote on a finished tx must fail")
	}
	// An unknown account breaks the foreign key on insert.
	err = e.st.Tx(e.ctx, func(tx *sql.Tx) error {
		_, err := e.p.Reserve(e.ctx, tx, "acc_nope", "ord_1", 49900, exp)
		return err
	})
	if err == nil {
		t.Error("reserving for an unknown account must fail")
	}
}

func injectFailure(t *testing.T, e env, ddl string) {
	t.Helper()
	if err := e.st.Tx(e.ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(e.ctx, ddl)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func promote(e env) ([]string, error) {
	var got []string
	err := e.st.Tx(e.ctx, func(tx *sql.Tx) error {
		var err error
		got, err = e.p.PromoteQueued(e.ctx, tx, e.acc.ID)
		return err
	})
	return got, err
}

func TestPromoteFailuresAreReturned(t *testing.T) {
	exp := t0.Add(time.Hour)
	t.Run("malformed expiry", func(t *testing.T) {
		e := setup(t)
		mkOrder(t, e, "q_1", e.acc.ID, "queued", exp, "2026-01-01T10:01:00Z")
		injectFailure(t, e, `UPDATE orders SET expires_at = 'not-a-time' WHERE id = 'q_1'`)
		if _, err := promote(e); err == nil {
			t.Error("a corrupt expiry must be an error")
		}
	})
	t.Run("order update fails", func(t *testing.T) {
		e := setup(t)
		mkOrder(t, e, "q_1", e.acc.ID, "queued", exp, "2026-01-01T10:01:00Z")
		injectFailure(t, e, `CREATE TRIGGER no_update BEFORE UPDATE ON orders BEGIN SELECT RAISE(ABORT, 'boom'); END`)
		if _, err := promote(e); err == nil {
			t.Error("a failed order update must be an error")
		}
	})
	t.Run("ledger write fails", func(t *testing.T) {
		e := setup(t)
		mkOrder(t, e, "q_1", e.acc.ID, "queued", exp, "2026-01-01T10:01:00Z")
		injectFailure(t, e, `CREATE TRIGGER no_ledger BEFORE INSERT ON ledger BEGIN SELECT RAISE(ABORT, 'boom'); END`)
		if _, err := promote(e); err == nil {
			t.Error("a failed ledger write must be an error")
		}
	})
}
