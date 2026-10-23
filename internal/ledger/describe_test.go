package ledger

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/testutil"
)

func TestDescribeOKAndBroken(t *testing.T) {
	ok := Result{Rows: 3, OK: true}
	if got := ok.Describe(); got != "ledger intact: 3 rows, chain valid" {
		t.Errorf("ok: %q", got)
	}
	bad := Result{Rows: 4, FirstBadSeq: 2, Reason: "row contents do not match its hash"}
	if got := bad.Describe(); !strings.Contains(got, "BROKEN at row 2") || !strings.Contains(got, "do not match") {
		t.Errorf("broken: %q", got)
	}
}

func TestAppendRejectsEmptyKind(t *testing.T) {
	s := testutil.OpenStore(t)
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	err := s.Tx(context.Background(), func(tx *sql.Tx) error {
		_, err := Append(context.Background(), tx, clk, Entry{})
		return err
	})
	if err == nil {
		t.Fatal("empty kind must be rejected")
	}
}

func TestAppendRejectsUnmarshalablePayload(t *testing.T) {
	s := testutil.OpenStore(t)
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	err := s.Tx(context.Background(), func(tx *sql.Tx) error {
		_, err := Append(context.Background(), tx, clk, Entry{Kind: "x", Payload: map[string]any{"ch": make(chan int)}})
		return err
	})
	if err == nil {
		t.Fatal("payload that cannot be encoded must be rejected")
	}
}

func TestAppendDefaultsActorAndEmptyPayload(t *testing.T) {
	s := testutil.OpenStore(t)
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err := s.Tx(context.Background(), func(tx *sql.Tx) error {
		_, err := Append(context.Background(), tx, clk, Entry{Kind: "k"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var actor, payload string
	_ = s.Read().QueryRow(`SELECT actor, payload FROM ledger WHERE seq=1`).Scan(&actor, &payload)
	if actor != "system" || payload != "{}" {
		t.Errorf("actor=%q payload=%q", actor, payload)
	}
}
