// Package refunds records refunds the merchant made from their own bank app. Kaasu never moves money.
package refunds

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/ledger"
	"github.com/opensawce/kaasu/internal/states"
	"github.com/opensawce/kaasu/internal/store"
)

var (
	ErrNotFound      = errors.New("order not found")
	ErrNotRefundable = errors.New("only a confirmed or late-paid order can be refunded")
	ErrOverRefund    = errors.New("refund is more than the amount still paid")
	ErrInvalid       = errors.New("invalid refund")
	ErrDuplicate     = errors.New("a refund with this UTR is already recorded")
	utrRe            = regexp.MustCompile(`^[0-9]{12}$`)
)

// Refund is one recorded refund.
type Refund struct {
	ID, OrderID, UTR, Note, CreatedAt string
	AmountPaise                       int64
}

type IDSource interface{ New(prefix string) string }

type Service struct {
	st  *store.Store
	clk clock.Clock
	sm  *states.Machine
	ids IDSource
}

func New(st *store.Store, clk clock.Clock, sm *states.Machine, ids IDSource) *Service {
	return &Service{st: st, clk: clk, sm: sm, ids: ids}
}

// Record adds a refund. It is one transaction: the check, the insert and the order change commit together,
// so concurrent refunds can never exceed the amount paid. A refund that returns the full amount moves the order to refunded.
func (s *Service) Record(ctx context.Context, orderID string, amountPaise int64, utr, note string) (Refund, error) {
	if amountPaise <= 0 {
		return Refund{}, fmt.Errorf("%w: amount must be positive", ErrInvalid)
	}
	if utr != "" && !utrRe.MatchString(utr) {
		return Refund{}, fmt.Errorf("%w: UTR must be 12 digits", ErrInvalid)
	}
	r := Refund{ID: s.ids.New("rfd"), OrderID: orderID, AmountPaise: amountPaise, UTR: utr, Note: note,
		CreatedAt: s.clk.Now().UTC().Format(time.RFC3339)}
	err := s.st.Tx(ctx, func(tx *sql.Tx) error {
		var state string
		var paid, refunded int64
		err := tx.QueryRowContext(ctx, `SELECT state, amount_paise,
			(SELECT COALESCE(SUM(amount_paise),0) FROM refunds WHERE order_id = orders.id) FROM orders WHERE id = ?`, orderID).
			Scan(&state, &paid, &refunded)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if state != "confirmed" && state != "late_paid" {
			return ErrNotRefundable
		}
		if amountPaise > paid-refunded {
			return ErrOverRefund
		}
		if utr != "" {
			var n int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM refunds WHERE utr = ?`, utr).Scan(&n); err != nil {
				return err
			}
			if n > 0 {
				return ErrDuplicate
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO refunds(id, order_id, amount_paise, utr, note, created_at) VALUES(?,?,?,?,?,?)`,
			r.ID, orderID, amountPaise, nullable(utr), nullable(note), r.CreatedAt); err != nil {
			return err
		}
		if _, err := ledger.Append(ctx, tx, s.clk, ledger.Entry{Kind: "refund.recorded", OrderID: orderID,
			Payload: map[string]any{"refund_id": r.ID, "amount_paise": amountPaise, "utr": utr}}); err != nil {
			return err
		}
		if refunded+amountPaise == paid {
			if _, err := s.sm.Transition(ctx, tx, orderID, "refunded", map[string]any{"refund_id": r.ID}); err != nil {
				return err
			}
		}
		return nil
	})
	return r, err
}

// List returns the refunds of one order, oldest first.
func (s *Service) List(ctx context.Context, orderID string) ([]Refund, error) {
	rows, err := s.st.Read().QueryContext(ctx, `SELECT id, order_id, amount_paise, COALESCE(utr,''), COALESCE(note,''), created_at
		FROM refunds WHERE order_id = ? ORDER BY created_at, id`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Refund
	for rows.Next() {
		var r Refund
		if err := rows.Scan(&r.ID, &r.OrderID, &r.AmountPaise, &r.UTR, &r.Note, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
