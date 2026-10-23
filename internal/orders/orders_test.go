package orders

import (
	"context"
	"errors"
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
	rows, err := e.svc.st.Read().Query(`SELECT kind, actor, order_id FROM ledger WHERE order_id IS NOT NULL ORDER BY seq`)
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
