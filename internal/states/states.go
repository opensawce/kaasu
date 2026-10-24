// Package states owns the order lifecycle. Every change to orders.state goes through Transition,
// which writes the ledger row and moves amount slots in the same transaction.
package states

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/ledger"
	"github.com/opensawce/kaasu/internal/pool"
)

var (
	ErrIllegal  = errors.New("illegal order transition")
	ErrNotFound = errors.New("order not found")
)

// table is the whole lifecycle. Anything not listed is illegal.
var table = map[string][]string{
	"created":   {"awaiting", "queued", "cancelled"},
	"queued":    {"awaiting", "cancelled", "expired"},
	"awaiting":  {"matched", "review", "cancelled", "expired"},
	"matched":   {"confirmed", "review"},
	"review":    {"confirmed", "late_paid", "rejected", "awaiting"}, // awaiting only before expiry, checked in Transition
	"expired":   {"review"},                                         // a late alert; late alerts always go to review
	"confirmed": {"refunded", "disputed"},
	"late_paid": {"refunded", "disputed"},
	"disputed":  {"confirmed", "refunded"},
}

// Allowed reports whether an order may move from one state to another.
func Allowed(from, to string) bool { return slices.Contains(table[from], to) }

type Machine struct {
	clk  clock.Clock
	pool *pool.Pool
}

func New(clk clock.Clock, p *pool.Pool) *Machine { return &Machine{clk: clk, pool: p} }

// Transition moves an order to `to` inside the caller's transaction and returns the state it was in.
// why is merged into the ledger payload. Illegal moves return ErrIllegal and change nothing.
func (m *Machine) Transition(ctx context.Context, tx *sql.Tx, orderID, to string, why map[string]any) (string, error) {
	var from, accountID, expiresAt string
	err := tx.QueryRowContext(ctx, `SELECT state, account_id, expires_at FROM orders WHERE id = ?`, orderID).Scan(&from, &accountID, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("states: read %s: %w", orderID, err)
	}
	now := m.clk.Now().UTC().Format(time.RFC3339)
	// Both sides are RFC 3339 UTC text, so string order is time order.
	if !Allowed(from, to) || (from == "review" && to == "awaiting" && now >= expiresAt) {
		return from, fmt.Errorf("%w: %s -> %s", ErrIllegal, from, to)
	}
	confirmed := to == "confirmed" || to == "late_paid"
	if _, err := tx.ExecContext(ctx, `UPDATE orders SET state = ?, updated_at = ?,
		confirmed_at = CASE WHEN ? THEN ? ELSE confirmed_at END,
		queued_at = CASE WHEN ? = 'queued' THEN ? ELSE queued_at END WHERE id = ?`, to, now, confirmed, now, to, now, orderID); err != nil {
		return from, fmt.Errorf("states: update %s: %w", orderID, err)
	}

	kind := "order." + to
	if from == "created" && to == "awaiting" {
		kind = "order.created" // creation keeps the name existing ledgers and tests use
	}
	payload := map[string]any{"from": from, "to": to}
	for k, v := range why {
		if k != "from" && k != "to" {
			payload[k] = v
		}
	}
	if _, err := ledger.Append(ctx, tx, m.clk, ledger.Entry{Kind: kind, OrderID: orderID, Payload: payload}); err != nil {
		return from, err
	}
	if ev, ok := EventFor(to); ok {
		if err := m.outbox(ctx, tx, orderID, ev); err != nil {
			return from, err
		}
	}

	switch to {
	case "refunded":
		// Only a refund frees the amount at once. Cancelled, rejected and expired orders keep their slot until
		// the grace window ends (reserved_until), so a late payment cannot be credited to a reused amount.
		// The sweep (Service.Sweep) promotes queued orders once that window has passed.
		if err := m.pool.Release(ctx, tx, orderID); err != nil {
			return from, err
		}
		promoted, err := m.pool.PromoteQueued(ctx, tx, accountID)
		if err != nil {
			return from, err
		}
		for _, id := range promoted {
			if _, err := m.Transition(ctx, tx, id, "awaiting", map[string]any{"reason": "slot_freed"}); err != nil {
				return from, err
			}
		}
	}
	return from, nil
}

// EventFor names the webhook event a state change sends. Other states send nothing.
func EventFor(to string) (string, bool) {
	switch to {
	case "confirmed", "late_paid":
		return "order.confirmed", true
	case "review", "rejected", "expired", "cancelled", "refunded", "disputed":
		return "order." + to, true
	}
	return "", false
}

// outbox queues the event in the same transaction as the change. The payload carries no payer data.
func (m *Machine) outbox(ctx context.Context, tx *sql.Tx, orderID, event string) error {
	var o struct {
		id, state, reference, accountID, confirmedAt string
		amount                                       int64
	}
	err := tx.QueryRowContext(ctx, `SELECT id, state, COALESCE(reference,''), account_id, COALESCE(confirmed_at,''), amount_paise
		FROM orders WHERE id = ?`, orderID).Scan(&o.id, &o.state, &o.reference, &o.accountID, &o.confirmedAt, &o.amount)
	if err != nil {
		return fmt.Errorf("states: outbox order: %w", err)
	}
	eventID := "evt_" + randomHex(12)
	now := m.clk.Now().UTC().Format(time.RFC3339)
	payload, err := json.Marshal(map[string]any{
		"event_id": eventID, "type": event, "created_at": now,
		"data": map[string]any{"order": map[string]any{
			"id": o.id, "state": o.state, "amount_paise": o.amount, "reference": o.reference,
			"account_id": o.accountID, "confirmed_at": o.confirmedAt,
		}},
	})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO outbox(id, event_id, kind, payload, status, attempts, next_attempt_at, created_at)
		VALUES(?,?,?,?, 'pending', 0, ?, ?)`, "obx_"+randomHex(12), eventID, event, string(payload), now, now)
	if err != nil {
		return fmt.Errorf("states: outbox insert: %w", err)
	}
	return nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("states: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
