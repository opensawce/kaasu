// Package orders creates, reads and cancels orders. Money is integer paise.
package orders

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/opensawce/kaasu/internal/accounts"
	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/ledger"
	"github.com/opensawce/kaasu/internal/pool"
	"github.com/opensawce/kaasu/internal/store"
	"github.com/opensawce/kaasu/internal/upi"
)

const (
	MinAmountPaise = 100        // ₹1
	MaxAmountPaise = 10_000_000 // ₹1,00,000, the usual UPI per-payment ceiling
	DefaultExpiry  = 15 * time.Minute
	minExpiry      = time.Minute
	maxExpiry      = 24 * time.Hour
)

var (
	ErrInvalid             = errors.New("invalid order")
	ErrNotFound            = errors.New("order not found")
	ErrIdempotencyConflict = errors.New("Idempotency-Key was already used with a different request")
	ErrNotCancellable      = errors.New("order can no longer be cancelled")
)

// Order is the API view of an order. All amounts are paise.
type Order struct {
	ID              string
	AccountID       string
	State           string
	BaseAmountPaise int64 // what the shop asked for
	AmountPaise     int64 // exact amount the customer pays
	Currency        string
	Reference       string
	Note            string
	UPILink         string
	ExpiresAt       string
	CreatedAt       string
	UpdatedAt       string
	ConfirmedAt     string
}

// CreateInput is the body of POST /v1/orders.
type CreateInput struct {
	AmountPaise int64
	AccountID   string // optional when the shop has exactly one active account
	Reference   string // the shop's own reference, for example an invoice number
	Note        string // shown in the customer's UPI app
	ExpiresIn   time.Duration
}

type IDSource interface{ New(prefix string) string }

type Service struct {
	st   *store.Store
	clk  clock.Clock
	ids  IDSource
	accs *accounts.Service
	pool *pool.Pool
}

func New(st *store.Store, clk clock.Clock, ids IDSource, accs *accounts.Service) *Service {
	return &Service{st: st, clk: clk, ids: ids, accs: accs, pool: pool.New(st, clk)}
}

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

