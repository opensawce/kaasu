package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/opensawce/kaasu/internal/evidence"
)

var extOn = map[string]string{"ai.enabled": "1", "ai.feature.extract": "1", "ai.notice_ack_at": "x", "ai.monthly_cap_inr": "500"}

func TestExtractMarksResultAsAI(t *testing.T) {
	s, _, _ := setup(t, extOn, `{"amount_paise":49907,"utr":"629812345678","occurred_at":"2026-01-15T10:00:00Z","is_credit":true,"bank":"X"}`)
	p, err := Extract(context.Background(), s, "some new wording")
	if err != nil {
		t.Fatal(err)
	}
	if p.Parser != "ai" || p.AmountPaise != 49907 {
		t.Errorf("parsed=%+v", p)
	}
}

func TestExtractDebitIsNotAccepted(t *testing.T) {
	s, _, _ := setup(t, extOn, `{"amount_paise":100,"utr":"","occurred_at":"2026-01-15T10:00:00Z","is_credit":false,"bank":"X"}`)
	if _, err := Extract(context.Background(), s, "debited"); !errors.Is(err, ErrNotCredit) {
		t.Errorf("debit: %v", err)
	}
}

// A fractional amount is a schema failure: money is integer paise only.
func TestExtractAmountMustBeInteger(t *testing.T) {
	s, _, _ := setup(t, extOn, `{"amount_paise":499.07,"utr":"","occurred_at":"2026-01-15T10:00:00Z","is_credit":true,"bank":"X"}`)
	if _, err := Extract(context.Background(), s, "x"); !errors.Is(err, ErrSchema) {
		t.Errorf("float amount: %v", err)
	}
}

func TestDraftNoteHasNoRawIdentifiers(t *testing.T) {
	p := evidence.Parsed{Bank: "X", AmountPaise: 100, UTR: "629812345678"}
	note := DraftNote("paid by ravi.kumar@okicici, utr 629812345678", p)
	if strings.Contains(note, "ravi.kumar") || strings.Contains(note, "629812345678") {
		t.Errorf("draft leaks identifiers: %s", note)
	}
	var v any
	if err := json.Unmarshal([]byte(strings.Split(strings.Split(note, "```json\n")[1], "\n```")[0]), &v); err != nil {
		t.Errorf("fields block is not JSON: %v", err)
	}
}
