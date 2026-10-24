// Package ai is the optional assistant layer. It is off by default, uses the owner's own key, sees masked text only,
// returns structured output only, and can never change an order. Nothing here imports states, match, orders or refunds.
package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/opensawce/kaasu/internal/id"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/ledger"
	"github.com/opensawce/kaasu/internal/secrets"
	"github.com/opensawce/kaasu/internal/store"
)

var (
	ErrDisabled = errors.New("the AI feature is off")
	ErrBudget   = errors.New("the monthly AI budget is used up")
	ErrUnmasked = errors.New("text still contains an identifier after masking")
	ErrSchema   = errors.New("the answer does not match the expected shape")
)

// Request is one call. Schema describes the only shape the answer may take.
type Request struct {
	Feature string
	System  string
	User    string
	Schema  map[string]any
}

// Response is the provider's answer.
type Response struct {
	JSON         json.RawMessage
	Model        string
	InputTokens  int
	OutputTokens int
}

// Provider is one model vendor. Implementations live beside this file; tests use Stub.
type Provider interface {
	Name() string
	Complete(ctx context.Context, r Request) (Response, error)
}

var (
	vpaRe  = regexp.MustCompile(`(?i)\b[A-Za-z0-9._-]{2,}@[a-z]{3,}\b`)
	utrRe  = regexp.MustCompile(`\b[0-9]{12}\b`)
	acctRe = regexp.MustCompile(`\b[0-9]{9,18}\b`)
	mailRe = regexp.MustCompile(`(?i)\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[a-z]{2,}\b`)
)

// Mask replaces identifiers before any text leaves the machine.
func Mask(s string) string {
	s = mailRe.ReplaceAllString(s, "[email]")
	s = vpaRe.ReplaceAllStringFunc(s, func(m string) string {
		name, handle, _ := strings.Cut(m, "@")
		return name[:min(2, len(name))] + "***@" + handle
	})
	s = utrRe.ReplaceAllString(s, "[utr]")
	s = acctRe.ReplaceAllString(s, "[account]")
	return s
}

// Service enforces every rule in one place: enabled, masked input, the monthly cap, the schema, and the ledger record.
type Service struct {
	st       *store.Store
	clk      clock.Clock
	vault    *secrets.Vault
	provider Provider
}

func New(st *store.Store, clk clock.Clock, vault *secrets.Vault, p Provider) *Service {
	return &Service{st: st, clk: clk, vault: vault, provider: p}
}

// setting reads a flag or number from settings; missing means the default.
func (s *Service) setting(ctx context.Context, key, def string) string {
	var v string
	if err := s.st.Read().QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v); err != nil {
		return def
	}
	return v
}

// Run makes one call that passes every gate. The answer is returned for the caller to show; it is never applied.
func (s *Service) Run(ctx context.Context, feature, system, user string, schema map[string]any) (Response, error) {
	if s.setting(ctx, "ai.enabled", "0") != "1" || s.setting(ctx, "ai.feature."+feature, "0") != "1" {
		return Response{}, ErrDisabled
	}
	if s.setting(ctx, "ai.notice_ack_at", "") == "" {
		return Response{}, ErrDisabled
	}
	maskedUser, maskedSystem := Mask(user), Mask(system)
	if vpaRe.MatchString(maskedUser) || utrRe.MatchString(maskedUser) {
		return Response{}, ErrUnmasked
	}
	if err := s.checkBudget(ctx); err != nil {
		return Response{}, err
	}
	resp, err := s.provider.Complete(ctx, Request{Feature: feature, System: maskedSystem, User: maskedUser, Schema: schema})
	if err != nil {
		return Response{}, err
	}
	if err := Validate(schema, resp.JSON); err != nil {
		return Response{}, err
	}
	return resp, s.record(ctx, feature, resp)
}

