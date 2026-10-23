// Package upi builds UPI payment links. Personal VPAs use only pa, pn, am, cu and tn:
// merchant fields (mc, tr, sign) make some apps reject or limit the payment.
package upi

import (
	"fmt"
	"net/url"
	"strings"
)

const maxNoteRunes = 50

// FormatPaise renders integer paise as rupees with two decimals, using integer maths only.
func FormatPaise(p int64) string { return fmt.Sprintf("%d.%02d", p/100, p%100) }

func esc(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }

// Link returns upi://pay?pa=...&pn=...&am=...&cu=INR[&tn=...]. The order of fields is fixed.
func Link(vpa, payeeName string, amountPaise int64, note string) string {
	var b strings.Builder
	b.WriteString("upi://pay?pa=" + esc(vpa) + "&pn=" + esc(payeeName) + "&am=" + FormatPaise(amountPaise) + "&cu=INR")
	if r := []rune(strings.TrimSpace(note)); len(r) > 0 {
		if len(r) > maxNoteRunes {
			r = r[:maxNoteRunes]
		}
		b.WriteString("&tn=" + esc(string(r)))
	}
	return b.String()
}
