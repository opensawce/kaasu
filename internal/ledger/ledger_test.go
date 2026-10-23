package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/store"
	"github.com/opensawce/kaasu/internal/testutil"
)

var clk = clock.NewFake(time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC))

func appendN(t *testing.T, s *store.Store, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		err := s.Tx(context.Background(), func(tx *sql.Tx) error {
			_, err := Append(context.Background(), tx, clk, Entry{Kind: "order.created", OrderID: fmt.Sprintf("ord_%d", i), Payload: map[string]any{"amount_paise": 49900 + i}})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		clk.Advance(time.Second)
	}
}

func TestAppendBuildsAValidChain(t *testing.T) {
	s := testutil.OpenStore(t)
	appendN(t, s, 3)
	res, err := Verify(context.Background(), s.Read())
	if err != nil || !res.OK || res.Rows != 3 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	var prev string
	_ = s.Read().QueryRow(`SELECT prev_hash FROM ledger WHERE seq=1`).Scan(&prev)
	if prev != genesis {
		t.Errorf("first prev_hash=%q", prev)
	}
	empty := testutil.OpenStore(t)
	if r, _ := Verify(context.Background(), empty.Read()); !r.OK || r.Rows != 0 {
		t.Errorf("empty ledger should verify: %+v", r)
	}
}

func TestTriggersBlockUpdateAndDelete(t *testing.T) {
	s := testutil.OpenStore(t)
	appendN(t, s, 2)
	if _, err := s.Read().Exec(`UPDATE ledger SET kind='x' WHERE seq=1`); err == nil {
		t.Error("UPDATE must be blocked")
	}
	if _, err := s.Read().Exec(`DELETE FROM ledger WHERE seq=1`); err == nil {
		t.Error("DELETE must be blocked")
	}
}

// Review focus 4 and issue #9: tamper with any row, verify fails at that row.
func TestTamperingBreaksTheChainAtThatRow(t *testing.T) {
	cols := map[string]string{
		"ts": "2030-01-01T00:00:00Z", "kind": "order.confirmed", "order_id": "ord_evil",
		"actor": "attacker", "payload": `{"amount_paise":1}`,
	}
	for row := 1; row <= 4; row++ {
		for col, val := range cols {
			t.Run(fmt.Sprintf("row%d/%s", row, col), func(t *testing.T) {
				s := testutil.OpenStore(t)
				appendN(t, s, 4)
				mustExec(t, s, `DROP TRIGGER ledger_no_update`)
				mustExec(t, s, fmt.Sprintf(`UPDATE ledger SET %s=? WHERE seq=?`, col), val, row)
				res, err := Verify(context.Background(), s.Read())
				if err != nil {
					t.Fatal(err)
				}
				if res.OK || res.FirstBadSeq != int64(row) {
					t.Errorf("want break at %d, got %+v", row, res)
				}
			})
		}
	}
}

func TestDeletedRowBreaksTheChain(t *testing.T) {
	s := testutil.OpenStore(t)
	appendN(t, s, 4)
	mustExec(t, s, `DROP TRIGGER ledger_no_delete`)
	mustExec(t, s, `DELETE FROM ledger WHERE seq=2`)
	res, _ := Verify(context.Background(), s.Read())
	if res.OK || res.FirstBadSeq != 3 {
		t.Errorf("want break at 3 (first row after the gap), got %+v", res)
	}
}

func TestRewrittenHashStillBreaksNextRow(t *testing.T) {
	s := testutil.OpenStore(t)
	appendN(t, s, 3)
	mustExec(t, s, `DROP TRIGGER ledger_no_update`)
	mustExec(t, s, `UPDATE ledger SET payload='{"amount_paise":1}' WHERE seq=2`)
	// A careful attacker also recomputes row 2's hash, but row 3 still points at the old one.
	var ts, kind, oid, actor, payload, prev string
	_ = s.Read().QueryRow(`SELECT ts,kind,COALESCE(order_id,''),actor,payload,prev_hash FROM ledger WHERE seq=2`).Scan(&ts, &kind, &oid, &actor, &payload, &prev)
	mustExec(t, s, `UPDATE ledger SET hash=? WHERE seq=2`, rowHash(prev, 2, ts, kind, oid, actor, payload))
	res, _ := Verify(context.Background(), s.Read())
	if res.OK || res.FirstBadSeq != 3 {
		t.Errorf("want break at 3, got %+v", res)
	}
}

func TestAppendRollsBackWithItsTransaction(t *testing.T) {
	s := testutil.OpenStore(t)
	appendN(t, s, 1)
	err := s.Tx(context.Background(), func(tx *sql.Tx) error {
		if _, err := Append(context.Background(), tx, clk, Entry{Kind: "order.created", OrderID: "ord_x"}); err != nil {
			return err
		}
		return errors.New("state change failed")
	})
	if err == nil {
		t.Fatal("expected failure")
	}
	res, _ := Verify(context.Background(), s.Read())
	if !res.OK || res.Rows != 1 {
		t.Errorf("rolled-back append must leave no row: %+v", res)
	}
}

func TestConcurrentAppendsStayChained(t *testing.T) {
	s := testutil.OpenStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = s.Tx(context.Background(), func(tx *sql.Tx) error {
				_, err := Append(context.Background(), tx, clk, Entry{Kind: "admin.login", Actor: fmt.Sprintf("admin:%d", i)})
				return err
			})
		}(i)
	}
	wg.Wait()
	res, err := Verify(context.Background(), s.Read())
	if err != nil || !res.OK || res.Rows != 30 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestActorFromContext(t *testing.T) {
	s := testutil.OpenStore(t)
	ctx := WithActor(context.Background(), "admin:priya")
	_ = s.Tx(ctx, func(tx *sql.Tx) error { _, err := Append(ctx, tx, clk, Entry{Kind: "account.created"}); return err })
	_ = s.Tx(context.Background(), func(tx *sql.Tx) error {
		_, err := Append(context.Background(), tx, clk, Entry{Kind: "order.expired"})
		return err
	})
	var a1, a2 string
	_ = s.Read().QueryRow(`SELECT actor FROM ledger WHERE seq=1`).Scan(&a1)
	_ = s.Read().QueryRow(`SELECT actor FROM ledger WHERE seq=2`).Scan(&a2)
	if a1 != "admin:priya" || a2 != "system" {
		t.Errorf("actors: %q %q", a1, a2)
	}
}

func mustExec(t *testing.T, s *store.Store, q string, args ...any) {
	t.Helper()
	if err := s.Tx(context.Background(), func(tx *sql.Tx) error { _, err := tx.Exec(q, args...); return err }); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}