func (s *Service) checkBudget(ctx context.Context) error {
	capRupees := s.setting(ctx, "ai.monthly_cap_inr", "0")
	if capRupees == "0" || capRupees == "" {
		return ErrBudget
	}
	var spent int64
	// The cap runs on the IST calendar month; usage rows are stamped in UTC, so compare against the UTC bounds of that month.
	now := s.clk.Now().In(istZone)
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, istZone).UTC().Format(time.RFC3339)
	end := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, istZone).UTC().Format(time.RFC3339)
	if err := s.st.Read().QueryRowContext(ctx, `SELECT COALESCE(SUM(cost_micro_inr),0) FROM ai_usage WHERE at >= ? AND at < ?`, start, end).Scan(&spent); err != nil {
		return fmt.Errorf("ai: read usage: %w", err)
	}
	limit := parseRupees(capRupees) * 1_000_000
	if spent >= limit {
		return ErrBudget
	}
	return nil
}

func (s *Service) record(ctx context.Context, feature string, r Response) error {
	return s.st.Tx(ctx, func(tx *sql.Tx) error {
		cost := int64(r.InputTokens*3 + r.OutputTokens*15) // estimate in micro-rupees; see budget.go
		if _, err := tx.ExecContext(ctx, `INSERT INTO ai_usage(id, at, feature, provider, model, input_tokens, output_tokens, cost_micro_inr)
			VALUES(?,?,?,?,?,?,?,?)`, id.New("aiu"), s.clk.Now().UTC().Format(time.RFC3339),
			feature, s.provider.Name(), r.Model, r.InputTokens, r.OutputTokens, cost); err != nil {
			return err
		}
		_, err := ledger.Append(ledger.WithActor(ctx, "ai"), tx, s.clk, ledger.Entry{Kind: "ai.suggestion",
			Payload: map[string]any{"feature": feature, "provider": s.provider.Name(), "model": r.Model}})
		return err
	})
}

func parseRupees(s string) int64 {
	var v int64
	for _, c := range s {
		if c >= '0' && c <= '9' {
			v = v*10 + int64(c-'0')
		}
	}
	return v
}

var istZone = time.FixedZone("IST", 5*3600+30*60)

// Validate checks an answer against a small JSON Schema subset: type, properties, required, enum, min/max, and no extras.
func Validate(schema map[string]any, raw json.RawMessage) error {
	var v any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return ErrSchema
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) { // a second value or trailing data is not an answer
		return ErrSchema
	}
	return check(schema, v)
}

func check(schema map[string]any, v any) error {
	switch schema["type"] {
	case "object":
		obj, ok := v.(map[string]any)
		if !ok {
			return ErrSchema
		}
		props, _ := schema["properties"].(map[string]any)
		for k := range obj {
			if _, ok := props[k]; !ok {
				return fmt.Errorf("%w: unexpected field %q", ErrSchema, k)
			}
		}
		if req, ok := schema["required"].([]string); ok {
			for _, r := range req {
				if _, ok := obj[r]; !ok {
					return fmt.Errorf("%w: missing %q", ErrSchema, r)
				}
			}
		}
		for k, sub := range props {
			if val, ok := obj[k]; ok {
				if err := check(sub.(map[string]any), val); err != nil {
					return err
				}
			}
		}
		return nil
	case "integer":
		n, ok := v.(json.Number)
		if !ok {
			return ErrSchema
		}
		i, err := n.Int64()
		if err != nil {
			return ErrSchema
		}
		if mn, ok := schema["minimum"].(int64); ok && i < mn {
			return ErrSchema
		}
		if mx, ok := schema["maximum"].(int64); ok && i > mx {
			return ErrSchema
		}
		return nil
	case "string":
		str, ok := v.(string)
		if !ok {
			return ErrSchema
		}
		if ml, ok := schema["maxLength"].(int); ok && len(str) > ml {
			return ErrSchema
		}
		if en, ok := schema["enum"].([]string); ok {
			for _, e := range en {
				if e == str {
					return nil
				}
			}
			return ErrSchema
		}
		return nil
	case "boolean":
		if _, ok := v.(bool); !ok {
			return ErrSchema
		}
		return nil
	}
	return ErrSchema
}
