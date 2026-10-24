package recon

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
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

var acctSeq atomic.Int64

// mkAccount creates a receiving account for statements to belong to.
func mkAccount(t testing.TB, st *store.Store) string {
	t.Helper()
	clk := clock.NewFake(t0)
	acc, err := accounts.New(st, clk, &testutil.Seq{}).Create(context.Background(), accounts.Input{VPA: fmt.Sprintf("shop%d@okbank", acctSeq.Add(1)), Bank: "B", DisplayName: "Shop"})
	if err != nil {
		t.Fatal(err)
	}
	return acc.ID
}

func TestParsePaiseInIntegerMaths(t *testing.T) {
	for in, want := range map[string]int64{"499.07": 49907, "1,25,000.50": 12500050, "5": 500, "": 0} {
		got, err := paise(in)
		if in == "" {
			if err != nil || got != 0 {
				t.Errorf("empty should be zero: %d %v", got, err)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("%q: %d %v want %d", in, got, err, want)
		}
	}
	if _, err := paise("1.2.3"); err == nil {
		t.Error("bad amount must fail")
	}
}

func TestImportHDFCFixture(t *testing.T) {
	st := testutil.OpenStore(t)
	accID := mkAccount(t, st)
	raw, _ := os.ReadFile("testdata/hdfc.csv")
	var batch string
	var rows int
	err := st.Tx(context.Background(), func(tx *sql.Tx) error {
		var err error
		batch, rows, err = ImportCSV(context.Background(), tx, bytes.NewReader(raw), hdfcMapping(), accID)
		return err
	})
	if err != nil || rows != 3 || batch == "" {
		t.Fatalf("batch=%s rows=%d err=%v", batch, rows, err)
	}
	var credits, debits int64
	_ = st.Read().QueryRow(`SELECT SUM(credit_paise), SUM(debit_paise) FROM statement_rows`).Scan(&credits, &debits)
	if credits != 149907 || debits != 25000 {
		t.Errorf("credits=%d debits=%d", credits, debits)
	}
}

func TestDuplicateImportIsIdempotent(t *testing.T) {
	st := testutil.OpenStore(t)
	accID := mkAccount(t, st)
	raw, _ := os.ReadFile("testdata/hdfc.csv")
	for i := 0; i < 2; i++ {
		_ = st.Tx(context.Background(), func(tx *sql.Tx) error {
			_, _, err := ImportCSV(context.Background(), tx, bytes.NewReader(raw), hdfcMapping(), accID)
			return err
		})
	}
	var n int
	_ = st.Read().QueryRow(`SELECT count(*) FROM statement_rows`).Scan(&n)
	if n != 3 {
		t.Errorf("rows=%d want 3 after importing the same file twice", n)
	}
}

func TestHugeFileRejected(t *testing.T) {
	st := testutil.OpenStore(t)
	accID := mkAccount(t, st)
	big := strings.Repeat("a,b,c,d,e\n", (10<<20)/10+10)
	err := st.Tx(context.Background(), func(tx *sql.Tx) error {
		_, _, err := ImportCSV(context.Background(), tx, strings.NewReader(big), hdfcMapping(), accID)
		return err
	})
	if err == nil {
		t.Error("files over 10 MB must be rejected")
	}
}

func TestUnknownMappingRejected(t *testing.T) {
	if _, ok := MappingFor("nope"); ok {
		t.Error("unknown bank must not have a mapping")
	}
}

func TestBuckets(t *testing.T) {
	st := testutil.OpenStore(t)
	clk := clock.NewFake(t0)
	ids := &testutil.Seq{}
	accs := accounts.New(st, clk, ids)
	ord := orders.New(st, clk, ids, accs)
	sm := states.New(clk, pool.New(st, clk))
	acc, _ := accs.Create(context.Background(), accounts.Input{VPA: "shop@okbank", Bank: "B", DisplayName: "Shop"})
	ctx := context.Background()
	matchedOrder, _, _ := ord.Create(ctx, orders.CreateInput{AmountPaise: 49907, AccountID: acc.ID, Reference: "R1", ExpiresIn: time.Hour}, "r1")
	missingOrder, _, _ := ord.Create(ctx, orders.CreateInput{AmountPaise: 77700, AccountID: acc.ID, ExpiresIn: time.Hour}, "r2")
	for _, id := range []string{matchedOrder.ID, missingOrder.ID} {
		_ = st.Tx(ctx, func(tx *sql.Tx) error {
			if _, err := sm.Transition(ctx, tx, id, "matched", nil); err != nil {
				return err
			}
			_, err := sm.Transition(ctx, tx, id, "confirmed", nil)
			return err
		})
	}
	raw, _ := os.ReadFile("testdata/hdfc.csv")
	_ = st.Tx(ctx, func(tx *sql.Tx) error {
		_, _, err := ImportCSV(ctx, tx, bytes.NewReader(raw), hdfcMapping(), acc.ID)
		return err
	})
	svc := New(st, sm, ids)
	rep, err := svc.Reconcile(ctx, t0.Add(-time.Hour), t0.Add(48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Matched) != 1 || rep.Matched[0].OrderID != matchedOrder.ID {
		t.Errorf("matched=%+v", rep.Matched)
	}
	if len(rep.ConfirmedButMissing) != 1 || rep.ConfirmedButMissing[0] != missingOrder.ID {
		t.Errorf("confirmed but missing=%v", rep.ConfirmedButMissing)
	}
	if len(rep.CreditedButUnmatched) != 1 {
		t.Errorf("credited but unmatched=%v", rep.CreditedButUnmatched)
	}
	// The confirmed-but-missing order moves to disputed.
	if err := svc.MarkDisputed(ctx, missingOrder.ID); err != nil {
		t.Fatal(err)
	}
	var s string
	_ = st.Read().QueryRow(`SELECT state FROM orders WHERE id=?`, missingOrder.ID).Scan(&s)
	if s != "disputed" {
		t.Errorf("state=%s want disputed", s)
	}
}

func hdfcMapping() Mapping {
	m, _ := MappingFor("hdfc")
	return m
}

func TestShortRecordIsAnImportErrorNotAPanic(t *testing.T) {
	st := testutil.OpenStore(t)
	accID := mkAccount(t, st)
	short := "Date,Narration,Withdrawal Amt.,Deposit Amt.,Reference No.\n23/10/2026,x,,1.00\n"
	err := st.Tx(context.Background(), func(tx *sql.Tx) error {
		_, _, err := ImportCSV(context.Background(), tx, strings.NewReader(short), hdfcMapping(), accID)
		return err
	})
	if err == nil {
		t.Fatal("a record with too few columns must fail the import")
	}
}

func TestCreditsOnlyMatchOrdersOfTheSameAccount(t *testing.T) {
	st := testutil.OpenStore(t)
	clk := clock.NewFake(t0)
	ids := &testutil.Seq{}
	accs := accounts.New(st, clk, ids)
	ord := orders.New(st, clk, ids, accs)
	sm := states.New(clk, pool.New(st, clk))
	ctx := context.Background()
	a1, _ := accs.Create(ctx, accounts.Input{VPA: "one@okbank", Bank: "B", DisplayName: "One"})
	a2, _ := accs.Create(ctx, accounts.Input{VPA: "two@okbank", Bank: "B", DisplayName: "Two"})
	o, _, _ := ord.Create(ctx, orders.CreateInput{AmountPaise: 49907, AccountID: a1.ID, ExpiresIn: time.Hour}, "x1")
	if err := st.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := sm.Transition(ctx, tx, o.ID, "matched", nil); err != nil {
			return err
		}
		_, err := sm.Transition(ctx, tx, o.ID, "confirmed", nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile("testdata/hdfc.csv")
	// The statement belongs to the other account, so its 499.07 credit must not prove this order.
	if err := st.Tx(ctx, func(tx *sql.Tx) error {
		_, _, err := ImportCSV(ctx, tx, bytes.NewReader(raw), hdfcMapping(), a2.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	rep, err := New(st, sm, ids).Reconcile(ctx, t0.Add(-time.Hour), t0.Add(48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Matched) != 0 || len(rep.ConfirmedButMissing) != 1 {
		t.Errorf("matched=%v missing=%v", rep.Matched, rep.ConfirmedButMissing)
	}
}
