// Package ingest is the only way bank alerts enter Kaasu from phones and mail sources.
// Requests are signed with the device's Ed25519 key over the body hash, a timestamp and a nonce.
package ingest

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/devices"
	"github.com/opensawce/kaasu/internal/evidence"
	"github.com/opensawce/kaasu/internal/ledger"
	"github.com/opensawce/kaasu/internal/parsers"
	"github.com/opensawce/kaasu/internal/store"
)

const (
	signingVersion = "KAASU-INGEST-V1"
	maxBody        = 64 << 10
	window         = 60 * time.Second
)

var nonceRe = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

// Matcher decides what an alert means for orders. It is optional until card 06 exists.
type Matcher interface {
	Process(ctx context.Context, alertID string) error
}

// Deps are the services ingest needs.
type Deps struct {
	Store    *store.Store
	Clock    clock.Clock
	Devices  *devices.Service
	Evidence *evidence.Service
	Matcher  Matcher
}

// signingString is what the device signs. Changing it breaks every paired device.
func signingString(deviceID string, ts int64, nonce string, body []byte) []byte {
	sum := sha256.Sum256(body)
	return []byte(fmt.Sprintf("%s\n%s\n%d\n%s\n%s", signingVersion, deviceID, ts, nonce, hex.EncodeToString(sum[:])))
}

// Sign returns the four headers a device must send. Used by the apps and the simulator.
func Sign(key ed25519.PrivateKey, deviceID string, ts time.Time, nonce string, body []byte) http.Header {
	sig := ed25519.Sign(key, signingString(deviceID, ts.Unix(), nonce, body))
	h := http.Header{}
	h.Set("X-Kaasu-Device", deviceID)
	h.Set("X-Kaasu-Timestamp", strconv.FormatInt(ts.Unix(), 10))
	h.Set("X-Kaasu-Nonce", nonce)
	h.Set("X-Kaasu-Signature", base64.StdEncoding.EncodeToString(sig))
	return h
}

type apiError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func fail(w http.ResponseWriter, status int, code, msg string) {
	var e apiError
	e.Error.Code, e.Error.Message = code, msg
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(e)
}

// Handler serves the ingest listener: /ingest/v1/pair and /ingest/v1/alerts.
func Handler(d Deps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /ingest/v1/pair", func(w http.ResponseWriter, r *http.Request) { pair(d, w, r) })
	mux.HandleFunc("POST /ingest/v1/alerts", func(w http.ResponseWriter, r *http.Request) { alert(d, w, r) })
	return mux
}

func readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			fail(w, http.StatusRequestEntityTooLarge, "too_large", "request body is too large")
		} else {
			fail(w, http.StatusBadRequest, "invalid_request", "could not read body")
		}
		return nil, false
	}
	return b, true
}

type pairReq struct {
	Token     string `json:"token"`
	PublicKey string `json:"public_key"`
	Label     string `json:"label"`
	Kind      string `json:"kind"`
}

func pair(d Deps, w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	var req pairReq
	if err := json.Unmarshal(body, &req); err != nil {
		fail(w, http.StatusBadRequest, "invalid_request", "body must be JSON")
		return
	}
	pub, err := base64.StdEncoding.DecodeString(req.PublicKey)
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid_request", "public_key must be base64")
		return
	}
	dev, err := d.Devices.Pair(r.Context(), req.Token, ed25519.PublicKey(pub), req.Label, req.Kind)
	switch {
	case errors.Is(err, devices.ErrBadToken):
		fail(w, http.StatusUnauthorized, "unauthorized", "pairing token is invalid, expired or already used")
	case errors.Is(err, devices.ErrInvalid):
		fail(w, http.StatusBadRequest, "invalid_request", "invalid device details")
	case err != nil:
		fail(w, http.StatusInternalServerError, "internal", "could not pair the device")
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"device_id": dev.ID})
	}
}

