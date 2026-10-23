package ai

import (
	"context"
	"encoding/json"
)

// Hint is an observation from a plain rule. It never changes anything; it is shown as a suggestion.
type Hint struct {
	ID   string // which detector produced it; the AI may only phrase hints that carry an ID
	Text string
}

// Facts are the numbers the detectors read. Amounts are integer paise.
type Facts struct {
	ReviewItems      int
	SamePayerAmounts int // the same masked payer paid several different amounts today
	JustBelowLimit   int // confirmed amounts within 1% under the auto-confirm limit
	NightAlerts      int // alerts received between 00:00 and 05:00 IST
}

// Detect returns the hints for today's facts. Each detector is a fixed threshold.
func Detect(f Facts) []Hint {
	var out []Hint
	if f.ReviewItems >= 5 {
		out = append(out, Hint{"review_burst", "Several payments are waiting for your OK."})
	}
	if f.SamePayerAmounts >= 3 {
		out = append(out, Hint{"payer_pattern", "One payer sent several different amounts today."})
	}
	if f.JustBelowLimit >= 3 {
		out = append(out, Hint{"near_limit", "Several payments sit just under your auto-confirm limit."})
	}
	if f.NightAlerts >= 3 {
		out = append(out, Hint{"night_alerts", "Payments arrived late at night. Worth a look."})
	}
	return out
}

// Phrase rewrites a detector's hint in the owner's language through the model. Hints with no detector ID are dropped.
func Phrase(ctx context.Context, s *Service, h Hint) (string, error) {
	if h.ID == "" {
		return "", ErrSchema
	}
	schema := map[string]any{"type": "object", "required": []string{"text"}, "properties": map[string]any{"text": map[string]any{"type": "string", "maxLength": 200}}}
	resp, err := s.Run(ctx, "hints", "Rewrite this observation in one plain sentence. Do not suggest actions. Answer JSON {\"text\":...}.", h.Text, schema)
	if err != nil {
		return "", err
	}
	var v struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(resp.JSON, &v); err != nil {
		return "", ErrSchema
	}
	return v.Text, nil
}