func requestHash(in CreateInput) string {
	b, _ := json.Marshal(struct {
		A int64
		B string
		R string
		N string
		E int64
	}{in.AmountPaise, in.AccountID, in.Reference, in.Note, int64(in.ExpiresIn)})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (s *Service) chooseAccount(ctx context.Context, id string) (accounts.Account, error) {
	if id != "" {
		a, err := s.accs.Get(ctx, id)
		if errors.Is(err, accounts.ErrNotFound) {
			return a, invalid("unknown receiving account %q", id)
		}
		if err == nil && !a.Active {
			return a, invalid("receiving account %s is inactive", id)
		}
		return a, err
	}
	all, err := s.accs.List(ctx)
	if err != nil {
		return accounts.Account{}, err
	}
	var active []accounts.Account
	for _, a := range all {
		if a.Active {
			active = append(active, a)
		}
	}
	switch len(active) {
	case 0:
		return accounts.Account{}, invalid("add a receiving account first")
	case 1:
		return active[0], nil
	}
	return accounts.Account{}, invalid("account_id is required when there is more than one receiving account")
}

// Create makes an order, or replays the earlier one when the same Idempotency-Key and body are sent again.
// The bool is true only when a new order was made.
func (s *Service) Create(ctx context.Context, in CreateInput, idemKey string) (Order, bool, error) {
	if strings.TrimSpace(idemKey) == "" {
		return Order{}, false, invalid("Idempotency-Key is required")
	}
	if in.ExpiresIn == 0 {
		in.ExpiresIn = DefaultExpiry
	}
	switch {
	case in.AmountPaise < MinAmountPaise || in.AmountPaise > MaxAmountPaise:
		return Order{}, false, invalid("amount_paise must be between %d and %d", MinAmountPaise, MaxAmountPaise)
	case in.ExpiresIn < minExpiry || in.ExpiresIn > maxExpiry:
		return Order{}, false, invalid("expiry must be between %s and %s", minExpiry, maxExpiry)
	}
	hash := requestHash(in)

	var existing string
	var existingHash sql.NullString
	err := s.st.Read().QueryRowContext(ctx, `SELECT id, request_hash FROM orders WHERE idempotency_key = ?`, idemKey).Scan(&existing, &existingHash)
	switch {
	case err == nil:
		if existingHash.String != hash {
			return Order{}, false, ErrIdempotencyConflict
		}
		o, err := s.Get(ctx, existing)
		return o, false, err
	case !errors.Is(err, sql.ErrNoRows):
		return Order{}, false, err
	}

	acc, err := s.chooseAccount(ctx, in.AccountID)
	if err != nil {
		return Order{}, false, err
	}
	now := s.clk.Now().UTC()
	expires := now.Add(in.ExpiresIn)
	o := Order{
		ID: s.ids.New("ord"), AccountID: acc.ID, State: "created", BaseAmountPaise: in.AmountPaise, AmountPaise: in.AmountPaise,
		Currency: "INR", Reference: in.Reference, Note: in.Note,
		ExpiresAt: expires.Format(time.RFC3339), CreatedAt: now.Format(time.RFC3339), UpdatedAt: now.Format(time.RFC3339),
	}
	err = s.st.Tx(ctx, func(tx *sql.Tx) error {
		// Re-check inside the writer so two concurrent requests with one key cannot both insert.
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM orders WHERE idempotency_key = ?`, idemKey).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return errRace
		}
		// The row goes in first so the pool's reservation can reference it; its final state is set below in this same tx.
		if _, err := tx.ExecContext(ctx, `INSERT INTO orders(id,account_id,base_amount_paise,amount_paise,state,reference,note,idempotency_key,request_hash,expires_at,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, o.ID, o.AccountID, o.BaseAmountPaise, o.AmountPaise, o.State, nullable(o.Reference), nullable(o.Note), idemKey, hash, o.ExpiresAt, o.CreatedAt, o.UpdatedAt); err != nil {
			return err
		}
		// Older queued orders get any free slots before this one, so the queue stays fair.
		if _, err := s.pool.PromoteQueued(ctx, tx, acc.ID); err != nil {
			return err
		}
		amt, err := s.pool.Reserve(ctx, tx, acc.ID, o.ID, in.AmountPaise, expires)
		switch {
		case errors.Is(err, pool.ErrPoolFull):
			o.State = "queued"
			if _, err := tx.ExecContext(ctx, `UPDATE orders SET state = 'queued', queued_at = ? WHERE id = ?`, o.CreatedAt, o.ID); err != nil {
				return err
			}
			_, err = ledger.Append(ctx, tx, s.clk, ledger.Entry{Kind: "order.queued", OrderID: o.ID,
				Payload: map[string]any{"amount_paise": o.AmountPaise, "account_id": o.AccountID}})
			return err
		case err != nil:
			return err
		}
		o.State, o.AmountPaise = "awaiting", amt
		if _, err := tx.ExecContext(ctx, `UPDATE orders SET state = 'awaiting', amount_paise = ? WHERE id = ?`, amt, o.ID); err != nil {
			return err
		}
		_, err = ledger.Append(ctx, tx, s.clk, ledger.Entry{Kind: "order.created", OrderID: o.ID,
			Payload: map[string]any{"amount_paise": o.AmountPaise, "account_id": o.AccountID, "reference": o.Reference}})
		return err
	})
	if errors.Is(err, errRace) {
		return s.Create(ctx, in, idemKey) // the other request won; this call replays it
	}
	if err != nil {
		return Order{}, false, err
	}
	if o.State == "awaiting" { // a queued order has no unique amount yet, so no link
		o.UPILink = upi.Link(acc.VPA, acc.DisplayName, o.AmountPaise, o.Note)
	}
	return o, true, nil
}

var errRace = errors.New("idempotency race")

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

const selectOrder = `SELECT o.id, o.account_id, o.state, o.base_amount_paise, o.amount_paise, o.currency,
	COALESCE(o.reference,''), COALESCE(o.note,''), o.expires_at, o.created_at, o.updated_at, COALESCE(o.confirmed_at,''),
	a.vpa, a.display_name FROM orders o JOIN receiving_accounts a ON a.id = o.account_id`

func scanOrder(r interface{ Scan(...any) error }) (Order, error) {
	var o Order
	var vpa, name string
	if err := r.Scan(&o.ID, &o.AccountID, &o.State, &o.BaseAmountPaise, &o.AmountPaise, &o.Currency, &o.Reference, &o.Note,
		&o.ExpiresAt, &o.CreatedAt, &o.UpdatedAt, &o.ConfirmedAt, &vpa, &name); err != nil {
		return Order{}, err
	}
	if o.State == "awaiting" {
		o.UPILink = upi.Link(vpa, name, o.AmountPaise, o.Note)
	}
	return o, nil
}

func (s *Service) Get(ctx context.Context, id string) (Order, error) {
	o, err := scanOrder(s.st.Read().QueryRowContext(ctx, selectOrder+` WHERE o.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	return o, err
}

// Cancel stops an order that has not been paid or expired.
func (s *Service) Cancel(ctx context.Context, id string) (Order, error) {
	err := s.st.Tx(ctx, func(tx *sql.Tx) error {
		var state string
		if err := tx.QueryRowContext(ctx, `SELECT state FROM orders WHERE id = ?`, id).Scan(&state); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		switch state {
		case "created", "queued", "awaiting":
		default:
			return ErrNotCancellable
		}
		var accountID string
		if err := tx.QueryRowContext(ctx, `SELECT account_id FROM orders WHERE id = ?`, id).Scan(&accountID); err != nil {
			return err
		}
		now := s.clk.Now().UTC().Format(time.RFC3339)
		if _, err := tx.ExecContext(ctx, `UPDATE orders SET state='cancelled', updated_at=? WHERE id=?`, now, id); err != nil {
			return err
		}
		if _, err := ledger.Append(ctx, tx, s.clk, ledger.Entry{Kind: "order.cancelled", OrderID: id, Payload: map[string]any{"from": state}}); err != nil {
			return err
		}
		if err := s.pool.Release(ctx, tx, id); err != nil {
			return err
		}
		_, err := s.pool.PromoteQueued(ctx, tx, accountID)
		return err
	})
	if err != nil {
		return Order{}, err
	}
	return s.Get(ctx, id)
}
