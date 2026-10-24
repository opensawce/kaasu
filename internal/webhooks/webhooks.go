// Package webhooks delivers the outbox to the shop's backend. Every request is signed, and delivery is at least once:
// receivers must dedupe by event_id and act only on final order states.
package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/ledger"
	"github.com/opensawce/kaasu/internal/secrets"
	"github.com/opensawce/kaasu/internal/store"
)

const (
	maxAttempts = 8
	batch       = 20
	timeout     = 10 * time.Second
)

// backoff is the wait after the nth failed attempt (1-based). Attempt maxAttempts is dead-lettered, so there are seven waits.
func backoff(attempt int) time.Duration {
	waits := [...]time.Duration{10 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute,
		time.Hour, 6 * time.Hour, 12 * time.Hour}
	return waits[min(attempt-1, len(waits)-1)]
}

// Sign returns the X-Kaasu-Signature header value: t=<unix>,v1=<hex HMAC-SHA256(secret, t "." body)>.
func Sign(secret string, t time.Time, body []byte) string {
	ts := strconv.FormatInt(t.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	return "t=" + ts + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

// Verify checks a signature header on the receiving side. It rejects bad formats, wrong keys, tampered bodies
// and timestamps outside tolerance.
func Verify(secret, header string, body []byte, now time.Time, tolerance time.Duration) error {
	var tsText, v1 string
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			return errors.New("bad signature format")
		}
		switch k {
		case "t":
			tsText = v
		case "v1":
			v1 = v
		}
	}
	ts, err := strconv.ParseInt(tsText, 10, 64)
	if err != nil || v1 == "" {
		return errors.New("bad signature format")
	}
	skew := now.Sub(time.Unix(ts, 0))
	if skew < -tolerance || skew > tolerance {
		return errors.New("timestamp outside tolerance")
	}
	want := Sign(secret, time.Unix(ts, 0), body)
	_, wantV1, _ := strings.Cut(want, "v1=")
	if !hmac.Equal([]byte(wantV1), []byte(v1)) {
		return errors.New("signature mismatch")
	}
	return nil
}

// Deliverer sends queued events.
type Deliverer struct {
	st           *store.Store
	clk          clock.Clock
	vault        *secrets.Vault
	allowPrivate bool
	client       *http.Client
	log          *slog.Logger
}

// New makes a deliverer. allowPrivate permits loopback and private targets and plain http; use it only in dev.
func New(st *store.Store, clk clock.Clock, vault *secrets.Vault, allowPrivate bool, log *slog.Logger) *Deliverer {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	d := &Deliverer{st: st, clk: clk, vault: vault, allowPrivate: allowPrivate, log: log}
	dialer := &net.Dialer{Timeout: timeout, Control: d.guardConn}
	d.client = &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{DialContext: dialer.DialContext},
		// A redirect could send the signed body somewhere the shop did not configure.
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirects are refused") },
	}
	return d
}

// guardConn checks the address actually dialled, so DNS rebinding cannot reach a private host.
func (d *Deliverer) guardConn(_, address string, _ syscall.RawConn) error {
	if d.allowPrivate {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if isPrivate(net.ParseIP(host)) {
		return fmt.Errorf("webhook target %s is private; blocked", host)
	}
	return nil
}

func isPrivate(ip net.IP) bool {
	return ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

// checkURL refuses non-https and private literal targets before anything is sent.
func (d *Deliverer) checkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return errors.New("webhook url is not valid")
	}
	if d.allowPrivate {
		return nil
	}
	if u.Scheme != "https" {
		return errors.New("webhook url must be https")
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return errors.New("webhook target localhost is blocked")
	}
	if ip := net.ParseIP(host); ip != nil && isPrivate(ip) {
		return fmt.Errorf("webhook target %s is private; blocked", host)
	}
	return nil
}

func (d *Deliverer) setting(ctx context.Context, key string) (string, error) {
	var v string
	err := d.st.Read().QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// secret returns the signing key, creating it on first use.
func (d *Deliverer) secret(ctx context.Context) (string, error) {
	b, err := d.vault.Get(ctx, "webhook_secret")
	if errors.Is(err, secrets.ErrNotFound) {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return "", err
		}
		s := hex.EncodeToString(raw)
		if err := d.vault.Put(ctx, "webhook_secret", []byte(s)); err != nil {
			return "", err
		}
		return s, nil
	}
	return string(b), err
}

