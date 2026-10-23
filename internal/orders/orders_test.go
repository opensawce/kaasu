package orders

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/opensawce/kaasu/internal/accounts"
	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/ledger"
	"github.com/opensawce/kaasu/internal/testutil"
)

type env struct {
	svc *Service
	acc accounts.Account
	clk *clock.Fake
	ctx context.Context
}

func setup(t *testing.T) env {
	t.Helper()
	st := testutil.OpenStore(t)
	clk := clock.NewFake(time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC))
	ids := &testutil.Seq{}
	accs := accounts.New(st, clk, ids)
	ctx := context.Background()
	acc, err := accs.Create(ctx, accounts.Input{VPA: "sharma.sweets@okhdfcbank", Bank: "HDFC", DisplayName: "Sharma Sweets"})
	if err != nil {
		t.Fatal(err)
	}
	return env{svc: New(st, clk, ids, accs), acc: acc, clk: clk, ctx: ctx}
}

func TestCreateOrder(t *testing.T) {
	e := setup(t)
	o, created, err := e.svc.Create(e.ctx, CreateInput{AmountPaise: 49900, Reference: "INV-1042", Note: "Order 1042"}, "key-1")
	if err != nil || !created {
		t.Fatalf("err=%v created=%v", err, created)
	}
	if o.State != "awaiting" || o.AccountID != e.acc.ID || o.BaseAmountPaise != 49900 || o.Currency != "INR" {
		t.Errorf("unexpected order %+v", o)
	}
	if o.ExpiresAt != "2026-01-01T10:15:00Z" {
		t.Errorf("default expiry should be 15 minutes, got %s", o.ExpiresAt)
	}
	if !strings.HasPrefix(o.UPILink, "upi://pay?pa=sharma.sweets%40okhdfcbank&pn=Sharma%20Sweets&am=") {
		t.Errorf("link=%s", o.UPILink)
	}
	got, err := e.svc.Get(e.ctx, o.ID)
	if err != nil || got != o {
		t.Fatalf("get=%+v err=%v", got, err)
	}
}

func TestIdempotency(t *testing.T) {
	e := setup(t)
	in := CreateInput{AmountPaise: 49900, Reference: "INV-1"}
	a, created, err := e.svc.Create(e.ctx, in, "k")
	if err != nil || !created {
		t.Fatal(err)
	}
	b, created, err := e.svc.Create(e.ctx, in, "k")
	if err != nil || created || b.ID != a.ID {
		t.Fatalf("same key and body must replay the first order: %+v created=%v err=%v", b, created, err)
	}
	other := in
	other.AmountPaise = 50000
	if _, _, err := e.svc.Create(e.ctx, other, "k"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Errorf("same key with a different body must conflict, got %v", err)
	}
	if _, _, err := e.svc.Create(e.ctx, in, ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("missing key must be rejected, got %v", err)
	}
}

