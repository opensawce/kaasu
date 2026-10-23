// Package app wires the services together and exposes them on the three listeners.
package app

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/opensawce/kaasu/internal/accounts"
	"github.com/opensawce/kaasu/internal/api"
	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/config"
	"github.com/opensawce/kaasu/internal/devices"
	"github.com/opensawce/kaasu/internal/evidence"
	"github.com/opensawce/kaasu/internal/id"
	"github.com/opensawce/kaasu/internal/ingest"
	"github.com/opensawce/kaasu/internal/match"
	"github.com/opensawce/kaasu/internal/orders"
	"github.com/opensawce/kaasu/internal/pool"
	"github.com/opensawce/kaasu/internal/scheduler"
	"github.com/opensawce/kaasu/internal/secrets"
	"github.com/opensawce/kaasu/internal/server"
	"github.com/opensawce/kaasu/internal/states"
	"github.com/opensawce/kaasu/internal/store"
	"github.com/opensawce/kaasu/internal/webhooks"
)

type randomIDs struct{}

func (randomIDs) New(prefix string) string { return id.New(prefix) }

// App is a running Kaasu: database, services and listeners.
type App struct {
	Config   config.Config
	Store    *store.Store
	Vault    *secrets.Vault
	Accounts *accounts.Service
	Orders   *orders.Service
	Devices  *devices.Service
	Jobs     *scheduler.Runner
	Server   *server.Server
	cancel   context.CancelFunc
	jobsDone chan struct{}
}

// New opens the database, applies migrations and builds the services. It does not listen yet.
func New(cfg config.Config, key secrets.MasterKey, log *slog.Logger) (*App, error) {
	st, err := store.Open(cfg.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := st.Migrate(context.Background()); err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	var (
		clk clock.Clock = clock.Real{}
		ids             = randomIDs{}
	)
	a := &App{Config: cfg, Store: st}
	a.Vault = secrets.NewVault(st, key, clk)
	a.Accounts = accounts.New(st, clk, ids)
	a.Orders = orders.New(st, clk, ids, a.Accounts)
	a.Devices = devices.New(st, clk, cmp.Or(cfg.PairingHost, cfg.IngestAddr))
	a.Jobs = scheduler.New(st, clk, ids, log)
	a.Jobs.Register("orders.expire", func(ctx context.Context, _ scheduler.Job) error {
		_, err := a.Orders.ExpireDue(ctx)
		return err
	})
	a.Jobs.Register("pool.sweep", func(ctx context.Context, _ scheduler.Job) error { return a.Orders.Sweep(ctx) })
	a.Jobs.Register("nonces.purge", func(ctx context.Context, _ scheduler.Job) error {
		return st.Tx(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `DELETE FROM nonces WHERE seen_at < ?`, clk.Now().Add(-10*time.Minute).UTC().Format(time.RFC3339))
			return err
		})
	})
	hooks := webhooks.New(st, clk, a.Vault, false, log)
	a.Jobs.Register("webhooks.deliver", func(ctx context.Context, _ scheduler.Job) error {
		_, err := hooks.Deliver(ctx)
		return err
	})
	a.Jobs.Register("jobs.purge", func(ctx context.Context, _ scheduler.Job) error {
		return st.Tx(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `DELETE FROM jobs WHERE status IN ('done','dead') AND created_at < ?`, clk.Now().Add(-7*24*time.Hour).UTC().Format(time.RFC3339))
			return err
		})
	})
	for kind, every := range map[string]time.Duration{
		"webhooks.deliver": 5 * time.Second, "orders.expire": 5 * time.Second, "pool.sweep": time.Minute, "nonces.purge": 5 * time.Minute, "jobs.purge": time.Hour,
	} {
		if err := a.Jobs.Every(kind, every); err != nil {
			_ = st.Close()
			return nil, err
		}
	}
	a.Server = server.New(cfg, log)
	// Ingest is served only on the ingest listener; admin and public never see /ingest/.
	a.Server.Mount("ingest", "/ingest/", ingest.Handler(ingest.Deps{
		Store: st, Clock: clk, Devices: a.Devices, Evidence: evidence.New(clk, ids),
		Matcher: match.New(st, clk, states.New(clk, pool.New(st, clk))),
	}))
	a.Server.Mount("public", "/v1/", api.New(api.Deps{Log: log, Orders: a.Orders, Vault: a.Vault}))
	return a, nil
}

// Start listens on all three addresses and starts the background jobs.
func (a *App) Start() error {
	if err := a.Server.Start(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.jobsDone = make(chan struct{})
	go func() {
		defer close(a.jobsDone)
		a.Jobs.Run(ctx)
	}()
	return nil
}

func (a *App) Shutdown(ctx context.Context) error {
	if a.cancel != nil {
		a.cancel()
		<-a.jobsDone // the scheduler must stop before the store closes, whatever ctx says
	}
	return errors.Join(a.Server.Shutdown(ctx), a.Store.Close())
}
