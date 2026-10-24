package ai

import (
	"context"
	"testing"
)

var hintsOn = map[string]string{"ai.enabled": "1", "ai.feature.hints": "1", "ai.notice_ack_at": "x", "ai.monthly_cap_inr": "500"}

func TestDetectorsFireAtTheirThresholds(t *testing.T) {
	if len(Detect(Facts{})) != 0 {
		t.Error("quiet day should produce no hints")
	}
	ids := map[string]bool{}
	for _, h := range Detect(Facts{ReviewItems: 5, SamePayerAmounts: 3, JustBelowLimit: 3, NightAlerts: 3}) {
		ids[h.ID] = true
	}
	for _, want := range []string{"review_burst", "payer_pattern", "near_limit", "night_alerts"} {
		if !ids[want] {
			t.Errorf("%s did not fire", want)
		}
	}
	if len(Detect(Facts{ReviewItems: 4})) != 0 {
		t.Error("below threshold must stay quiet")
	}
}

// AI phrasing never runs for a hint without a detector ID.
func TestHintWithoutDetectorIsDropped(t *testing.T) {
	s, _, _ := setup(t, hintsOn, `{"text":"do something"}`)
	if _, err := Phrase(context.Background(), s, Hint{Text: "made up"}); err == nil {
		t.Error("a hint with no detector ID must not be phrased")
	}
}

func TestPhraseUsesStubAnswer(t *testing.T) {
	s, _, _ := setup(t, hintsOn, `{"text":"Several payments wait for you."}`)
	out, err := Phrase(context.Background(), s, Hint{ID: "review_burst", Text: "5 review items"})
	if err != nil || out != "Several payments wait for you." {
		t.Errorf("out=%q err=%v", out, err)
	}
}

// Setup help uses a fixed shape: at most five short steps per topic, nothing else.
func TestSetupHelpShapeEnforced(t *testing.T) {
	schema := map[string]any{"type": "object", "required": []string{"battery_saver"}, "properties": map[string]any{
		"battery_saver": map[string]any{"type": "string", "maxLength": 300},
	}}
	s, _, _ := setup(t, hintsOn, `{"battery_saver":"Turn off battery saver for Kaasu.","extra":"x"}`)
	if _, err := s.Run(context.Background(), "hints", "sys", "Pixel 7", schema); err == nil {
		t.Error("extra fields in setup help must be refused")
	}
}

func TestAllAIFeaturesOffByDefault(t *testing.T) {
	s, _, _ := setup(t, nil, `{"text":"x"}`)
	if _, err := Phrase(context.Background(), s, Hint{ID: "review_burst", Text: "x"}); err == nil {
		t.Error("off by default")
	}
}