type due struct {
	id, eventID, payload string
	attempts             int
}

// Deliver sends every due event once and returns how many were attempted.
func (d *Deliverer) Deliver(ctx context.Context) (int, error) {
	now := d.clk.Now().UTC()
	rows, err := d.st.Read().QueryContext(ctx, `SELECT id, event_id, payload, attempts FROM outbox
		WHERE status = 'pending' AND next_attempt_at <= ? ORDER BY next_attempt_at, id LIMIT ?`, now.Format(time.RFC3339), batch)
	if err != nil {
		return 0, err
	}
	var items []due
	for rows.Next() {
		var x due
		if err := rows.Scan(&x.id, &x.eventID, &x.payload, &x.attempts); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, x)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("webhooks: read outbox: %w", err)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if len(items) == 0 {
		return 0, nil
	}
	target, err := d.setting(ctx, "webhook.url")
	if err != nil {
		return 0, err
	}
	if target == "" {
		return 0, nil // no webhook configured: events wait in the outbox
	}
	if err := d.checkURL(target); err != nil {
		// Nothing is sent to a target that fails the guard. Events stay queued until the URL is fixed.
		d.log.Error("webhook target refused", "err", err)
		return 0, nil
	}
	key, err := d.secret(ctx)
	if err != nil {
		return 0, err
	}
	for _, x := range items {
		if err := d.send(ctx, x, target, key); err != nil {
			return 0, err
		}
	}
	return len(items), nil
}

func (d *Deliverer) send(ctx context.Context, x due, target, key string) error {
	body := []byte(x.payload)
	now := d.clk.Now().UTC()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Kaasu-Event-Id", x.eventID)
	req.Header.Set("X-Kaasu-Signature", Sign(key, now, body))
	code, sendErr := 0, error(nil)
	if resp, err := d.client.Do(req); err != nil {
		sendErr = err
	} else {
		code = resp.StatusCode
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
	}
	ok := sendErr == nil && code/100 == 2
	return d.record(ctx, x, now, code, sendErr, ok)
}

func (d *Deliverer) record(ctx context.Context, x due, now time.Time, code int, sendErr error, ok bool) error {
	msg := ""
	if sendErr != nil {
		msg = sendErr.Error()
	} else if !ok {
		msg = fmt.Sprintf("status %d", code)
	}
	return d.st.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO webhook_deliveries(id, outbox_id, attempt, status_code, error, at)
			VALUES(?,?,?,?,?,?)`, "whd_"+x.id+"_"+strconv.FormatInt(now.UnixNano(), 10), x.id, x.attempts+1, nullInt(code), nullStr(msg), now.Format(time.RFC3339)); err != nil {
			return err
		}
		attempts := x.attempts + 1
		if ok {
			_, err := tx.ExecContext(ctx, `UPDATE outbox SET status='delivered', attempts=?, delivered_at=? WHERE id=?`,
				attempts, now.Format(time.RFC3339), x.id)
			return err
		}
		if attempts >= maxAttempts {
			if _, err := tx.ExecContext(ctx, `UPDATE outbox SET status='dead', attempts=? WHERE id=?`, attempts, x.id); err != nil {
				return err
			}
			_, err := ledger.Append(ctx, tx, d.clk, ledger.Entry{Kind: "webhook.dead", Payload: map[string]any{"outbox_id": x.id, "attempts": attempts}})
			return err
		}
		next := now.Add(backoff(attempts))
		_, err := tx.ExecContext(ctx, `UPDATE outbox SET attempts=?, next_attempt_at=? WHERE id=?`, attempts, next.Format(time.RFC3339), x.id)
		return err
	})
}

// Resend puts a dead or delivered event back in the queue.
func (d *Deliverer) Resend(ctx context.Context, outboxID string) error {
	return d.st.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE outbox SET status='pending', attempts=0, next_attempt_at=? WHERE id=?`,
			d.clk.Now().UTC().Format(time.RFC3339), outboxID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("outbox %s not found", outboxID)
		}
		_, err = ledger.Append(ctx, tx, d.clk, ledger.Entry{Kind: "webhook.resent", Payload: map[string]any{"outbox_id": outboxID}})
		return err
	})
}

func nullInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
