// Package review lets the owner decide payments the matcher held. A confirmation here is a human decision,
// recorded with the owner's name in the ledger. Routes in web/admin require the owner role and fresh sign-in.
package review

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/ledger"
	"github.com/opensawce/kaasu/internal/states"
	"github.com/opensawce/kaasu/internal/store"
)

var (
	ErrNotInReview = errors.New("this payment is no longer waiting for a decision")
	ErrNoReason    = errors.New("choose a reason for rejecting")
	ErrNotFound    = errors.New("payment not found")
)

// Reasons are the only reasons a rejection can carry.
var Reasons = []string{"not_received", "wrong_amount", "duplicate", "suspected_fraud", "other"}

// Item is one row of the queue.
type Item struct {
	OrderID, Reference, Reason string
	ExpectedPaise, SaidPaise   int64
	Breakdown                  string
	RawAlert                   string
	PayerMasked, UTR           string
}

type Service struct {
	st  *store.Store
	clk clock.Clock
	sm  *states.Machine
}

func New(st *store.Store, clk clock.Clock, sm *states.Machine) *Service {
	return &Service{st: st, clk: clk, sm: sm}
}

// Queue lists orders waiting for a decision, oldest first.
func (s *Service) Queue(ctx context.Context) ([]Item, error) {
	rows, err := s.st.Read().QueryContext(ctx, `SELECT o.id, COALESCE(o.reference,''), o.amount_paise,
		COALESCE(m.reason,''), COALESCE(m.breakdown,'{}'), COALESCE(a.amount_paise,0), COALESCE(a.utr,''), COALESCE(a.payer_masked,'')
		FROM orders o
		LEFT JOIN matches m ON m.order_id = o.id AND m.decision = 'review'
		LEFT JOIN alerts a ON a.id = m.alert_id
		WHERE o.state = 'review' ORDER BY o.created_at, o.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.OrderID, &it.Reference, &it.ExpectedPaise, &it.Reason, &it.Breakdown, &it.SaidPaise, &it.UTR, &it.PayerMasked); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// Detail returns one item with the raw alert text, escaped by the caller before display.
func (s *Service) Detail(ctx context.Context, orderID string) (Item, error) {
	items, err := s.Queue(ctx)
	if err != nil {
		return Item{}, err
	}
	for _, it := range items {
		if it.OrderID == orderID {
			var raw []byte
			_ = s.st.Read().QueryRowContext(ctx, `SELECT e.body FROM alert_sources src JOIN evidence e ON e.id = src.evidence_id
				JOIN matches m ON m.alert_id = src.alert_id WHERE m.order_id = ? LIMIT 1`, orderID).Scan(&raw)
			it.RawAlert = string(raw)
			return it, nil
		}
	}
	return Item{}, ErrNotFound
}

// Confirm moves a held payment to confirmed (or late-paid when its window has passed). Callers check role and freshness.
func (s *Service) Confirm(ctx context.Context, orderID, actor string) error {
	ctx = ledger.WithActor(ctx, actor)
	return s.st.Tx(ctx, func(tx *sql.Tx) error {
		var state, expires string
		err := tx.QueryRowContext(ctx, `SELECT state, expires_at FROM orders WHERE id = ?`, orderID).Scan(&state, &expires)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if state != "review" {
			return ErrNotInReview
		}
		to := "confirmed"
		if t, err := time.Parse(time.RFC3339, expires); err == nil && !s.clk.Now().Before(t) {
			to = "late_paid"
		}
		if _, err := s.sm.Transition(ctx, tx, orderID, to, map[string]any{"decided_by": actor}); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE matches SET decision = 'auto', decided_by = ? WHERE order_id = ? AND decision = 'review'`, actor, orderID)
		return err
	})
}

// Reject closes a held payment with a reason from the list.
func (s *Service) Reject(ctx context.Context, orderID, reason, actor string) error {
	if !validReason(reason) {
		return ErrNoReason
	}
	ctx = ledger.WithActor(ctx, actor)
	return s.st.Tx(ctx, func(tx *sql.Tx) error {
		var state string
		err := tx.QueryRowContext(ctx, `SELECT state FROM orders WHERE id = ?`, orderID).Scan(&state)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if state != "review" {
			return ErrNotInReview
		}
		if _, err := s.sm.Transition(ctx, tx, orderID, "rejected", map[string]any{"reason": reason, "decided_by": actor}); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE matches SET decision = 'rejected', decided_by = ?, reason = ? WHERE order_id = ? AND decision = 'review'`, actor, reason, orderID)
		return err
	})
}

func validReason(r string) bool {
	for _, x := range Reasons {
		if x == r {
			return true
		}
	}
	return false
}

// Badge is the number of payments waiting, for the header badge and the apps.
func (s *Service) Badge(ctx context.Context) (int, error) {
	var n int
	err := s.st.Read().QueryRowContext(ctx, `SELECT count(*) FROM orders WHERE state = 'review'`).Scan(&n)
	return n, err
}
