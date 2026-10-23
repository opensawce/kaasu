// Command kaasu is the single Kaasu binary: server, tools and simulator.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/opensawce/kaasu/internal/accounts"
	"github.com/opensawce/kaasu/internal/app"
	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/config"
	"github.com/opensawce/kaasu/internal/ledger"
	"github.com/opensawce/kaasu/internal/secrets"
	"github.com/opensawce/kaasu/internal/store"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = "usage: kaasu <serve|migrate|account|apikey|keygen|rotate-key|verify|backup|pair|version> [flags]"

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

func run(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintf(stdout, "kaasu %s\n", version)
		return 0
	case "serve":
		return serve(args[1:], getenv, stderr)
	case "migrate":
		return migrate(getenv, stdout, stderr)
	case "keygen":
		k := make([]byte, 32)
		if _, err := rand.Read(k); err != nil {
			fmt.Fprintln(stderr, "keygen:", err)
			return 1
		}
		fmt.Fprintln(stdout, base64.StdEncoding.EncodeToString(k))
		return 0
	case "rotate-key":
		return rotateKey(getenv, stdout, stderr)
	case "account":
		return account(args[1:], getenv, stdout, stderr)
	case "apikey":
		return apikey(args[1:], getenv, stdout, stderr)
	case "verify":
		return verify(getenv, stdout, stderr)
	case "backup", "pair":
		fmt.Fprintf(stderr, "kaasu %s: not implemented yet\n", args[0])
		return 1
	default:
		fmt.Fprintf(stderr, "unknown command %q\n%s\n", args[0], usage)
		return 2
	}
}

func migrate(getenv func(string) string, stdout, stderr io.Writer) int {
	cfg, err := config.Load(getenv, getenv("KAASU_CONFIG"))
	if err != nil {
		fmt.Fprintln(stderr, "config:", err)
		return 1
	}
	st, err := store.Open(cfg.DatabasePath)
	if err != nil {
		fmt.Fprintln(stderr, "open database:", err)
		return 1
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		fmt.Fprintln(stderr, "migrate:", err)
		return 1
	}
	fmt.Fprintln(stdout, "schema up to date")
	return 0
}

// rotateKey re-wraps every stored secret under KAASU_NEW_MASTER_KEY. After it succeeds,
// switch KAASU_MASTER_KEY to the new key; nothing changes if any secret fails to rewrap.
func rotateKey(getenv func(string) string, stdout, stderr io.Writer) int {
	cfg, err := config.Load(getenv, getenv("KAASU_CONFIG"))
	if err != nil {
		fmt.Fprintln(stderr, "config:", err)
		return 1
	}
	cur, _, err := secrets.LoadMasterKey(getenv, secrets.MacKeychain)
	if err != nil {
		fmt.Fprintln(stderr, "current key:", err)
		return 1
	}
	next, err := secrets.ParseMasterKey(getenv("KAASU_NEW_MASTER_KEY"))
	if err != nil {
		fmt.Fprintln(stderr, "set KAASU_NEW_MASTER_KEY (make one with `kaasu keygen`):", err)
		return 1
	}
	st, err := store.Open(cfg.DatabasePath)
	if err != nil {
		fmt.Fprintln(stderr, "open database:", err)
		return 1
	}
	defer st.Close()
	n, err := secrets.NewVault(st, cur, clock.Real{}).RotateAll(context.Background(), next)
	if err != nil {
		fmt.Fprintln(stderr, "rotate:", err)
		return 1
	}
	fmt.Fprintf(stdout, "rotated %d secrets; now point KAASU_MASTER_KEY at the new key\n", n)
	return 0
}

