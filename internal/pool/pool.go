// Package pool hands out a unique payable amount per (account, base amount), so an alert's exact paise identifies its order.
// Slots are base+0 .. base+99 paise. A reservation is held until the order expires plus a grace period.
package pool

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/ledger"
	"github.com/opensawce/kaasu/internal/store"
)

const (
	slots        = 100
	graceMinutes = 10 // ponytail: fixed grace; read from settings when the settings screen lands
)

// ErrPoolFull means every slot for this account and base amount is held.
var ErrPoolFull = errors.New("no free amount for this account and base amount")

type Pool struct {
	st           *store.Store
	clk          clock.Clock
	graceMinutes int
}

func New(st *store.Store, clk clock.Clock) *Pool {
	return &Pool{st: st, clk: clk, graceMinutes: graceMinutes}
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// Reserve runs inside the caller's transaction. It takes the first free amount from base+0 to base+99.
func (p *Pool) Reserve(ctx context.Context, tx *sql.Tx, accountID, orderID string, basePaise int64, orderExpiresAt time.Time) (int64, error) {
	now := p.clk.Now()
	// Free slots whose hold has run out, then find the ones still held in this range.
	if _, err := tx.ExecContext(ctx, `DELETE FROM amount_reservations WHERE account_id = ? AND reserved_until <= ?`, accountID, ts(now)); err != nil {
		return 0, fmt.Errorf("pool: sweep: %w", err)
	}
	rows, err := tx.QueryContext(ctx, `SELECT amount_paise FROM amount_reservations WHERE account_id = ? AND amount_paise BETWEEN ? AND ?`,
		accountID, basePaise, basePaise+slots-1)
	if err != nil {
		return 0, fmt.Errorf("pool: read: %w", err)
	}
	taken := map[int64]bool{}
	for rows.Next() {
		var a int64
		if err := rows.Scan(&a); err != nil {
			rows.Close()
			return 0, err
		}
		taken[a] = true
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for amt := basePaise; amt < basePaise+slots; amt++ {
		if taken[amt] {
			continue
		}
		until := orderExpiresAt.Add(time.Duration(p.graceMinutes) * time.Minute)
		if _, err := tx.ExecContext(ctx, `INSERT INTO amount_reservations(account_id, amount_paise, order_id, reserved_until) VALUES(?,?,?,?)`,
			accountID, amt, orderID, ts(until)); err != nil {
			return 0, fmt.Errorf("pool: reserve: %w", err)
		}
		return amt, nil
	}
	return 0, ErrPoolFull
}

// Release frees the order's slot, if it has one.
func (p *Pool) Release(ctx context.Context, tx *sql.Tx, orderID string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM amount_reservations WHERE order_id = ?`, orderID); err != nil {
		return fmt.Errorf("pool: release: %w", err)
	}
	return nil
}

type queued struct {
	id        string
	base      int64
	expiresAt time.Time
}

// PromoteQueued gives free slots to queued orders on the account, oldest first. Expired queued orders are skipped.
func (p *Pool) PromoteQueued(ctx context.Context, tx *sql.Tx, accountID string) ([]string, error) {
	now := p.clk.Now()
	rows, err := tx.QueryContext(ctx, `SELECT id, base_amount_paise, expires_at FROM orders
		WHERE account_id = ? AND state = 'queued' AND expires_at > ? ORDER BY queued_at, id`, accountID, ts(now))
	if err != nil {
		return nil, fmt.Errorf("pool: queue: %w", err)
	}
	var waiting []queued
	for rows.Next() {
		var q queued
		var exp string
		if err := rows.Scan(&q.id, &q.base, &exp); err != nil {
			rows.Close()
			return nil, err
		}
		if q.expiresAt, err = time.Parse(time.RFC3339, exp); err != nil {
			rows.Close()
			return nil, fmt.Errorf("pool: parse expiry: %w", err)
		}
		waiting = append(waiting, q)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	var promoted []string
	for _, q := range waiting {
		amt, err := p.Reserve(ctx, tx, accountID, q.id, q.base, q.expiresAt)
		if errors.Is(err, ErrPoolFull) {
			break
		}
		if err != nil {
			return promoted, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE orders SET amount_paise = ?, state = 'awaiting', queued_at = NULL, updated_at = ? WHERE id = ?`,
			amt, ts(now), q.id); err != nil {
			return promoted, fmt.Errorf("pool: promote: %w", err)
		}
		if _, err := ledger.Append(ctx, tx, p.clk, ledger.Entry{Kind: "order.promoted", OrderID: q.id,
			Payload: map[string]any{"amount_paise": amt}}); err != nil {
			return promoted, err
		}
		promoted = append(promoted, q.id)
	}
	return promoted, nil
}
