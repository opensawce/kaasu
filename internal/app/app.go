// Package app wires the services together and exposes them on the three listeners.
package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/opensawce/kaasu/internal/accounts"
	"github.com/opensawce/kaasu/internal/api"
	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/config"
	"github.com/opensawce/kaasu/internal/devices"
	"github.com/opensawce/kaasu/internal/evidence"
	"github.com/opensawce/kaasu/internal/id"
	"github.com/opensawce/kaasu/internal/ingest"
	"github.com/opensawce/kaasu/internal/orders"
	"github.com/opensawce/kaasu/internal/secrets"
	"github.com/opensawce/kaasu/internal/server"
	"github.com/opensawce/kaasu/internal/store"
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
	Server   *server.Server
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
	a.Server = server.New(cfg, log)
	// Ingest is served only on the ingest listener; admin and public never see /ingest/.
	a.Server.Mount("ingest", "/ingest/", ingest.Handler(ingest.Deps{
		Store: st, Clock: clk, Devices: a.Devices, Evidence: evidence.New(clk, ids),
	}))
	a.Server.Mount("public", "/v1/", api.New(api.Deps{Log: log, Orders: a.Orders, Vault: a.Vault}))
	return a, nil
}

func (a *App) Start() error { return a.Server.Start() }

func (a *App) Shutdown(ctx context.Context) error {
	return errors.Join(a.Server.Shutdown(ctx), a.Store.Close())
}
