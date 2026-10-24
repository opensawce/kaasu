// Package sim sends signed bank alerts through the real ingest path, so development runs the same code as production.
// It also holds the outbound guard: dev mode refuses any network call that leaves the machine.
package sim

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/opensawce/kaasu/internal/ingest"
)

// Scenario is one named payment situation. Expect is the order state it should end in.
type Scenario struct {
	Name, Description, Expect string
}

var scenarios = []Scenario{
	{"exact-match", "a credit with the exact amount from a paired phone", "confirmed"},
	{"edited-amount", "the customer changed the amount by one paisa", "review"},
	{"late", "the alert arrives after the order expired", "review"},
	{"duplicate-sources", "the same payment by SMS and by email", "confirmed"},
	{"replay", "the same signed request sent twice", "confirmed"},
	{"forged-sender", "an alert from an unknown sender id", "awaiting"},
	{"unknown-format", "a bank message the parsers do not know", "awaiting"},
	{"never-arrives", "no alert at all; the order expires", "expired"},
	{"pool-full", "101 orders on one amount; the 101st waits in the queue", "queued"},
	{"refund", "a full refund recorded after confirmation", "refunded"},
}

// Names lists scenario names in order.
func Names() []string {
	out := make([]string, len(scenarios))
	for i, s := range scenarios {
		out[i] = s.Name
	}
	return out
}

// All returns every scenario.
func All() []Scenario { return append([]Scenario(nil), scenarios...) }

// Lookup finds a scenario by name.
func Lookup(name string) (Scenario, bool) {
	for _, s := range scenarios {
		if s.Name == name {
			return s, true
		}
	}
	return Scenario{}, false
}

// Send posts one alert signed with the device key. It returns the status code and the response body.
func Send(ctx context.Context, base, deviceID string, key ed25519.PrivateKey, ts time.Time, nonce string, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/ingest/v1/alerts", bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("sim: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range ingest.Sign(key, deviceID, ts, nonce, body) {
		req.Header[k] = v
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("sim: send alert: %w", err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return resp.StatusCode, out, fmt.Errorf("sim: read response: %w", err)
	}
	return resp.StatusCode, out, nil
}

// CheckOutbound refuses any URL that is not on this machine. Dev mode uses it on every outbound call.
func CheckOutbound(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	host := u.Hostname()
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("dev mode blocks outbound call to %s", host)
}
