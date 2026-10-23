package setup

import (
	"context"
	"testing"
	"time"

	"github.com/opensawce/kaasu/internal/accounts"
	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/orders"
	"github.com/opensawce/kaasu/internal/testutil"
)

func setup(t *testing.T) (*Wizard, string, *clock.Fake, context.Context) {
	t.Helper()
	st := testutil.OpenStore(t)
	clk := clock.NewFake(time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC))
	ids := &testutil.Seq{}
	accs := accounts.New(st, clk, ids)
	ord := orders.New(st, clk, ids, accs)
	acc, _ := accs.Create(context.Background(), accounts.Input{VPA: "shop@okbank", Bank: "B", DisplayName: "Shop"})
	return New(st, clk, ord), acc.ID, clk, context.Background()
}

func TestStepsSkippableAndReminded(t *testing.T) {
	w, _, _, ctx := setup(t)
	if err := w.Mark(ctx, "bank", "skipped"); err != nil {
		t.Fatal(err)
	}
	if err := w.Mark(ctx, "passkey", "done"); err != nil {
		t.Fatal(err)
	}
	rem, err := w.Reminders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	has := map[string]bool{}
	for _, r := range rem {
		has[r] = true
	}
	if !has["bank"] || has["passkey"] {
		t.Errorf("reminders=%v", rem)
	}
	if err := w.Mark(ctx, "nonsense", "done"); err != ErrUnknownStep {
		t.Errorf("unknown step: %v", err)
	}
	if err := w.Mark(ctx, "bank", "maybe"); err == nil {
		t.Error("bad status must be refused")
	}
}

func TestWizardCreatesOneRupeeTestOrder(t *testing.T) {
	w, acc, _, ctx := setup(t)
	o, err := w.StartTestPayment(ctx, acc, "setup-1")
	if err != nil {
		t.Fatal(err)
	}
	if o.AmountPaise != 100 || o.State != "awaiting" {
		t.Errorf("test order: %+v", o)
	}
}

func TestHelpShownAfterThreeMinutes(t *testing.T) {
	w, _, clk, _ := setup(t)
	created := clk.Now().Format(time.RFC3339)
	if w.NeedsHelp(created) {
		t.Error("no help at once")
	}
	clk.Advance(HelpAfter)
	if !w.NeedsHelp(created) {
		t.Error("help after three minutes")
	}
}

// The test order must never count in today's money: dashboards filter it by reference.
func TestTestReferenceIsStable(t *testing.T) {
	if TestReference != "setup-test" {
		t.Errorf("reference changed to %s; update the totals filter too", TestReference)
	}
}
