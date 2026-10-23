package voice

import (
	"testing"

	"github.com/opensawce/kaasu/i18n"
)

// Only a confirmation is ever spoken. Every other state must be silent.
func TestAnnouncementOnlyAfterConfirmation(t *testing.T) {
	states := []string{"created", "queued", "awaiting", "matched", "confirmed", "review", "rejected",
		"cancelled", "expired", "late_paid", "refunded", "disputed"}
	for _, s := range states {
		_, ok := Announcement(s, 49900, "en")
		want := s == "confirmed" || s == "late_paid"
		if ok != want {
			t.Errorf("state %s: spoken=%v want %v", s, ok, want)
		}
	}
}

func TestSpokenAmount(t *testing.T) {
	if text, _ := Announcement("confirmed", 49900, "en"); text != "₹499 received" {
		t.Errorf("whole rupees: %q", text)
	}
	if text, _ := Announcement("confirmed", 49907, "en"); text != "₹499.07 received" {
		t.Errorf("with paise: %q", text)
	}
}

func TestVoiceLanguageIsAllLanguages(t *testing.T) {
	for _, l := range i18n.Languages {
		if text, ok := Announcement("confirmed", 100, l); !ok || text == "" {
			t.Errorf("%s: no announcement", l)
		}
		if BCP47(l) == "" {
			t.Errorf("%s: no speech tag", l)
		}
	}
	if BCP47("xx") != "en-IN" {
		t.Error("unknown language should fall back to English speech")
	}
}
