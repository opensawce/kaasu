// Package httpx holds the HTTP protections every listener wears: headers, body and time limits,
// rate limits, panic recovery and client identity.
package httpx

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
)

// Secure sets the response headers. HSTS is only sent when the request came over TLS.
func Secure(next http.Handler, tls bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		if tls {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

// MaxBody caps request bodies. Larger bodies fail to read.
func MaxBody(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}

// Timeout bounds how long a handler may take.
func Timeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.TimeoutHandler(next, d, `{"error":{"code":"timeout","message":"the request took too long"}}`)
	}
}

// Recover turns a panic into a plain 500. The panic text and stack never reach the client.
func Recover(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if p := recover(); p != nil {
					log.ErrorContext(r.Context(), "handler panic", "path", r.URL.Path, "panic", fmt.Sprint(p))
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "internal", "message": "something went wrong"}})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// Limiter is a token bucket per key.
type Limiter struct {
	clk   clock.Clock
	rate  float64 // tokens per second
	burst float64
	mu    sync.Mutex
	b     map[string]bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

func NewLimiter(clk clock.Clock, perSecond float64, burst int) *Limiter {
	return &Limiter{clk: clk, rate: perSecond, burst: float64(burst), b: map[string]bucket{}}
}

// Allow takes one token for key. When none is left it returns how long to wait.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clk.Now()
	bk, seen := l.b[key]
	if !seen {
		bk = bucket{tokens: l.burst, last: now}
	}
	bk.tokens = math.Min(l.burst, bk.tokens+now.Sub(bk.last).Seconds()*l.rate)
	bk.last = now
	if bk.tokens >= 1 {
		bk.tokens--
		l.b[key] = bk
		return true, 0
	}
	l.b[key] = bk
	return false, time.Duration((1 - bk.tokens) / l.rate * float64(time.Second))
}

// Middleware refuses with 429 and Retry-After when a key is out of tokens.
func (l *Limiter) Middleware(key func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ok, wait := l.Allow(key(r))
			if !ok {
				w.Header().Set("Retry-After", fmt.Sprint(int(math.Ceil(wait.Seconds()))))
				http.Error(w, `{"error":{"code":"rate_limited","message":"too many requests"}}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP is the caller's address. Forwarded headers are trusted only behind a proxy the owner configured.
func ClientIP(r *http.Request, trustedProxy bool) string {
	if trustedProxy {
		if f := r.Header.Get("X-Forwarded-For"); f != "" {
			return strings.TrimSpace(strings.Split(f, ",")[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
