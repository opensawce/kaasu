// Package obs is the logging and metrics layer: redacted JSON logs, request ids, and Prometheus text on the admin listener.
package obs

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

var (
	vpaRe  = regexp.MustCompile(`\b([A-Za-z0-9._-]{2,})@([a-z]{3,})\b`)
	utrRe  = regexp.MustCompile(`\b[0-9]{12}\b`)
	keyRe  = regexp.MustCompile(`kaasu_live_[A-Za-z2-7]{20,}`)
	bearer = regexp.MustCompile(`Bearer [A-Za-z0-9._~+/=-]+`)
	acctRe = regexp.MustCompile(`\b[0-9]{9,18}\b`)
)

// Redact removes payer and account identifiers and keys from free text. Values keep only what helps support.
func Redact(s string) string {
	s = keyRe.ReplaceAllString(s, "kaasu_live_***")
	s = bearer.ReplaceAllString(s, "Bearer ***")
	s = vpaRe.ReplaceAllStringFunc(s, func(m string) string {
		name, handle, _ := strings.Cut(m, "@")
		keep := 2
		if len(name) <= 2 {
			keep = 1
		}
		return name[:keep] + "***@" + handle
	})
	s = utrRe.ReplaceAllStringFunc(s, func(m string) string { return "***" + m[len(m)-4:] })
	s = acctRe.ReplaceAllStringFunc(s, func(m string) string { return "***" + m[len(m)-4:] })
	return s
}

type redactHandler struct{ inner slog.Handler }

func (h redactHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h redactHandler) Handle(ctx context.Context, r slog.Record) error {
	nr := slog.NewRecord(r.Time, r.Level, Redact(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		nr.AddAttrs(redactAttr(a))
		return true
	})
	if id := RequestIDFrom(ctx); id != "" {
		nr.AddAttrs(slog.String("request_id", Redact(id)))
	}
	return h.inner.Handle(ctx, nr)
}

func redactAttr(a slog.Attr) slog.Attr {
	if a.Value.Kind() == slog.KindGroup {
		var attrs []any
		for _, g := range a.Value.Group() {
			attrs = append(attrs, redactAttr(g))
		}
		return slog.Group(a.Key, attrs...)
	}
	switch a.Value.Kind() {
	case slog.KindString:
		return slog.String(a.Key, Redact(a.Value.String()))
	case slog.KindAny:
		// Errors and other values are rendered to text first, so their message cannot bypass redaction.
		return slog.String(a.Key, Redact(fmt.Sprint(a.Value.Any())))
	}
	return a
}

func (h redactHandler) WithAttrs(as []slog.Attr) slog.Handler {
	out := make([]slog.Attr, len(as))
	for i, a := range as {
		out[i] = redactAttr(a)
	}
	return redactHandler{h.inner.WithAttrs(out)}
}

func (h redactHandler) WithGroup(name string) slog.Handler {
	return redactHandler{h.inner.WithGroup(name)}
}

// NewLogger writes JSON lines with every string redacted.
func NewLogger(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(redactHandler{slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})})
}

type ridKey struct{}

// RequestIDFrom returns the request id in ctx, if any.
func RequestIDFrom(ctx context.Context) string {
	s, _ := ctx.Value(ridKey{}).(string)
	return s
}

// RequestID sets X-Request-Id (keeping a supplied one) and puts it in the request context.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = fmt.Sprintf("req-%d", nextID.Add(1))
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ridKey{}, id)))
	})
}

var nextID atomic.Int64

// Counter counts with optional labels.
type Counter struct {
	mu    sync.Mutex
	name  string
	vals  map[string]float64
	label string
}

func newCounter(name, label string) *Counter {
	return &Counter{name: name, label: label, vals: map[string]float64{}}
}

func (c *Counter) Inc(lv ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := ""
	if len(lv) > 0 {
		key = lv[0]
	}
	c.vals[key]++
}

// Histogram with fixed buckets.
type Histogram struct {
	mu      sync.Mutex
	buckets []float64
	counts  []int64
	sum     float64
	n       int64
}

func newHistogram(b ...float64) *Histogram {
	return &Histogram{buckets: b, counts: make([]int64, len(b))}
}

func (h *Histogram) Observe(v float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sum += v
	h.n++
	for i, b := range h.buckets {
		if v <= b {
			h.counts[i]++
		}
	}
}

// Metrics is the set Kaasu exposes.
type Metrics struct {
	OrdersCreated   *Counter
	OrdersConfirmed *Counter
	AlertsReceived  *Counter
	AlertsRejected  *Counter
	WebhookAttempts *Counter
	IngestSeconds   *Histogram
}

func NewMetrics() *Metrics {
	return &Metrics{
		OrdersCreated:   newCounter("kaasu_orders_created_total", ""),
		OrdersConfirmed: newCounter("kaasu_orders_confirmed_total", ""),
		AlertsReceived:  newCounter("kaasu_alerts_received_total", "source"),
		AlertsRejected:  newCounter("kaasu_alerts_rejected_total", "reason"),
		WebhookAttempts: newCounter("kaasu_webhook_attempts_total", "result"),
		IngestSeconds:   newHistogram(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1),
	}
}

func escape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s)
}

func (c *Counter) write(b *strings.Builder) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Fprintf(b, "# TYPE %s counter\n", c.name)
	keys := make([]string, 0, len(c.vals))
	for k := range c.vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		fmt.Fprintf(b, "%s 0\n", c.name)
	}
	for _, k := range keys {
		if c.label == "" {
			fmt.Fprintf(b, "%s %g\n", c.name, c.vals[k])
		} else {
			fmt.Fprintf(b, "%s{%s=\"%s\"} %g\n", c.name, c.label, escape(k), c.vals[k])
		}
	}
}

func (h *Histogram) write(b *strings.Builder, name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	fmt.Fprintf(b, "# TYPE %s histogram\n", name)
	for i, bk := range h.buckets {
		fmt.Fprintf(b, "%s_bucket{le=\"%g\"} %d\n", name, bk, h.counts[i])
	}
	fmt.Fprintf(b, "%s_bucket{le=\"+Inf\"} %d\n", name, h.n)
	fmt.Fprintf(b, "%s_sum %g\n%s_count %d\n", name, h.sum, name, h.n)
}

// Expose renders the Prometheus text format.
func (m *Metrics) Expose(ctx context.Context) string {
	var b strings.Builder
	for _, c := range []*Counter{m.OrdersCreated, m.OrdersConfirmed, m.AlertsReceived, m.AlertsRejected, m.WebhookAttempts} {
		c.write(&b)
	}
	m.IngestSeconds.write(&b, "kaasu_ingest_seconds")
	return b.String()
}

// Handler serves /metrics. Mount it on the admin listener only.
func Handler(m *Metrics) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = io.WriteString(w, m.Expose(r.Context()))
	})
}