// verify walks the ledger hash chain and exits non-zero at the first broken row.
func verify(getenv func(string) string, stdout, stderr io.Writer) int {
	cfg, err := config.Load(getenv, getenv("KAASU_CONFIG"))
	if err != nil {
		fmt.Fprintln(stderr, "config:", err)
		return 1
	}
	st, err := store.Open(cfg.DatabasePath)
	if err != nil {
		fmt.Fprintln(stderr, "open database:", err)
		return 1
	}
	defer st.Close()
	if err := st.Migrate(context.Background()); err != nil {
		fmt.Fprintln(stderr, "migrate:", err)
		return 1
	}
	res, err := ledger.Verify(context.Background(), st.Read())
	if err != nil {
		fmt.Fprintln(stderr, "verify:", err)
		return 1
	}
	fmt.Fprintln(stdout, res.Describe())
	if !res.OK {
		return 1
	}
	return 0
}

// openApp loads config and the master key, then builds the app (no listeners yet).
func openApp(getenv func(string) string, stderr io.Writer) (*app.App, *slog.Logger, bool) {
	cfg, err := config.Load(getenv, getenv("KAASU_CONFIG"))
	if err != nil {
		fmt.Fprintln(stderr, "config:", err)
		return nil, nil, false
	}
	key, source, err := secrets.LoadMasterKey(getenv, secrets.MacKeychain)
	if err != nil {
		fmt.Fprintln(stderr, "master key:", err)
		return nil, nil, false
	}
	var level slog.Level
	_ = level.UnmarshalText([]byte(cfg.LogLevel))
	log := slog.New(slog.NewJSONHandler(stderr, &slog.HandlerOptions{Level: level}))
	log.Info("master key loaded", "source", source)
	a, err := app.New(cfg, key, log)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return nil, nil, false
	}
	return a, log, true
}

func account(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "add" {
		fmt.Fprintln(stderr, "usage: kaasu account add --vpa <name@handle> --bank <bank> --name <display name> [--limit-rupees N]")
		return 2
	}
	fs := flag.NewFlagSet("account add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	vpa, bank, name := fs.String("vpa", "", "UPI ID"), fs.String("bank", "", "bank name"), fs.String("name", "", "display name")
	limit := fs.Int64("limit-rupees", 0, "orders above this need your OK (0 = always ask)")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	a, _, ok := openApp(getenv, io.Discard)
	if !ok {
		fmt.Fprintln(stderr, "could not open Kaasu: check KAASU_MASTER_KEY and the database path")
		return 1
	}
	defer a.Store.Close()
	acc, err := a.Accounts.Create(ledger.WithActor(context.Background(), "cli"), accounts.Input{VPA: *vpa, Bank: *bank, DisplayName: *name, AutoConfirmLimitPaise: *limit * 100})
	if err != nil {
		fmt.Fprintln(stderr, "account add:", err)
		return 1
	}
	fmt.Fprintf(stdout, "added %s (%s)\n", acc.ID, acc.VPA)
	return 0
}

func apikey(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) != 2 || args[0] != "new" {
		fmt.Fprintln(stderr, "usage: kaasu apikey new <label>")
		return 2
	}
	a, _, ok := openApp(getenv, io.Discard)
	if !ok {
		fmt.Fprintln(stderr, "could not open Kaasu: check KAASU_MASTER_KEY and the database path")
		return 1
	}
	defer a.Store.Close()
	plain, rec, err := a.Vault.NewAPIKey(ledger.WithActor(context.Background(), "cli"), args[1])
	if err != nil {
		fmt.Fprintln(stderr, "apikey new:", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s\nkey id %s. This key is shown once; Kaasu stores only a hash.\n", plain, rec.ID)
	return 0
}

func serve(args []string, getenv func(string) string, stderr io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintf(stderr, "serve: unexpected arguments %q (configure with KAASU_CONFIG or KAASU_* env)\n", args)
		return 2
	}
	a, log, ok := openApp(getenv, stderr)
	if !ok {
		return 1
	}
	if err := a.Start(); err != nil {
		log.Error("start", "err", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	code := 0
	select {
	case <-ctx.Done():
	case err := <-a.Server.Err():
		log.Error("listener failed", "err", err)
		code = 1
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := a.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown", "err", err)
		return 1
	}
	return code
}
