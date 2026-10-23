package httpx

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
)

func ok(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }

func TestSecureHeadersOnEveryResponse(t *testing.T) {
	h := Secure(http.HandlerFunc(ok), false)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	want := map[string]string{
		"Content-Security-Policy":    "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'",
		"X-Content-Type-Options":     "nosniff",
		"Referrer-Policy":            "no-referrer",
		"Cross-Origin-Opener-Policy": "same-origin",
	}
	for k, v := range want {
		if rec.Header().Get(k) != v {
			t.Errorf("%s=%q want %q", k, rec.Header().Get(k), v)
		}
	}
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Error("HSTS only over TLS")
	}
	rec2 := httptest.NewRecorder()
	Secure(http.HandlerFunc(ok), true).ServeHTTP(rec2, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(rec2.Header().Get("Strict-Transport-Security"), "max-age=31536000") {
		t.Error("HSTS missing over TLS")
	}
}

func TestMaxBodyReturns413(t *testing.T) {
	h := MaxBody(16)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(200)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/", strings.NewReader(strings.Repeat("a", 100))))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status %d want 413", rec.Code)
	}
}

func TestRecoverHidesPanic(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := Recover(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("secret internals") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 500 || strings.Contains(rec.Body.String(), "secret internals") || strings.Contains(rec.Body.String(), "goroutine") {
		t.Errorf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestTimeoutReturns503(t *testing.T) {
	h := Timeout(20 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	rec := httptest.NewRecorder()
	start := time.Now()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if time.Since(start) > time.Second {
		t.Error("timeout did not fire")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d", rec.Code)
	}
}

func TestRateLimitThenRefillWithFakeClock(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	l := NewLimiter(clk, 1, 2) // one token per second, burst of two
	h := l.Middleware(func(*http.Request) string { return "ip" })(http.HandlerFunc(ok))
	codes := []int{}
	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
		codes = append(codes, rec.Code)
		if rec.Code == 429 && rec.Header().Get("Retry-After") == "" {
			t.Error("429 needs Retry-After")
		}
	}
	if codes[0] != 200 || codes[1] != 200 || codes[2] != 429 {
		t.Fatalf("codes=%v", codes)
	}
	clk.Advance(time.Second)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 {
		t.Errorf("after refill: %d", rec.Code)
	}
}

func TestClientIPIgnoresForwardedHeaderByDefault(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.9:5555"
	r.Header.Set("X-Forwarded-For", "10.0.0.1")
	if ip := ClientIP(r, false); ip != "203.0.113.9" {
		t.Errorf("ip=%s", ip)
	}
	if ip := ClientIP(r, true); ip != "10.0.0.1" {
		t.Errorf("trusted proxy ip=%s", ip)
	}
}

// No inline script or third-party script may appear on customer pages. The CSP already forbids it;
// this checks the templates that exist.
func TestCSPMatchesNoInlineScript(t *testing.T) {
	h := Secure(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><script src="/static/pay.js"></script></body></html>`))
	}), false)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/pay/x", nil))
	if strings.Contains(rec.Body.String(), `<script src="http`) {
		t.Error("third-party script on a customer page")
	}
}
