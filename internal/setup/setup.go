// Package setup is the first-run wizard: guided steps the owner may skip, and a ₹1 test payment that proves the setup works.
package setup

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/orders"
	"github.com/opensawce/kaasu/internal/store"
)

// Steps in order. Each may be done or skipped; skipped steps are reminded on the Today page.
var Steps = []string{"passkey", "upi_id", "pair_phone", "bank", "test_payment", "limits"}

// TestReference marks the ₹1 test order so totals and reconciliation leave it out.
const TestReference = "setup-test"

// HelpAfter is how long the test payment waits before showing per-bank and per-phone help.
const HelpAfter = 3 * time.Minute

var ErrUnknownStep = errors.New("unknown setup step")

type Wizard struct {
	st     *store.Store
	clk    clock.Clock
	orders *orders.Service
}

func New(st *store.Store, clk clock.Clock, o *orders.Service) *Wizard {
	return &Wizard{st: st, clk: clk, orders: o}
}

// Mark records a step as done or skipped.
func (w *Wizard) Mark(ctx context.Context, step, status string) error {
	if !known(step) {
		return ErrUnknownStep
	}
	if status != "done" && status != "skipped" {
		return errors.New("status must be done or skipped")
	}
	return w.st.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			"setup.step."+step, status)
		return err
	})
}

// Progress returns each step's status, "" when not yet touched.
func (w *Wizard) Progress(ctx context.Context) (map[string]string, error) {
	out := map[string]string{}
	for _, s := range Steps {
		var v string
		err := w.st.Read().QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, "setup.step."+s).Scan(&v)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		out[s] = v
	}
	return out, nil
}

// Reminders are the steps that are not done yet.
func (w *Wizard) Reminders(ctx context.Context) ([]string, error) {
	p, err := w.Progress(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, s := range Steps {
		if p[s] != "done" {
			out = append(out, s)
		}
	}
	return out, nil
}

// StartTestPayment creates the ₹1 order on the account. It is flagged so totals exclude it.
func (w *Wizard) StartTestPayment(ctx context.Context, accountID, key string) (orders.Order, error) {
	o, _, err := w.orders.Create(ctx, orders.CreateInput{AmountPaise: 100, AccountID: accountID, Reference: TestReference, Note: "Kaasu setup test"}, key)
	return o, err
}

// NeedsHelp reports whether a test payment has waited long enough to show help.
func (w *Wizard) NeedsHelp(createdAt string) bool {
	t, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return false
	}
	return !w.clk.Now().Before(t.Add(HelpAfter))
}

func known(s string) bool {
	for _, x := range Steps {
		if x == s {
			return true
		}
	}
	return false
}
