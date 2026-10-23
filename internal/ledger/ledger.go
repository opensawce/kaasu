// Package ledger is the append-only, hash-chained record of every money-relevant event.
//
// Each row's hash covers the previous row's hash, so changing or removing any row breaks every row after it.
// Append must be called inside the same transaction as the change it records.
package ledger

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
)

const genesis = "0000000000000000000000000000000000000000000000000000000000000000"

type actorKey struct{}

// WithActor records who is acting ("admin:priya", "device:dev_1", "api:key_3"). The default is "system".
func WithActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

func actorFrom(ctx context.Context) string {
	if a, ok := ctx.Value(actorKey{}).(string); ok && a != "" {
		return a
	}
	return "system"
}

// Entry is one event. Payload must not contain secrets or full payer identifiers.
type Entry struct {
	Kind    string
	OrderID string
	Actor   string // optional; defaults to the context actor
	Payload map[string]any
}

func rowHash(prev string, seq int64, ts, kind, orderID, actor, payload string) string {
	h := sha256.New()
	for _, part := range []string{prev, strconv.FormatInt(seq, 10), ts, kind, orderID, actor, payload} {
		h.Write([]byte(part))
		h.Write([]byte{0x1f}) // unit separator keeps field boundaries unambiguous
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Append adds a row inside tx and returns its sequence number.
func Append(ctx context.Context, tx *sql.Tx, clk clock.Clock, e Entry) (int64, error) {
	if e.Kind == "" {
		return 0, errors.New("ledger: entry needs a kind")
	}
	actor := e.Actor
	if actor == "" {
		actor = actorFrom(ctx)
	}
	payload := "{}"
	if len(e.Payload) > 0 {
		b, err := json.Marshal(e.Payload) // map keys are sorted, so the text is stable
		if err != nil {
			return 0, fmt.Errorf("ledger: payload: %w", err)
		}
		payload = string(b)
	}
	var prevSeq int64
	prev := genesis
	err := tx.QueryRowContext(ctx, `SELECT seq, hash FROM ledger ORDER BY seq DESC LIMIT 1`).Scan(&prevSeq, &prev)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("ledger: read tail: %w", err)
	}
	seq := prevSeq + 1
	ts := clk.Now().UTC().Format(time.RFC3339)
	hash := rowHash(prev, seq, ts, e.Kind, e.OrderID, actor, payload)
	var oid any
	if e.OrderID != "" {
		oid = e.OrderID
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO ledger(seq,ts,kind,order_id,actor,payload,prev_hash,hash) VALUES(?,?,?,?,?,?,?,?)`,
		seq, ts, e.Kind, oid, actor, payload, prev, hash); err != nil {
		return 0, fmt.Errorf("ledger: insert: %w", err)
	}
	return seq, nil
}

// Result is the outcome of walking the chain.
type Result struct {
	Rows        int
	OK          bool
	FirstBadSeq int64
	Reason      string
}

// Verify walks the whole chain and reports the first broken row.
func Verify(ctx context.Context, db *sql.DB) (Result, error) {
	rows, err := db.QueryContext(ctx, `SELECT seq,ts,kind,COALESCE(order_id,''),actor,payload,prev_hash,hash FROM ledger ORDER BY seq`)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	res := Result{OK: true}
	prevHash, expectSeq := genesis, int64(1)
	for rows.Next() {
		var seq int64
		var ts, kind, oid, actor, payload, prev, hash string
		if err := rows.Scan(&seq, &ts, &kind, &oid, &actor, &payload, &prev, &hash); err != nil {
			return Result{}, err
		}
		res.Rows++
		bad := func(reason string) (Result, error) {
			return Result{Rows: res.Rows, FirstBadSeq: seq, Reason: reason}, nil
		}
		switch {
		case seq != expectSeq:
			return bad(fmt.Sprintf("expected row %d but found %d: a row is missing", expectSeq, seq))
		case prev != prevHash:
			return bad("previous hash does not match the row before it")
		case rowHash(prev, seq, ts, kind, oid, actor, payload) != hash:
			return bad("row contents do not match its hash")
		}
		prevHash, expectSeq = hash, seq+1
	}
	return res, rows.Err()
}

// Describe renders a Result for people.
func (r Result) Describe() string {
	if r.OK {
		return fmt.Sprintf("ledger intact: %d rows, chain valid", r.Rows)
	}
	return strings.TrimSpace(fmt.Sprintf("ledger BROKEN at row %d: %s", r.FirstBadSeq, r.Reason))
}
