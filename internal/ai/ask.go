package ai

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/opensawce/kaasu/internal/askdata"
)

// ErrNoQuery means the question does not map to any known question.
var ErrNoQuery = errors.New("no matching question")

var askSchema = map[string]any{
	"type":     "object",
	"required": []string{"query"},
	"properties": map[string]any{
		"query": map[string]any{"type": "string", "maxLength": 40, "enum": []string{"total_received", "count_orders", "refunds_total", "review_backlog", "none"}},
		"from":  map[string]any{"type": "string", "maxLength": 40},
		"to":    map[string]any{"type": "string", "maxLength": 40},
		"state": map[string]any{"type": "string", "maxLength": 20},
	},
}

type askAnswer struct {
	Query string `json:"query"`
	From  string `json:"from"`
	To    string `json:"to"`
	State string `json:"state"`
}

const askSystem = "Choose exactly one query name from the list, or none, and fill its dates (RFC 3339) and state if needed. Answer only with JSON."

// ceilSecond rounds up to a whole second in UTC, since stored timestamps carry whole seconds only.
func ceilSecond(t time.Time) time.Time {
	t = t.UTC()
	if t.Nanosecond() != 0 {
		return t.Truncate(time.Second).Add(time.Second)
	}
	return t
}

// Ask answers a question with a fixed query. The result carries the numbers used, so the owner can check them.
func Ask(ctx context.Context, s *Service, question string, now time.Time) (askdata.Result, error) {
	resp, err := s.Run(ctx, "ask", askSystem, question, askSchema)
	if err != nil {
		return askdata.Result{}, err
	}
	var a askAnswer
	if err := json.Unmarshal(resp.JSON, &a); err != nil {
		return askdata.Result{}, ErrSchema
	}
	if a.Query == "none" {
		return askdata.Result{}, ErrNoQuery
	}
	p := askdata.Params{From: a.From, To: a.To, State: a.State}
	if p.From == "" {
		p.From = now.AddDate(0, 0, -30).UTC().Format(time.RFC3339)
	}
	if p.To == "" {
		p.To = ceilSecond(now).Format(time.RFC3339) // exclusive bound: keep the fraction of a second by rounding up
	}
	return askdata.Run(ctx, s.st, a.Query, p)
}