// verify checks every header, the window and the signature. All failures look the same to the caller.
func verify(d Deps, r *http.Request, body []byte) (devices.Device, string, error) {
	deviceID := r.Header.Get("X-Kaasu-Device")
	nonce := r.Header.Get("X-Kaasu-Nonce")
	tsText := r.Header.Get("X-Kaasu-Timestamp")
	sig, err := base64.StdEncoding.DecodeString(r.Header.Get("X-Kaasu-Signature"))
	ts, tsErr := strconv.ParseInt(tsText, 10, 64)
	if deviceID == "" || !nonceRe.MatchString(nonce) || err != nil || tsErr != nil {
		return devices.Device{}, "", errUnauthorized
	}
	dev, err := d.Devices.Authenticate(r.Context(), deviceID)
	if err != nil {
		return devices.Device{}, "", errUnauthorized
	}
	now := d.Clock.Now()
	skew := now.Sub(time.Unix(ts, 0))
	if skew < -window || skew > window {
		return devices.Device{}, "", errUnauthorized
	}
	if !ed25519.Verify(dev.PublicKey, signingString(deviceID, ts, nonce, body), sig) {
		return devices.Device{}, "", errUnauthorized
	}
	return dev, nonce, nil
}

var (
	errUnauthorized = errors.New("unauthorized")
	errReplay       = errors.New("replay")
)

type alertReq struct {
	Source     string `json:"source"`
	Sender     string `json:"sender"`
	Text       string `json:"text"`
	ReceivedAt string `json:"received_at"`
}

func alert(d Deps, w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r)
	if !ok {
		return
	}
	dev, nonce, err := verify(d, r, body)
	if err != nil {
		fail(w, http.StatusUnauthorized, "unauthorized", "request could not be verified")
		return
	}
	status, resp, err := accept(r.Context(), d, dev, nonce, body)
	switch {
	case errors.Is(err, errReplay):
		fail(w, http.StatusConflict, "replay", "this request was already received")
	case err != nil:
		fail(w, http.StatusInternalServerError, "internal", "could not store the alert")
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// accept does everything for one verified request in one transaction: nonce, evidence, parse, alert.
func accept(ctx context.Context, d Deps, dev devices.Device, nonce string, raw []byte) (int, map[string]any, error) {
	status := http.StatusAccepted
	resp := map[string]any{"status": "accepted"}
	err := d.Store.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO nonces(device_id, nonce, seen_at) VALUES(?,?,?)`,
			dev.ID, nonce, d.Clock.Now().UTC().Format(time.RFC3339)); err != nil {
			if strings.Contains(err.Error(), "UNIQUE constraint failed") {
				return errReplay
			}
			return err
		}
		if err := d.Devices.TouchSeen(ctx, tx, dev.ID); err != nil {
			return err
		}
		evID, _, err := d.Evidence.SaveEvidence(ctx, tx, "device", dev.ID, raw)
		if err != nil {
			return err
		}
		// A signed body that is not valid JSON is still kept as evidence and reported as unparsed.
		var req alertReq
		if err := json.Unmarshal(raw, &req); err != nil {
			resp["status"] = "unparsed"
			_, err := ledger.Append(ctx, tx, d.Clock, ledger.Entry{Kind: "alert.unparsed", Payload: map[string]any{"evidence_id": evID, "reason": "invalid_json"}})
			return err
		}
		p, ok := parsers.ForSender(req.Sender)
		if !ok {
			resp["status"] = "ignored"
			return nil
		}
		parsed, err := p.Parse(req.Text)
		if err != nil { // unknown format or any other parser failure: keep the evidence, report unparsed
			resp["status"] = "unparsed"
			_, err := ledger.Append(ctx, tx, d.Clock, ledger.Entry{Kind: "alert.unparsed", Payload: map[string]any{"evidence_id": evID, "parser": p.Name()}})
			return err
		}
		alertID, isNew, err := d.Evidence.SaveAlert(ctx, tx, evID, parsed)
		if err != nil {
			return err
		}
		resp["alert_id"] = alertID
		resp["duplicate"] = !isNew
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	if d.Matcher != nil && resp["alert_id"] != nil {
		if err := d.Matcher.Process(ctx, resp["alert_id"].(string)); err != nil {
			return 0, nil, err
		}
	}
	return status, resp, nil
}