func TestValidation(t *testing.T) {
	e := setup(t)
	tests := []struct {
		name string
		in   CreateInput
	}{
		{"zero", CreateInput{AmountPaise: 0}},
		{"negative", CreateInput{AmountPaise: -100}},
		{"below minimum", CreateInput{AmountPaise: 99}},
		{"above maximum", CreateInput{AmountPaise: 10_000_001}},
		{"expiry too short", CreateInput{AmountPaise: 100, ExpiresIn: 10 * time.Second}},
		{"expiry too long", CreateInput{AmountPaise: 100, ExpiresIn: 48 * time.Hour}},
		{"unknown account", CreateInput{AmountPaise: 100, AccountID: "acc_nope"}},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := e.svc.Create(e.ctx, tt.in, "k"+string(rune('a'+i))); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestAccountSelection(t *testing.T) {
	e := setup(t)
	// Only one account: it is the default. Add a second and the choice becomes required.
	accs := e.svc.accs
	b, _ := accs.Create(e.ctx, accounts.Input{VPA: "second@okicici", Bank: "ICICI", DisplayName: "Second"})
	if _, _, err := e.svc.Create(e.ctx, CreateInput{AmountPaise: 100}, "k1"); !errors.Is(err, ErrInvalid) {
		t.Errorf("two accounts and no choice must be rejected, got %v", err)
	}
	o, _, err := e.svc.Create(e.ctx, CreateInput{AmountPaise: 100, AccountID: b.ID}, "k2")
	if err != nil || o.AccountID != b.ID {
		t.Fatalf("o=%+v err=%v", o, err)
	}
	_ = accs.Deactivate(e.ctx, b.ID)
	if _, _, err := e.svc.Create(e.ctx, CreateInput{AmountPaise: 100, AccountID: b.ID}, "k3"); !errors.Is(err, ErrInvalid) {
		t.Errorf("inactive account must be rejected, got %v", err)
	}
}

func TestCancel(t *testing.T) {
	e := setup(t)
	o, _, _ := e.svc.Create(e.ctx, CreateInput{AmountPaise: 100}, "k")
	got, err := e.svc.Cancel(e.ctx, o.ID)
	if err != nil || got.State != "cancelled" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if _, err := e.svc.Cancel(e.ctx, o.ID); !errors.Is(err, ErrNotCancellable) {
		t.Errorf("cancelling twice: %v", err)
	}
	if _, err := e.svc.Cancel(e.ctx, "ord_nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown order: %v", err)
	}
}

func TestOrderLifecycleIsLedgered(t *testing.T) {
	e := setup(t)
	ctx := ledgerActor(e.ctx, "api:key_1")
	o, _, _ := e.svc.Create(ctx, CreateInput{AmountPaise: 100}, "k")
	_, _ = e.svc.Cancel(ctx, o.ID)
	rows, err := e.svc.st.Read().QueryContext(e.ctx, `SELECT kind, actor, order_id FROM ledger WHERE order_id IS NOT NULL ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var k, a, oid string
		_ = rows.Scan(&k, &a, &oid)
		got = append(got, k+"/"+a+"/"+oid)
	}
	want := []string{"order.created/api:key_1/" + o.ID, "order.cancelled/api:key_1/" + o.ID}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v want %v", got, want)
	}
}

func ledgerActor(ctx context.Context, a string) context.Context { return ledger.WithActor(ctx, a) }

func TestPoolFullQueuesOrder(t *testing.T) {
	e := setup(t)
	for i := range 100 {
		o, _, err := e.svc.Create(e.ctx, CreateInput{AmountPaise: 49900}, fmt.Sprintf("p%d", i))
		if err != nil || o.State != "awaiting" || o.AmountPaise != 49900+int64(i) {
			t.Fatalf("order %d: %+v err=%v", i, o, err)
		}
	}
	q, created, err := e.svc.Create(e.ctx, CreateInput{AmountPaise: 49900}, "p100")
	if err != nil || !created {
		t.Fatalf("err=%v created=%v", err, created)
	}
	if q.State != "queued" || q.AmountPaise != 49900 {
		t.Errorf("101st order must be queued at the base amount, got %+v", q)
	}
	var kind string
	if err := e.svc.st.Read().QueryRow(`SELECT kind FROM ledger WHERE order_id = ?`, q.ID).Scan(&kind); err != nil || kind != "order.queued" {
		t.Errorf("ledger kind=%q err=%v", kind, err)
	}
}

func TestCancelKeepsReservationUntilGrace(t *testing.T) {
	e := setup(t)
	first, _, _ := e.svc.Create(e.ctx, CreateInput{AmountPaise: 49900}, "p0")
	for i := 1; i < 100; i++ {
		_, _, _ = e.svc.Create(e.ctx, CreateInput{AmountPaise: 49900}, fmt.Sprintf("p%d", i))
	}
	// Long expiry so the queued order is still live once the first order's grace has ended.
	q, _, _ := e.svc.Create(e.ctx, CreateInput{AmountPaise: 49900, ExpiresIn: 60 * time.Minute}, "pq")
	if q.State != "queued" {
		t.Fatalf("setup: want queued, got %s", q.State)
	}
	if _, err := e.svc.Cancel(e.ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := e.svc.Get(e.ctx, q.ID)
	if got.State != "queued" {
		t.Fatalf("cancelled amount must stay held inside grace; queued order got %s", got.State)
	}
	// Past expiry plus the 10 minute grace, the sweep frees the slot and promotes the queued order.
	e.clk.Advance(26 * time.Minute)
	if err := e.svc.Sweep(e.ctx); err != nil {
		t.Fatal(err)
	}
	got, err := e.svc.Get(e.ctx, q.ID)
	if err != nil || got.State != "awaiting" || got.AmountPaise != 49900 {
		t.Fatalf("after grace the queued order should be promoted: %+v err=%v", got, err)
	}
	if !strings.Contains(got.UPILink, "am=499.00") {
		t.Errorf("promoted order must show its reserved paise in the link: %s", got.UPILink)
	}
}

func TestQueuedOrderHasNoPayableLink(t *testing.T) {
	e := setup(t)
	for i := 0; i < 100; i++ {
		_, _, _ = e.svc.Create(e.ctx, CreateInput{AmountPaise: 49900}, fmt.Sprintf("p%d", i))
	}
	q, _, err := e.svc.Create(e.ctx, CreateInput{AmountPaise: 49900}, "pq")
	if err != nil || q.State != "queued" {
		t.Fatalf("setup: %+v %v", q, err)
	}
	if q.UPILink != "" {
		t.Errorf("queued order must not carry a payable link, got %s", q.UPILink)
	}
	got, _ := e.svc.Get(e.ctx, q.ID)
	if got.UPILink != "" {
		t.Errorf("Get: queued order must not carry a payable link, got %s", got.UPILink)
	}
}

func TestCustomerAmountShowsExactPaise(t *testing.T) {
	e := setup(t)
	a, _, _ := e.svc.Create(e.ctx, CreateInput{AmountPaise: 49907}, "a")
	b, _, _ := e.svc.Create(e.ctx, CreateInput{AmountPaise: 49907}, "b")
	if b.AmountPaise != 49908 {
		t.Fatalf("second order should take base+1, got %d", b.AmountPaise)
	}
	if !strings.Contains(a.UPILink, "am=499.07") || !strings.Contains(b.UPILink, "am=499.08") {
		t.Errorf("links: %s / %s", a.UPILink, b.UPILink)
	}
}

func TestPoolFailureRollsBackOrder(t *testing.T) {
	e := setup(t)
	if err := e.svc.st.Tx(e.ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(e.ctx, `CREATE TRIGGER no_hold BEFORE INSERT ON amount_reservations BEGIN SELECT RAISE(ABORT, 'boom'); END`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.svc.Create(e.ctx, CreateInput{AmountPaise: 49900}, "k"); err == nil {
		t.Fatal("a failed reservation must fail the request")
	}
	var n int
	if err := e.svc.st.Read().QueryRow(`SELECT count(*) FROM orders`).Scan(&n); err != nil || n != 0 {
		t.Errorf("no order may survive a failed reservation: n=%d err=%v", n, err)
	}
}

func TestUnknownOrderErrors(t *testing.T) {
	e := setup(t)
	if _, err := e.svc.Get(e.ctx, "ord_nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("get: %v", err)
	}
	if _, err := e.svc.Cancel(e.ctx, "ord_nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("cancel: %v", err)
	}
}

// A new order must not jump the queue: older queued orders get the freed slots first.
func TestNewOrderDoesNotJumpTheQueue(t *testing.T) {
	e := setup(t)
	for i := 0; i < 100; i++ {
		if _, _, err := e.svc.Create(e.ctx, CreateInput{AmountPaise: 49900, ExpiresIn: time.Minute}, fmt.Sprintf("a%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	waiting, _, err := e.svc.Create(e.ctx, CreateInput{AmountPaise: 49900, ExpiresIn: 60 * time.Minute}, "waiting")
	if err != nil || waiting.State != "queued" {
		t.Fatalf("setup: %+v %v", waiting, err)
	}
	e.clk.Advance(12 * time.Minute) // every first-batch hold (1 min + 10 min grace) has ended
	newer, _, err := e.svc.Create(e.ctx, CreateInput{AmountPaise: 49900, ExpiresIn: 60 * time.Minute}, "newer")
	if err != nil {
		t.Fatal(err)
	}
	old, _ := e.svc.Get(e.ctx, waiting.ID)
	if old.State != "awaiting" || old.AmountPaise != 49900 {
		t.Errorf("the older queued order must be promoted first: %+v", old)
	}
	if newer.State != "awaiting" || newer.AmountPaise == old.AmountPaise {
		t.Errorf("the new order gets the next free amount: %+v", newer)
	}
}

func TestExpireDueAtExactExpiry(t *testing.T) {
	e := setup(t)
	o, _, _ := e.svc.Create(e.ctx, CreateInput{AmountPaise: 49900, ExpiresIn: 15 * time.Minute}, "exp1")
	e.clk.Advance(15*time.Minute - time.Second)
	if n, err := e.svc.ExpireDue(e.ctx); err != nil || n != 0 {
		t.Fatalf("one second early: n=%d err=%v", n, err)
	}
	e.clk.Advance(time.Second)
	if n, err := e.svc.ExpireDue(e.ctx); err != nil || n != 1 {
		t.Fatalf("at expiry: n=%d err=%v", n, err)
	}
	got, _ := e.svc.Get(e.ctx, o.ID)
	if got.State != "expired" {
		t.Errorf("state=%s", got.State)
	}
}
