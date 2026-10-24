// Package server runs the three Kaasu listeners: public, ingest and admin.
// Admin routes are mounted only on the admin mux, so they cannot leak onto the other two.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/opensawce/kaasu/internal/config"
)

// Addrs are the bound listener addresses (useful when configured with port 0).
type Addrs struct{ Public, Ingest, Admin string }

type mount struct {
	listener, pattern string
	h                 http.Handler
}

type listener struct {
	name string
	srv  *http.Server
	ln   net.Listener
}

// Server owns the three HTTP servers.
type Server struct {
	cfg    config.Config
	log    *slog.Logger
	ls     []*listener
	mounts []mount
	// errc reports a listener that stopped for any reason other than Shutdown.
	errc chan error
}

// Mount serves h at pattern on one listener ("public", "ingest" or "admin"). Call before Start.
func (s *Server) Mount(listenerName, pattern string, h http.Handler) {
	s.mounts = append(s.mounts, mount{listenerName, pattern, h})
}

func (s *Server) mux(name string, m *http.ServeMux) *http.ServeMux {
	for _, mt := range s.mounts {
		if mt.listener == name {
			m.Handle(mt.pattern, mt.h)
		}
	}
	return m
}

func New(cfg config.Config, log *slog.Logger) *Server {
	return &Server{cfg: cfg, log: log, errc: make(chan error, 3)}
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func baseMux() *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", healthz)
	return m
}

func adminMux() *http.ServeMux {
	m := baseMux()
	m.HandleFunc("GET /admin/ping", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("pong\n")) })
	return m
}

// Start binds all three addresses first, so a failure leaves nothing half-running.
func (s *Server) Start() error {
	specs := []struct {
		name, addr string
		h          http.Handler
	}{
		{"public", s.cfg.PublicAddr, s.mux("public", baseMux())},
		{"ingest", s.cfg.IngestAddr, s.mux("ingest", baseMux())},
		{"admin", s.cfg.AdminAddr, s.mux("admin", adminMux())},
	}
	for _, sp := range specs {
		ln, err := net.Listen("tcp", sp.addr)
		if err != nil {
			s.closeListeners()
			return fmt.Errorf("listen %s on %s: %w", sp.name, sp.addr, err)
		}
		srv := &http.Server{
			Handler:           sp.h,
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       60 * time.Second,
			MaxHeaderBytes:    16 << 10,
		}
		s.ls = append(s.ls, &listener{name: sp.name, srv: srv, ln: ln})
	}
	for _, l := range s.ls {
		s.log.Info("listening", "listener", l.name, "addr", l.ln.Addr().String())
		go func(l *listener) {
			if err := l.srv.Serve(l.ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				s.log.Error("serve failed", "listener", l.name, "err", err)
				s.errc <- fmt.Errorf("serve %s: %w", l.name, err)
			}
		}(l)
	}
	return nil
}

func (s *Server) closeListeners() {
	for _, l := range s.ls {
		_ = l.ln.Close()
	}
	s.ls = nil
}

// Addrs returns the bound addresses; call after Start.
func (s *Server) Addrs() Addrs {
	var a Addrs
	for _, l := range s.ls {
		switch l.name {
		case "public":
			a.Public = l.ln.Addr().String()
		case "ingest":
			a.Ingest = l.ln.Addr().String()
		case "admin":
			a.Admin = l.ln.Addr().String()
		}
	}
	return a
}

// Err delivers the first unexpected listener failure; the caller should then Shutdown and exit nonzero.
func (s *Server) Err() <-chan error { return s.errc }

// Shutdown stops accepting on all listeners at once, then waits for in-flight requests until ctx ends.
func (s *Server) Shutdown(ctx context.Context) error {
	errs := make([]error, len(s.ls))
	var wg sync.WaitGroup
	for i, l := range s.ls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := l.srv.Shutdown(ctx); err != nil {
				errs[i] = fmt.Errorf("shutdown %s: %w", l.name, err)
			}
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}
