// Package api serves the merchant REST API under /v1. Money is always integer paise.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/opensawce/kaasu/internal/ledger"
	"github.com/opensawce/kaasu/internal/orders"
	"github.com/opensawce/kaasu/internal/secrets"
)

const maxBody = 64 << 10

// Deps are the services the API needs.
type Deps struct {
	Orders *orders.Service
	Vault  *secrets.Vault
}

// Routes lists every route, so tests can check openapi.yaml documents them all.
func Routes() []string {
	return []string{"POST /v1/orders", "GET /v1/orders/{id}", "POST /v1/orders/{id}/cancel"}
}

type server struct{ Deps }

// New returns the /v1 handler. Every route needs a valid API key sent as a Bearer token.
func New(d Deps) http.Handler {
	s := &server{d}
	m := http.NewServeMux()
	m.HandleFunc("POST /v1/orders", s.auth(s.createOrder))
	m.HandleFunc("GET /v1/orders/{id}", s.auth(s.getOrder))
	m.HandleFunc("POST /v1/orders/{id}/cancel", s.auth(s.cancelOrder))
	return m
}

type errBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, code, msg string) {
	var b errBody
	b.Error.Code, b.Error.Message = code, msg
	writeJSON(w, status, b)
}

func (s *server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			fail(w, http.StatusUnauthorized, "unauthorized", "send your API key as: Authorization: Bearer <key>")
			return
		}
		key, err := s.Vault.VerifyAPIKey(r.Context(), tok)
		if err != nil {
			fail(w, http.StatusUnauthorized, "unauthorized", "invalid or revoked API key")
			return
		}
		ctx := ledger.WithActor(r.Context(), "api:"+key.ID)
		next(w, r.WithContext(ctx))
	}
}

type orderJSON struct {
	ID              string `json:"id"`
	AccountID       string `json:"account_id"`
	State           string `json:"state"`
	BaseAmountPaise int64  `json:"base_amount_paise"`
	AmountPaise     int64  `json:"amount_paise"`
	Currency        string `json:"currency"`
	Reference       string `json:"reference,omitempty"`
	Note            string `json:"note,omitempty"`
	UPILink         string `json:"upi_link"`
	ExpiresAt       string `json:"expires_at"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
	ConfirmedAt     string `json:"confirmed_at,omitempty"`
}

func toJSON(o orders.Order) orderJSON {
	return orderJSON{o.ID, o.AccountID, o.State, o.BaseAmountPaise, o.AmountPaise, o.Currency, o.Reference, o.Note, o.UPILink,
		o.ExpiresAt, o.CreatedAt, o.UpdatedAt, o.ConfirmedAt}
}

type createReq struct {
	AmountPaise      int64  `json:"amount_paise"`
	AccountID        string `json:"account_id"`
	Reference        string `json:"reference"`
	Note             string `json:"note"`
	ExpiresInSeconds int64  `json:"expires_in_seconds"`
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			fail(w, http.StatusRequestEntityTooLarge, "too_large", "request body is too large")
		} else {
			fail(w, http.StatusBadRequest, "invalid_request", "body must be one JSON object with integer paise amounts: "+err.Error())
		}
		return false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		fail(w, http.StatusBadRequest, "invalid_request", "body must contain exactly one JSON object")
		return false
	}
	return true
}

func (s *server) createOrder(w http.ResponseWriter, r *http.Request) {
	var req createReq
	if !decode(w, r, &req) {
		return
	}
	o, created, err := s.Orders.Create(r.Context(), orders.CreateInput{
		AmountPaise: req.AmountPaise, AccountID: req.AccountID, Reference: req.Reference, Note: req.Note,
		ExpiresIn: time.Duration(req.ExpiresInSeconds) * time.Second,
	}, r.Header.Get("Idempotency-Key"))
	if err != nil {
		s.orderError(w, err)
		return
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
		w.Header().Set("Idempotent-Replayed", "true")
	}
	writeJSON(w, status, toJSON(o))
}

func (s *server) getOrder(w http.ResponseWriter, r *http.Request) {
	o, err := s.Orders.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.orderError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toJSON(o))
}

func (s *server) cancelOrder(w http.ResponseWriter, r *http.Request) {
	o, err := s.Orders.Cancel(r.Context(), r.PathValue("id"))
	if err != nil {
		s.orderError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toJSON(o))
}

func (s *server) orderError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, orders.ErrInvalid):
		fail(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, orders.ErrNotFound):
		fail(w, http.StatusNotFound, "not_found", "order not found")
	case errors.Is(err, orders.ErrIdempotencyConflict):
		fail(w, http.StatusUnprocessableEntity, "idempotency_conflict", err.Error())
	case errors.Is(err, orders.ErrNotCancellable):
		fail(w, http.StatusConflict, "not_cancellable", err.Error())
	case errors.Is(err, context.Canceled):
		fail(w, 499, "canceled", "request canceled")
	default:
		fail(w, http.StatusInternalServerError, "internal", "something went wrong; the details are in the server log")
	}
}
