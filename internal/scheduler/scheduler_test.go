package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/testutil"
)

var t0 = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

func setup(t *testing.T) (*Runner, *clock.Fake, *sql.DB) {
	t.Helper()
	st := testutil.OpenStore(t)
	clk := clock.NewFake(t0)
	return New(st, clk, &testutil.Seq{}, nil), clk, st.Read()
}

func enqueue(t *testing.T, r *Runner, kind string, at time.Time) {
	t.Helper()
	if err := r.st.Tx(context.Background(), func(tx *sql.Tx) error {
		return r.Enqueue(context.Background(), tx, kind, at, nil)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRunsDueJobOnlyWhenDue(t *testing.T) {
	r, clk, _ := setup(t)
	var ran atomic.Int32
	r.Register("k", func(context.Context, Job) error { ran.Add(1); return nil })
	enqueue(t, r, "k", t0.Add(time.Minute))
	if n, _ := r.Tick(context.Background()); n != 0 || ran.Load() != 0 {
		t.Fatal("job ran before it was due")
	}
	clk.Advance(time.Minute)
	if n, _ := r.Tick(context.Background()); n != 1 || ran.Load() != 1 {
		t.Fatalf("job should run exactly when due: n=%d ran=%d", n, ran.Load())
	}
}

func TestRestartResumesPendingJobs(t *testing.T) {
	r, clk, _ := setup(t)
	enqueue(t, r, "k", t0)
	r2 := New(r.st, clk, &testutil.Seq{}, nil) // a new process on the same database
	var ran atomic.Int32
	r2.Register("k", func(context.Context, Job) error { ran.Add(1); return nil })
	if _, err := r2.Tick(context.Background()); err != nil || ran.Load() != 1 {
		t.Fatalf("restart should resume the pending job: ran=%d err=%v", ran.Load(), err)
	}
}

func TestBackoffScheduleAndDeadAfterEight(t *testing.T) {
	r, clk, db := setup(t)
	r.Register("boom", func(context.Context, Job) error { return errors.New("nope") })
	enqueue(t, r, "boom", t0)
	want := []time.Duration{10 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute, time.Hour, time.Hour, time.Hour}
	for i, gap := range want {
		if _, err := r.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		var runAt string
		if err := db.QueryRow(`SELECT run_at FROM jobs WHERE kind='boom'`).Scan(&runAt); err != nil {
			t.Fatal(err)
		}
		got, _ := time.Parse(time.RFC3339, runAt)
		if got.Sub(clk.Now()) != gap {
			t.Fatalf("attempt %d: next gap %s want %s", i+1, got.Sub(clk.Now()), gap)
		}
		clk.Set(got)
	}
	if _, err := r.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	var status string
	_ = db.QueryRow(`SELECT status FROM jobs WHERE kind='boom'`).Scan(&status)
	if status != "dead" {
		t.Errorf("status=%s want dead after eight attempts", status)
	}
}

func TestPanicIsRecorded(t *testing.T) {
	r, _, db := setup(t)
	r.Register("p", func(context.Context, Job) error { panic("kaboom") })
	enqueue(t, r, "p", t0)
	if _, err := r.Tick(context.Background()); err != nil {
		t.Fatalf("a panic must not stop the runner: %v", err)
	}
	var last string
	_ = db.QueryRow(`SELECT last_error FROM jobs WHERE kind='p'`).Scan(&last)
	if last == "" {
		t.Error("panic should be recorded as the last error")
	}
}

func TestTwoRunnersNeverRunSameJobTwice(t *testing.T) {
	r, clk, _ := setup(t)
	var count atomic.Int32
	r.Register("once", func(context.Context, Job) error { count.Add(1); time.Sleep(20 * time.Millisecond); return nil })
	enqueue(t, r, "once", t0)
	r2 := New(r.st, clk, &testutil.Seq{}, nil)
	r2.Register("once", func(context.Context, Job) error { count.Add(1); time.Sleep(20 * time.Millisecond); return nil })
	var wg sync.WaitGroup
	for _, rr := range []*Runner{r, r2} {
		wg.Add(1)
		go func(rr *Runner) { defer wg.Done(); _, _ = rr.Tick(context.Background()) }(rr)
	}
	wg.Wait()
	if count.Load() != 1 {
		t.Errorf("job ran %d times", count.Load())
	}
}

func TestRecurringEnqueuesWhenDue(t *testing.T) {
	r, clk, db := setup(t)
	var ran atomic.Int32
	r.Register("tick", func(context.Context, Job) error { ran.Add(1); return nil })
	r.Every("tick", time.Minute)
	if _, err := r.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ran.Load() != 1 {
		t.Fatalf("first run should be immediate, ran=%d", ran.Load())
	}
	clk.Advance(30 * time.Second)
	_, _ = r.Tick(context.Background())
	if ran.Load() != 1 {
		t.Error("ran again before the interval")
	}
	clk.Advance(31 * time.Second)
	_, _ = r.Tick(context.Background())
	if ran.Load() != 2 {
		t.Errorf("recurring should run each interval, ran=%d", ran.Load())
	}
	var n int
	_ = db.QueryRow(`SELECT count(*) FROM recurring`).Scan(&n)
	if n != 1 {
		t.Errorf("recurring rows=%d", n)
	}
}

func TestStaleRunningJobIsReclaimed(t *testing.T) {
	r, _, db := setup(t)
	var ran atomic.Int32
	r.Register("k", func(context.Context, Job) error { ran.Add(1); return nil })
	enqueue(t, r, "k", t0)
	if err := r.st.Tx(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE jobs SET status='running', locked_until=? WHERE kind='k'`, t0.Add(-time.Second).Format(time.RFC3339))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	_ = db
	if _, err := r.Tick(context.Background()); err != nil || ran.Load() != 1 {
		t.Errorf("a lock that expired should be reclaimed: ran=%d err=%v", ran.Load(), err)
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	r, _, _ := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop after cancel")
	}
}
