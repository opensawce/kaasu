// Package voice decides when the merchant hears a payment, and what is said. Only a confirmed payment is ever spoken.
package voice

import (
	"github.com/opensawce/kaasu/i18n"
)

// Announcement returns the text to speak for an order that just changed to state. ok is false for every
// state other than a confirmation: held, waiting, refunded or disputed payments are never spoken.
func Announcement(state string, amountPaise int64, l i18n.Lang) (text string, ok bool) {
	switch state {
	case "confirmed", "late_paid":
		return i18n.Spoken(amountPaise, l), true
	}
	return "", false
}

// BCP47 maps a Kaasu language to the speech tag a browser or OS voice uses.
func BCP47(l i18n.Lang) string {
	tags := map[i18n.Lang]string{"en": "en-IN", "hi": "hi-IN", "ta": "ta-IN", "te": "te-IN", "kn": "kn-IN",
		"ml": "ml-IN", "mr": "mr-IN", "bn": "bn-IN", "gu": "gu-IN", "pa": "pa-IN", "or": "or-IN"}
	if t, ok := tags[l]; ok {
		return t
	}
	return "en-IN"
}
