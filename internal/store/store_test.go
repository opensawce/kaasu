package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "kaasu.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func pragma(t *testing.T, s *Store, name string) string {
	t.Helper()
	var v string
	if err := s.db.QueryRow("PRAGMA " + name).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestOpenSetsWALAndForeignKeys(t *testing.T) {
	s := open(t)
	if got := pragma(t, s, "journal_mode"); got != "wal" {
		t.Errorf("journal_mode=%s", got)
	}
	if got := pragma(t, s, "foreign_keys"); got != "1" {
		t.Errorf("foreign_keys=%s", got)
	}
}

func TestMigrateCreatesStrictTables(t *testing.T) {
	s := open(t)
	want := []string{"receiving_accounts", "orders", "amount_reservations", "devices", "nonces", "evidence", "alerts", "matches", "refunds", "ledger", "outbox"}
	for _, name := range want {
		var strict int
		err := s.db.QueryRow(`SELECT strict FROM pragma_table_list WHERE name = ?`, name).Scan(&strict)
		if err != nil {
			t.Errorf("table %s missing: %v", name, err)
			continue
		}
		if strict != 1 {
			t.Errorf("table %s is not STRICT", name)
		}
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	s := open(t)
	var before int
	_ = s.db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&before)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	var after int
	_ = s.db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&after)
	if before == 0 || before != after {
		t.Errorf("migrations before=%d after=%d", before, after)
	}
}

func seed(t *testing.T, s *Store) {
	t.Helper()
	stmts := []string{
		`INSERT INTO receiving_accounts(id,vpa,bank,display_name,created_at) VALUES('acc1','shop@okbank','Bank','Shop','2026-01-01T00:00:00Z')`,
		`INSERT INTO orders(id,account_id,base_amount_paise,amount_paise,state,idempotency_key,request_hash,expires_at,created_at,updated_at)
		 VALUES('o1','acc1',49900,49907,'awaiting','k1','h1','2026-01-01T00:15:00Z','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`,
		`INSERT INTO orders(id,account_id,base_amount_paise,amount_paise,state,idempotency_key,request_hash,expires_at,created_at,updated_at)
		 VALUES('o3','acc1',49900,49908,'awaiting','k3','h3','2026-01-01T00:15:00Z','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`,
		`INSERT INTO devices(id,label,kind,public_key,paired_at) VALUES('d1','Phone','android',x'00','2026-01-01T00:00:00Z')`,
		`INSERT INTO evidence(id,source,device_id,received_at,raw_hash,body) VALUES('e1','device','d1','2026-01-01T00:01:00Z','rh1',x'00')`,
		`INSERT INTO alerts(id,evidence_id,amount_paise,utr,occurred_at,parsed_at) VALUES('a1','e1',49907,'UTR1','2026-01-01T00:01:00Z','2026-01-01T00:01:01Z')`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
}

func TestConstraints(t *testing.T) {
	s := open(t)
	seed(t, s)
	tests := []struct{ name, sql string }{
		{"reservation unique per account and amount", `INSERT INTO amount_reservations(account_id,amount_paise,order_id,reserved_until) VALUES('acc1',49907,'o3','2026-01-01T01:00:00Z')`},
		{"UTR unique", `INSERT INTO alerts(id,evidence_id,amount_paise,utr,occurred_at,parsed_at) VALUES('a2','e1',100,'UTR1','2026-01-01T00:02:00Z','2026-01-01T00:02:01Z')`},
		{"one match per alert", `INSERT INTO matches(id,alert_id,order_id,score,decision,created_at) VALUES('m2','a1','o1',90,'auto','2026-01-01T00:03:00Z')`},
		{"nonce unique per device", `INSERT INTO nonces(device_id,nonce,seen_at) VALUES('d1','n1','2026-01-01T00:00:00Z')`},
		{"idempotency key unique", `INSERT INTO orders(id,account_id,base_amount_paise,amount_paise,state,idempotency_key,request_hash,expires_at,created_at,updated_at)
		 VALUES('o2','acc1',100,100,'awaiting','k1','h2','2026-01-01T00:15:00Z','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`},
		{"foreign key enforced", `INSERT INTO refunds(id,order_id,amount_paise,created_at) VALUES('r1','nope',1,'2026-01-01T00:00:00Z')`},
		{"state check", `UPDATE orders SET state='bogus' WHERE id='o1'`},
		{"amounts positive", `UPDATE orders SET amount_paise=0 WHERE id='o1'`},
	}
	// First insert the rows that the duplicates collide with.
	for _, q := range []string{
		`INSERT INTO amount_reservations(account_id,amount_paise,order_id,reserved_until) VALUES('acc1',49907,'o1','2026-01-01T01:00:00Z')`,
		`INSERT INTO matches(id,alert_id,order_id,score,decision,created_at) VALUES('m1','a1','o1',90,'auto','2026-01-01T00:03:00Z')`,
		`INSERT INTO nonces(device_id,nonce,seen_at) VALUES('d1','n1','2026-01-01T00:00:00Z')`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("setup %q: %v", q, err)
		}
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := s.db.Exec(tt.sql); err == nil {
				t.Error("expected constraint error")
			}
		})
	}
}

func TestTxSerialisesWriters(t *testing.T) {
	s := open(t)
	if _, err := s.db.Exec(`CREATE TABLE counter(n INTEGER NOT NULL) STRICT; INSERT INTO counter VALUES(0)`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- s.Tx(context.Background(), func(tx *sql.Tx) error {
				var n int
				if err := tx.QueryRow(`SELECT n FROM counter`).Scan(&n); err != nil {
					return err
				}
				_, err := tx.Exec(`UPDATE counter SET n = ?`, n+1)
				return err
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var n int
	_ = s.db.QueryRow(`SELECT n FROM counter`).Scan(&n)
	if n != 40 {
		t.Errorf("lost updates: n=%d", n)
	}
}

func TestTxRollsBackOnError(t *testing.T) {
	s := open(t)
	err := s.Tx(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO receiving_accounts(id,vpa,bank,display_name,created_at) VALUES('x','x@y','b','n','t')`); err != nil {
			return err
		}
		return fmt.Errorf("boom")
	})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err=%v", err)
	}
	var n int
	_ = s.db.QueryRow(`SELECT count(*) FROM receiving_accounts`).Scan(&n)
	if n != 0 {
		t.Error("insert survived rollback")
	}
}
