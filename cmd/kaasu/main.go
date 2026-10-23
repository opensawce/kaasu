// Command kaasu is the single Kaasu binary: server, tools and simulator.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/opensawce/kaasu/internal/config"
	"github.com/opensawce/kaasu/internal/server"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = "usage: kaasu <serve|migrate|verify|backup|pair|version> [flags]"

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
	case "migrate", "verify", "backup", "pair":
		fmt.Fprintf(stderr, "kaasu %s: not implemented yet\n", args[0])
		return 1
	default:
		fmt.Fprintf(stderr, "unknown command %q\n%s\n", args[0], usage)
		return 2
	}
}

func serve(_ []string, getenv func(string) string, stderr io.Writer) int {
	cfg, err := config.Load(getenv, getenv("KAASU_CONFIG"))
	if err != nil {
		fmt.Fprintln(stderr, "config:", err)
		return 1
	}
	var level slog.Level
	_ = level.UnmarshalText([]byte(cfg.LogLevel))
	log := slog.New(slog.NewJSONHandler(stderr, &slog.HandlerOptions{Level: level}))

	s := server.New(cfg, log)
	if err := s.Start(); err != nil {
		log.Error("start", "err", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown", "err", err)
		return 1
	}
	return 0
}
