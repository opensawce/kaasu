package evidence

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/testutil"
)

var t0 = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

func setup(t *testing.T) (*Service, *sql.DB, func(func(*sql.Tx) error) error) {
	t.Helper()
	st := testutil.OpenStore(t)
	svc := New(clock.NewFake(t0), &testutil.Seq{})
	if err := st.Tx(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO devices(id,label,kind,public_key,paired_at) VALUES('dev_1','Phone','android',x'00','2026-01-01T00:00:00Z')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	tx := func(fn func(*sql.Tx) error) error { return st.Tx(context.Background(), fn) }
	return svc, st.Read(), tx
}

func save(t *testing.T, svc *Service, tx func(func(*sql.Tx) error) error, raw string) (string, bool) {
	t.Helper()
	var id string
	var dup bool
	if err := tx(func(tx *sql.Tx) error {
		var err error
		id, dup, err = svc.SaveEvidence(context.Background(), tx, "device", "dev_1", []byte(raw))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return id, dup
}

func alert(t *testing.T, svc *Service, tx func(func(*sql.Tx) error) error, evID string, p Parsed) (string, bool) {
	t.Helper()
	var id string
	var isNew bool
	if err := tx(func(tx *sql.Tx) error {
		var err error
		id, isNew, err = svc.SaveAlert(context.Background(), tx, evID, p)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return id, isNew
}

func count(t *testing.T, db *sql.DB, q string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSameRawBodyOneEvidenceRow(t *testing.T) {
	svc, db, tx := setup(t)
	a, dup1 := save(t, svc, tx, "Rs.499.07 credited UTR 629812345678")
	b, dup2 := save(t, svc, tx, "Rs.499.07 credited UTR 629812345678")
	if a != b || dup1 || !dup2 {
		t.Fatalf("ids %s %s dup %v %v", a, b, dup1, dup2)
	}
	if n := count(t, db, `SELECT count(*) FROM evidence`); n != 1 {
		t.Errorf("evidence rows=%d", n)
	}
}

func TestSmsAndEmailSameUTROneAlertTwoSources(t *testing.T) {
	svc, db, tx := setup(t)
	sms, _ := save(t, svc, tx, "sms text")
	mail := ""
	if err := tx(func(tx *sql.Tx) error {
		var err error
		mail, _, err = svc.SaveEvidence(context.Background(), tx, "email", "", []byte("email text"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	p := Parsed{Bank: "HDFC", AmountPaise: 49907, UTR: "629812345678", OccurredAt: t0, Parser: "hdfc"}
	a1, new1 := alert(t, svc, tx, sms, p)
	a2, new2 := alert(t, svc, tx, mail, p)
	if a1 != a2 || !new1 || new2 {
		t.Fatalf("want one alert: %s %s new %v %v", a1, a2, new1, new2)
	}
	if n := count(t, db, `SELECT count(*) FROM alerts`); n != 1 {
		t.Errorf("alerts=%d", n)
	}
	if n := count(t, db, `SELECT count(*) FROM alert_sources`); n != 2 {
		t.Errorf("sources=%d", n)
	}
}

func TestDifferentUTRTwoAlerts(t *testing.T) {
	svc, db, tx := setup(t)
	e1, _ := save(t, svc, tx, "one")
	e2, _ := save(t, svc, tx, "two")
	alert(t, svc, tx, e1, Parsed{AmountPaise: 100, UTR: "111111111111", OccurredAt: t0})
	alert(t, svc, tx, e2, Parsed{AmountPaise: 100, UTR: "222222222222", OccurredAt: t0})
	if n := count(t, db, `SELECT count(*) FROM alerts`); n != 2 {
		t.Errorf("alerts=%d", n)
	}
}

func TestNoUTRFingerprintDedupe(t *testing.T) {
	svc, db, tx := setup(t)
	e1, _ := save(t, svc, tx, "one")
	e2, _ := save(t, svc, tx, "two")
	p := Parsed{AccountHint: "1234", AmountPaise: 500, OccurredAt: t0}
	_, n1 := alert(t, svc, tx, e1, p)
	_, n2 := alert(t, svc, tx, e2, p)
	if !n1 || n2 || count(t, db, `SELECT count(*) FROM alerts`) != 1 {
		t.Error("same fingerprint must dedupe when UTR is missing")
	}
}

func TestFingerprintBucketBoundary(t *testing.T) {
	a := Fingerprint("1234", 500, time.Unix(300*10+10, 0))
	b := Fingerprint("1234", 500, time.Unix(300*10+299, 0))
	c := Fingerprint("1234", 500, time.Unix(300*11, 0))
	if a != b {
		t.Error("same 5-minute bucket must give the same fingerprint")
	}
	if a == c {
		t.Error("crossing the bucket boundary must change the fingerprint")
	}
	if Fingerprint("1234", 501, time.Unix(3000, 0)) == Fingerprint("1234", 500, time.Unix(3000, 0)) {
		t.Error("amount must be part of the fingerprint")
	}
}

func TestMaskVPA(t *testing.T) {
	tests := map[string]string{
		"ravi.kumar@okicici": "ra***@okicici",
		"ab@okicici":         "a***@okicici",
		"a@okicici":          "a***@okicici",
		"":                   "***",
		"noatsign":           "***",
		"x@":                 "x***@",
	}
	for in, want := range tests {
		if got := MaskVPA(in); got != want {
			t.Errorf("MaskVPA(%q)=%q want %q", in, got, want)
		}
	}
}

func TestConcurrentDuplicateSavesOneAlert(t *testing.T) {
	svc, db, tx := setup(t)
	var ids []string
	for i := 0; i < 20; i++ {
		id, _ := save(t, svc, tx, "body-"+strings.Repeat("x", i))
		ids = append(ids, id)
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(ids))
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			errs <- tx(func(tx *sql.Tx) error {
				_, _, err := svc.SaveAlert(context.Background(), tx, id, Parsed{AmountPaise: 700, UTR: "333333333333", OccurredAt: t0})
				return err
			})
		}(id)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := count(t, db, `SELECT count(*) FROM alerts`); n != 1 {
		t.Errorf("alerts=%d", n)
	}
}

func TestRawBodyStoredByteForByte(t *testing.T) {
	svc, db, tx := setup(t)
	raw := []byte{0x00, 0xff, 0xfe, 'R', 's', '\n', 0xc3}
	var id string
	if err := tx(func(tx *sql.Tx) error {
		var err error
		id, _, err = svc.SaveEvidence(context.Background(), tx, "device", "dev_1", raw)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var got []byte
	if err := db.QueryRow(`SELECT body FROM evidence WHERE id=?`, id).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if string(got) != string(raw) {
		t.Errorf("body changed: %x", got)
	}
}

func TestLedgerNeverHoldsRawText(t *testing.T) {
	svc, db, tx := setup(t)
	e, _ := save(t, svc, tx, "SECRET RAW TEXT ravi.kumar@okicici")
	alert(t, svc, tx, e, Parsed{AmountPaise: 100, UTR: "444444444444", PayerMasked: "ra***@okicici", OccurredAt: t0})
	var payloads string
	rows, err := db.Query(`SELECT payload FROM ledger`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		_ = rows.Scan(&p)
		payloads += p
	}
	if strings.Contains(payloads, "SECRET RAW TEXT") || strings.Contains(payloads, "ravi.kumar") {
		t.Errorf("ledger leaks raw text: %s", payloads)
	}
}

func TestInvalidSourceRejected(t *testing.T) {
	svc, _, tx := setup(t)
	err := tx(func(tx *sql.Tx) error {
		_, _, err := svc.SaveEvidence(context.Background(), tx, "fax", "", []byte("x"))
		return err
	})
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("want ErrInvalid, got %v", err)
	}
}

func TestUTRlessAlertThenUTRAlertIsOnePayment(t *testing.T) {
	svc, db, tx := setup(t)
	e1, _ := save(t, svc, tx, "sms")
	e2, _ := save(t, svc, tx, "email")
	id1, _ := alert(t, svc, tx, e1, Parsed{AccountHint: "1234", AmountPaise: 500, OccurredAt: t0})
	id2, isNew := alert(t, svc, tx, e2, Parsed{AccountHint: "1234", AmountPaise: 500, UTR: "123456789012", OccurredAt: t0})
	if isNew || id1 != id2 || count(t, db, `SELECT count(*) FROM alerts`) != 1 || count(t, db, `SELECT count(*) FROM alerts WHERE utr = '123456789012'`) != 1 {
		t.Fatal("the email with a UTR must join the earlier UTR-less alert and give it the UTR")
	}
}
