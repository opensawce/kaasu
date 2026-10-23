package ai

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/opensawce/kaasu/internal/evidence"
)

// ErrNotCredit is an answer that says the message is not money received. It is kept for the owner, not confirmed.
var ErrNotCredit = errors.New("the message is not a credit")

var extractSchema = map[string]any{
	"type":     "object",
	"required": []string{"amount_paise", "utr", "occurred_at", "is_credit", "bank"},
	"properties": map[string]any{
		"amount_paise": map[string]any{"type": "integer", "minimum": int64(1), "maximum": int64(10000000_00)},
		"utr":          map[string]any{"type": "string", "maxLength": 12},
		"occurred_at":  map[string]any{"type": "string", "maxLength": 40},
		"is_credit":    map[string]any{"type": "boolean"},
		"bank":         map[string]any{"type": "string", "maxLength": 40},
	},
}

type extracted struct {
	AmountPaise int64  `json:"amount_paise"`
	UTR         string `json:"utr"`
	OccurredAt  string `json:"occurred_at"`
	IsCredit    bool   `json:"is_credit"`
	Bank        string `json:"bank"`
}

const extractSystem = "Extract the amount in integer paise, the 12-digit UTR (or empty), the time, whether money was received, and the bank name from this bank message. Answer only with the JSON object."

// Extract reads an alert no parser knows. The result is always marked Parser "ai" so it can only go to review.
func Extract(ctx context.Context, s *Service, text string) (evidence.Parsed, error) {
	resp, err := s.Run(ctx, "extract", extractSystem, text, extractSchema)
	if err != nil {
		return evidence.Parsed{}, err
	}
	var x extracted
	if err := json.Unmarshal(resp.JSON, &x); err != nil {
		return evidence.Parsed{}, ErrSchema
	}
	if !x.IsCredit {
		return evidence.Parsed{}, ErrNotCredit
	}
	when, err := time.Parse(time.RFC3339, x.OccurredAt)
	if err != nil {
		return evidence.Parsed{}, ErrSchema
	}
	return evidence.Parsed{Bank: x.Bank, AmountPaise: x.AmountPaise, UTR: x.UTR, OccurredAt: when.UTC(), Parser: "ai"}, nil
}

// DraftNote is the suggestion file for a maintainer: masked text and the fields read, with no identifiers.
func DraftNote(maskedText string, p evidence.Parsed) string {
	b, _ := json.MarshalIndent(map[string]any{
		"bank": p.Bank, "amount_paise": p.AmountPaise, "occurred_at": p.OccurredAt.Format(time.RFC3339),
		"utr_present": p.UTR != "",
	}, "", "  ")
	return "# Unknown bank format (draft)\n\nMasked sample:\n\n```\n" + Mask(maskedText) + "\n```\n\nFields read:\n\n```json\n" + string(b) + "\n```\n\nOpen a bank-format report issue with this sample.\n"
}
