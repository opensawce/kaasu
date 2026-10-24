// Package scheduler runs time-based work from rows in SQLite, so a restart resumes what was pending.
// Handlers must be idempotent: a job can run again after a crash during its lock.
package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/store"
)

const (
	maxAttempts = 8
	lockFor     = 5 * time.Minute
	batch       = 50
)

// backoff is the wait after the nth failure (1-based). The last value repeats.
var backoff = []time.Duration{10 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute, time.Hour}

// IDSource makes job ids.
type IDSource interface{ New(prefix string) string }

// Job is one unit of work handed to a handler.
type Job struct {
	ID       string
	Kind     string
	Payload  json.RawMessage
	Attempts int
}

// Handler runs one job. Returning an error schedules a retry.
type Handler func(ctx context.Context, job Job) error

// Runner owns the handlers and drives the job table.
type Runner struct {
	st       *store.Store
	clk      clock.Clock
	ids      IDSource
	log      *slog.Logger
	handlers map[string]Handler
}

// New makes a runner. A nil logger discards logs.
func New(st *store.Store, clk clock.Clock, ids IDSource, log *slog.Logger) *Runner {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Runner{st: st, clk: clk, ids: ids, log: log, handlers: map[string]Handler{}}
}

// Register sets the handler for a job kind.
func (r *Runner) Register(kind string, h Handler) { r.handlers[kind] = h }

// Every makes kind run now and then every d. The schedule is stored, so it survives restarts.
func (r *Runner) Every(kind string, every time.Duration) error {
	if every < time.Second {
		return fmt.Errorf("scheduler: %s interval %v is shorter than one second", kind, every)
	}
	now := r.clk.Now().UTC().Format(time.RFC3339)
	err := r.st.Tx(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO recurring(kind, every_seconds, next_run_at) VALUES(?,?,?)
			ON CONFLICT(kind) DO UPDATE SET every_seconds = excluded.every_seconds`, kind, int64(every.Seconds()), now)
		return err
	})
	if err != nil {
		return fmt.Errorf("scheduler: register %s: %w", kind, err)
	}
	return nil
}

// Enqueue adds a job inside the caller's transaction, so it commits with the change that caused it.
func (r *Runner) Enqueue(ctx context.Context, tx *sql.Tx, kind string, runAt time.Time, payload any) error {
	b := []byte("{}")
	if payload != nil {
		var err error
		if b, err = json.Marshal(payload); err != nil {
			return fmt.Errorf("job payload: %w", err)
		}
	}
	now := r.clk.Now().UTC().Format(time.RFC3339)
	_, err := tx.ExecContext(ctx, `INSERT INTO jobs(id, kind, payload, run_at, created_at) VALUES(?,?,?,?,?)`,
		r.ids.New("job"), kind, string(b), runAt.UTC().Format(time.RFC3339), now)
	return err
}

// Tick runs every due job once and returns how many ran. Tests drive the runner with this.
func (r *Runner) Tick(ctx context.Context) (int, error) {
	if err := r.enqueueRecurring(ctx); err != nil {
		return 0, err
	}
	jobs, err := r.claim(ctx)
	if err != nil {
		return 0, err
	}
	for _, j := range jobs {
		r.runOne(ctx, j)
	}
	return len(jobs), nil
}

// Run ticks once a second until ctx ends.
func (r *Runner) Run(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := r.Tick(ctx); err != nil {
				r.log.Error("scheduler tick", "err", err)
			}
		}
	}
}

func (r *Runner) enqueueRecurring(ctx context.Context) error {
	now := r.clk.Now().UTC()
	nowText := now.Format(time.RFC3339)
	return r.st.Tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT kind, every_seconds FROM recurring WHERE next_run_at <= ?`, nowText)
		if err != nil {
			return err
		}
		type due struct {
			kind  string
			every int64
		}
		var dues []due
		for rows.Next() {
			var d due
			if err := rows.Scan(&d.kind, &d.every); err != nil {
				rows.Close()
				return err
			}
			dues = append(dues, d)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("scheduler: read recurring: %w", err)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, d := range dues {
			if err := r.Enqueue(ctx, tx, d.kind, now, nil); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE recurring SET next_run_at = ? WHERE kind = ?`,
				now.Add(time.Duration(d.every)*time.Second).Format(time.RFC3339), d.kind); err != nil {
				return err
			}
		}
		return nil
	})
}

// claim marks due jobs as running and locks them, in one short transaction.
func (r *Runner) claim(ctx context.Context) ([]Job, error) {
	now := r.clk.Now().UTC()
	nowText := now.Format(time.RFC3339)
	var out []Job
	err := r.st.Tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id, kind, payload, attempts FROM jobs
			WHERE (status = 'pending' AND run_at <= ?) OR (status = 'running' AND locked_until < ?)
			ORDER BY run_at, id LIMIT ?`, nowText, nowText, batch)
		if err != nil {
			return err
		}
		for rows.Next() {
			var j Job
			var payload string
			if err := rows.Scan(&j.ID, &j.Kind, &payload, &j.Attempts); err != nil {
				rows.Close()
				return err
			}
			j.Payload = json.RawMessage(payload)
			out = append(out, j)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("scheduler: read due jobs: %w", err)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, j := range out {
			if _, err := tx.ExecContext(ctx, `UPDATE jobs SET status='running', locked_until=? WHERE id=?`,
				now.Add(lockFor).Format(time.RFC3339), j.ID); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

// runOne runs a handler outside any transaction, recovers panics, then records the outcome.
func (r *Runner) runOne(ctx context.Context, j Job) {
	runErr := safeCall(ctx, r.handlers[j.Kind], j)
	if r.handlers[j.Kind] == nil {
		runErr = fmt.Errorf("no handler for %s", j.Kind)
	}
	if err := r.record(ctx, j, runErr); err != nil {
		r.log.Error("scheduler: record", "job", j.ID, "err", err)
	}
}

func safeCall(ctx context.Context, h Handler, j Job) (err error) {
	if h == nil {
		return nil
	}
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	return h(ctx, j)
}

func (r *Runner) record(ctx context.Context, j Job, runErr error) error {
	now := r.clk.Now().UTC()
	return r.st.Tx(ctx, func(tx *sql.Tx) error {
		if runErr == nil {
			_, err := tx.ExecContext(ctx, `UPDATE jobs SET status='done', attempts=attempts+1, locked_until=NULL, last_error=NULL WHERE id=?`, j.ID)
			return err
		}
		attempts := j.Attempts + 1
		if attempts >= maxAttempts {
			_, err := tx.ExecContext(ctx, `UPDATE jobs SET status='dead', attempts=?, locked_until=NULL, last_error=? WHERE id=?`,
				attempts, runErr.Error(), j.ID)
			return err
		}
		wait := backoff[min(attempts-1, len(backoff)-1)]
		_, err := tx.ExecContext(ctx, `UPDATE jobs SET status='pending', attempts=?, run_at=?, locked_until=NULL, last_error=? WHERE id=?`,
			attempts, now.Add(wait).Format(time.RFC3339), runErr.Error(), j.ID)
		return err
	})
}
