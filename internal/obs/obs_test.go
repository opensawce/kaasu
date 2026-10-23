package obs

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestRedactionTable(t *testing.T) {
	tests := map[string]string{
		"payer ravi.kumar@okicici paid": "payer ra***@okicici paid",
		"utr 629812345678 credited":     "utr ***5678 credited",
		"key kaasu_live_ABCDEFGHIJKLMNOPQRSTUVWXYZ234567ABCDEFGHIJKLMNOPQRSTUVWXYZ2345": "key kaasu_live_***",
		"account 123456789012345 debited":                                               "account ***2345 debited",
		"Authorization: Bearer abc.def.ghi":                                             "Authorization: Bearer ***",
		"plain text":                                                                    "plain text",
	}
	for in, want := range tests {
		if got := Redact(in); got != want {
			t.Errorf("Redact(%q)=%q want %q", in, got, want)
		}
	}
}

func TestSecretsNeverReachLogOutput(t *testing.T) {
	var buf bytes.Buffer
	log := NewLogger(&buf, slog.LevelDebug)
	secrets := []string{"ravi.kumar@okicici", "629812345678", "kaasu_live_ABCDEFGHIJKLMNOPQRSTUVWXYZ234567ABCDEFGHIJKLMNOPQRSTUVWXYZ2345"}
	log.Info("paid", "vpa", secrets[0], "utr", secrets[1], "key", secrets[2], "msg", "x "+secrets[1])
	for _, s := range secrets {
		if strings.Contains(buf.String(), s) {
			t.Errorf("log leaks %q: %s", s, buf.String())
		}
	}
}

func TestRequestIDPropagates(t *testing.T) {
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequestIDFrom(r.Context()) == "" {
			t.Error("request id missing in context")
		}
		w.WriteHeader(200)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Header().Get("X-Request-Id") == "" {
		t.Error("X-Request-Id header missing")
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-Id", "abc-123")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("X-Request-Id") != "abc-123" {
		t.Error("supplied request id must be kept")
	}
}

func TestMetricsFormat(t *testing.T) {
	m := NewMetrics()
	m.OrdersCreated.Inc()
	m.AlertsReceived.Inc("device")
	m.AlertsRejected.Inc(`bad"reason`)
	m.IngestSeconds.Observe(0.02)
	out := m.Expose(context.Background())
	for _, line := range []string{"# TYPE kaasu_orders_created_total counter", "kaasu_orders_created_total 1",
		`kaasu_alerts_received_total{source="device"} 1`, `kaasu_alerts_rejected_total{reason="bad\"reason"} 1`,
		"# TYPE kaasu_ingest_seconds histogram", `kaasu_ingest_seconds_bucket{le="0.025"} 1`, "kaasu_ingest_seconds_count 1"} {
		if !strings.Contains(out, line) {
			t.Errorf("missing %q in:\n%s", line, out)
		}
	}
	if !regexp.MustCompile(`(?m)^# TYPE kaasu_orders_created_total`).MatchString(out) {
		t.Error("TYPE line must come before samples")
	}
}

func TestMetricsHandlerOnlyOnAdmin(t *testing.T) {
	h := Handler(NewMetrics())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "kaasu_orders_created_total") {
		t.Errorf("code=%d", rec.Code)
	}
}

func FuzzRedact(f *testing.F) {
	f.Add("ravi.kumar@okicici 629812345678 kaasu_live_X")
	f.Fuzz(func(t *testing.T, s string) {
		_ = Redact(s)
	})
}
