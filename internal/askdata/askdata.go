// Package askdata holds the only questions the assistant may answer. Each is a fixed, parameterised, read-only query.
// The model chooses a name and fills typed parameters; it never writes SQL.
package askdata

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/opensawce/kaasu/internal/store"
)

var (
	ErrUnknownQuery = errors.New("that question is not one I can answer")
	ErrParams       = errors.New("invalid parameters")
)

// Result is what a query returns: named columns and rows, all already plain numbers and text.
type Result struct {
	Query   string
	Columns []string
	Rows    [][]string
}

type Params struct {
	From, To string // RFC 3339 dates, UTC
	State    string
	Limit    int
}

var states = map[string]bool{"created": true, "queued": true, "awaiting": true, "matched": true, "confirmed": true,
	"review": true, "rejected": true, "cancelled": true, "expired": true, "late_paid": true, "refunded": true, "disputed": true}

// Names lists every query the assistant can pick.
func Names() []string {
	return []string{"total_received", "count_orders", "refunds_total", "review_backlog"}
}

func window(p Params) (time.Time, time.Time, error) {
	from, err1 := time.Parse(time.RFC3339, p.From)
	to, err2 := time.Parse(time.RFC3339, p.To)
	// Stored timestamps are whole-second UTC text, so bounds must be too: a fraction would be lost by formatting.
	if err1 != nil || err2 != nil || from.Nanosecond() != 0 || to.Nanosecond() != 0 || !to.After(from) || to.Sub(from) > 366*24*time.Hour {
		return from, to, ErrParams
	}
	return from.UTC(), to.UTC(), nil
}

// Run executes one named query on the read pool. It returns ErrUnknownQuery for any other name.
func Run(ctx context.Context, st *store.Store, name string, p Params) (Result, error) {
	db := st.Read()
	fromT, toT, err := window(p)
	res := Result{Query: name}
	switch name {
	case "total_received":
		if err != nil {
			return res, err
		}
		var total int64
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_paise),0), count(*) FROM orders
			WHERE state IN ('confirmed','late_paid') AND confirmed_at >= ? AND confirmed_at < ? AND id NOT IN (SELECT substr(key, 18) FROM settings WHERE key LIKE 'setup.test\_order.%' ESCAPE '\')`,
			fromT.Format(time.RFC3339), toT.Format(time.RFC3339)).Scan(&total, &n); err != nil {
			return res, err
		}
		res.Columns = []string{"count", "total_paise"}
		res.Rows = [][]string{{fmt.Sprint(n), fmt.Sprint(total)}}
	case "count_orders":
		if err != nil {
			return res, err
		}
		if !states[p.State] {
			return res, ErrParams
		}
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM orders WHERE state = ? AND created_at >= ? AND created_at < ?`,
			p.State, fromT.Format(time.RFC3339), toT.Format(time.RFC3339)).Scan(&n); err != nil {
			return res, err
		}
		res.Columns = []string{"state", "count"}
		res.Rows = [][]string{{p.State, fmt.Sprint(n)}}
	case "refunds_total":
		if err != nil {
			return res, err
		}
		var total int64
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_paise),0), count(*) FROM refunds WHERE created_at >= ? AND created_at < ?`,
			fromT.Format(time.RFC3339), toT.Format(time.RFC3339)).Scan(&total, &n); err != nil {
			return res, err
		}
		res.Columns = []string{"count", "total_paise"}
		res.Rows = [][]string{{fmt.Sprint(n), fmt.Sprint(total)}}
	case "review_backlog":
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM orders WHERE state = 'review'`).Scan(&n); err != nil {
			return res, err
		}
		res.Columns = []string{"waiting"}
		res.Rows = [][]string{{fmt.Sprint(n)}}
	default:
		return res, ErrUnknownQuery
	}
	return res, nil
}
