// Package match decides, from an alert, whether an order is confirmed automatically or sent to the owner.
// It is the only code that confirms a payment. Everything it does runs in one transaction.
package match

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/ledger"
	"github.com/opensawce/kaasu/internal/states"
	"github.com/opensawce/kaasu/internal/store"
)

const (
	autoThreshold  = 80
	graceWindow    = 10 * time.Minute
	earlyTolerance = 2 * time.Minute
)

// Signals are the facts the score is built from.
type Signals struct {
	ExactAmount, AuthenticSource, UTRPresentUnique, NoteEcho, TimingOK, PayerKnown, MultiSource bool
}

// Decision is the policy result for one candidate order.
type Decision struct {
	Outcome   string // auto | review
	Score     int
	Breakdown map[string]int
	Reason    string
}

var weights = []struct {
	name string
	w    int
	on   func(Signals) bool
}{
	{"exact", 40, func(s Signals) bool { return s.ExactAmount }},
	{"authentic", 20, func(s Signals) bool { return s.AuthenticSource }},
	{"utr", 10, func(s Signals) bool { return s.UTRPresentUnique }},
	{"note", 10, func(s Signals) bool { return s.NoteEcho }},
	{"timing", 10, func(s Signals) bool { return s.TimingOK }},
	{"payer", 5, func(s Signals) bool { return s.PayerKnown }},
	{"multi", 5, func(s Signals) bool { return s.MultiSource }},
}

// Score adds the weights of every signal that holds. The weights sum to 100.
func Score(s Signals) (int, map[string]int) {
	total := 0
	bd := map[string]int{}
	for _, x := range weights {
		if x.on(s) {
			total += x.w
			bd[x.name] = x.w
		}
	}
	return total, bd
}

// Decide applies the policy. Automatic confirmation needs an exact amount from an authentic source,
// a score of at least 80, and an account limit that the order is under.
func Decide(score int, s Signals, orderAmount, accountLimit int64) Decision {
	d := Decision{Score: score}
	switch {
	case !s.ExactAmount:
		d.Outcome, d.Reason = "review", "amount_mismatch"
	case !s.AuthenticSource:
		d.Outcome, d.Reason = "review", "unauthentic"
	case score < autoThreshold:
		d.Outcome, d.Reason = "review", "low_score"
	case accountLimit <= 0 || orderAmount > accountLimit:
		d.Outcome, d.Reason = "review", "over_limit"
	default:
		d.Outcome = "auto"
	}
	return d
}

// Matcher processes alerts against orders.
type Matcher struct {
	st  *store.Store
	clk clock.Clock
	sm  *states.Machine
}

func New(st *store.Store, clk clock.Clock, sm *states.Machine) *Matcher {
	return &Matcher{st: st, clk: clk, sm: sm}
}

type alertRow struct {
	amount      int64
	utr, payer  string
	occurred    string
	accountHint string
}

type candidate struct {
	id, accountID, state, reference, created, expires string
	amount, limit                                     int64
}

// Process decides what one alert means. Calling it again for the same alert changes nothing.
func (m *Matcher) Process(ctx context.Context, alertID string) error {
	ctx = ledger.WithActor(ctx, "matcher")
	return m.st.Tx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM matches WHERE alert_id = ?`, alertID).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
		a, err := loadAlert(ctx, tx, alertID)
		if err != nil {
			return err
		}
		exact, err := candidates(ctx, tx, a, a.amount, true)
		if err != nil {
			return err
		}
		switch len(exact) {
		case 0:
			return m.mismatch(ctx, tx, alertID, a)
		case 1:
			return m.one(ctx, tx, alertID, a, exact[0])
		default:
			return m.ambiguous(ctx, tx, alertID, exact)
		}
	})
}

func loadAlert(ctx context.Context, tx *sql.Tx, id string) (alertRow, error) {
	var a alertRow
	err := tx.QueryRowContext(ctx, `SELECT amount_paise, COALESCE(utr,''), COALESCE(payer_masked,''), occurred_at, COALESCE(account_hint,'')
		FROM alerts WHERE id = ?`, id).Scan(&a.amount, &a.utr, &a.payer, &a.occurred, &a.accountHint)
	if err != nil {
		return a, fmt.Errorf("load alert %s: %w", id, err)
	}
	return a, nil
}

// candidates finds open orders that fit the amount. exact=false means the mismatch search: base within 99 paise below.
func candidates(ctx context.Context, tx *sql.Tx, a alertRow, amount int64, exact bool) ([]candidate, error) {
	var q string
	var args []any
	hintArgs := []any{a.accountHint, a.accountHint}
	if exact {
		q = `SELECT o.id, o.account_id, o.state, COALESCE(o.reference,''), o.created_at, o.expires_at, o.amount_paise, a.auto_confirm_limit_paise
			FROM orders o JOIN receiving_accounts a ON a.id = o.account_id
			WHERE o.amount_paise = ? AND o.state IN ('awaiting','expired','cancelled','rejected')`
		args = append([]any{amount}, hintArgs...)
	} else {
		q = `SELECT o.id, o.account_id, o.state, COALESCE(o.reference,''), o.created_at, o.expires_at, o.base_amount_paise, a.auto_confirm_limit_paise
			FROM orders o JOIN receiving_accounts a ON a.id = o.account_id
			WHERE o.base_amount_paise BETWEEN ? AND ? AND o.state = 'awaiting'`
		args = append([]any{amount - 99, amount - 1}, hintArgs...)
	}
	q += ` AND (? = '' OR NOT EXISTS (SELECT 1 FROM settings s WHERE s.key = 'account.' || o.account_id || '.last4')
		OR EXISTS (SELECT 1 FROM settings s WHERE s.key = 'account.' || o.account_id || '.last4' AND s.value = ?))
		ORDER BY o.created_at, o.id`
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.accountID, &c.state, &c.reference, &c.created, &c.expires, &c.amount, &c.limit); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (m *Matcher) signals(ctx context.Context, tx *sql.Tx, alertID string, a alertRow, c candidate) (Signals, error) {
	var s Signals
	s.ExactAmount = c.amount == a.amount
	s.UTRPresentUnique = a.utr != ""
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM alert_sources s JOIN evidence e ON e.id = s.evidence_id
		WHERE s.alert_id = ? AND e.source IN ('device','email'))`, alertID).Scan(&s.AuthenticSource); err != nil {
		return s, err
	}
	var sources int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM alert_sources WHERE alert_id = ?`, alertID).Scan(&sources); err != nil {
		return s, err
	}
	s.MultiSource = sources >= 2
	if a.payer != "" {
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM alerts x JOIN matches mm ON mm.alert_id = x.id
			WHERE x.payer_masked = ? AND x.id <> ? AND mm.decision = 'auto')`, a.payer, alertID).Scan(&s.PayerKnown); err != nil {
			return s, err
		}
	}
	if c.reference != "" {
		var body []byte
		err := tx.QueryRowContext(ctx, `SELECT e.body FROM alert_sources s JOIN evidence e ON e.id = s.evidence_id WHERE s.alert_id = ? LIMIT 1`, alertID).Scan(&body)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return s, err
		}
		s.NoteEcho = bytes.Contains(body, []byte(c.reference))
	}
	occurred, err := time.Parse(time.RFC3339, a.occurred)
	if err != nil {
		return s, err
	}
	created, _ := time.Parse(time.RFC3339, c.created)
	expires, _ := time.Parse(time.RFC3339, c.expires)
	s.TimingOK = !occurred.Before(created.Add(-earlyTolerance)) && !occurred.After(expires.Add(graceWindow))
	return s, nil
}

func (m *Matcher) one(ctx context.Context, tx *sql.Tx, alertID string, a alertRow, c candidate) error {
	// An order past its expiry is late even if the expiry job has not run yet.
	expires, err := time.Parse(time.RFC3339, c.expires)
	if err != nil {
		return err
	}
	pastExpiry := !m.clk.Now().Before(expires)
	switch {
	case c.state == "expired" || (c.state == "awaiting" && pastExpiry):
		return m.review(ctx, tx, alertID, c, "late", 0, nil, true)
	case c.state == "cancelled" || c.state == "rejected":
		return m.review(ctx, tx, alertID, c, "order_closed", 0, nil, false)
	}
	sig, err := m.signals(ctx, tx, alertID, a, c)
	if err != nil {
		return err
	}
	score, bd := Score(sig)
	d := Decide(score, sig, c.amount, c.limit)
	if d.Outcome == "review" {
		return m.review(ctx, tx, alertID, c, d.Reason, score, bd, true)
	}
	if _, err := m.sm.Transition(ctx, tx, c.id, "matched", map[string]any{"alert_id": alertID, "score": score}); err != nil {
		return err
	}
	if _, err := m.sm.Transition(ctx, tx, c.id, "confirmed", map[string]any{"alert_id": alertID, "score": score}); err != nil {
		return err
	}
	return m.insertMatch(ctx, tx, alertID, c.id, "auto", score, bd, "")
}

// review records the decision and moves the order to review when it is still awaiting or expired.
func (m *Matcher) review(ctx context.Context, tx *sql.Tx, alertID string, c candidate, reason string, score int, bd map[string]int, transition bool) error {
	if transition {
		if _, err := m.sm.Transition(ctx, tx, c.id, "review", map[string]any{"alert_id": alertID, "reason": reason, "score": score}); err != nil {
			return err
		}
	}
	return m.insertMatch(ctx, tx, alertID, c.id, "review", score, bd, reason)
}

func (m *Matcher) mismatch(ctx context.Context, tx *sql.Tx, alertID string, a alertRow) error {
	near, err := candidates(ctx, tx, a, a.amount, false)
	if err != nil {
		return err
	}
	if len(near) != 1 {
		return nil // nothing to attach to: the alert stays unmatched
	}
	return m.review(ctx, tx, alertID, near[0], "amount_mismatch", 0, nil, true)
}

func (m *Matcher) ambiguous(ctx context.Context, tx *sql.Tx, alertID string, cs []candidate) error {
	for _, c := range cs {
		if c.state != "awaiting" {
			continue
		}
		if _, err := m.sm.Transition(ctx, tx, c.id, "review", map[string]any{"alert_id": alertID, "reason": "ambiguous"}); err != nil {
			return err
		}
	}
	return m.insertMatch(ctx, tx, alertID, cs[0].id, "review", 0, nil, "ambiguous")
}

func (m *Matcher) insertMatch(ctx context.Context, tx *sql.Tx, alertID, orderID, decision string, score int, bd map[string]int, reason string) error {
	if bd == nil {
		bd = map[string]int{}
	}
	b, err := json.Marshal(bd)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO matches(id, alert_id, order_id, score, decision, decided_by, created_at, breakdown, reason)
		VALUES(?,?,?,?,?,?,?,?,?)`, "mat_"+alertID, alertID, orderID, score, decision, "matcher", m.clk.Now().UTC().Format(time.RFC3339), string(b), reason)
	if err != nil {
		return fmt.Errorf("insert match: %w", err)
	}
	return nil
}
