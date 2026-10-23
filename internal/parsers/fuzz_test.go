package parsers

import (
	"strings"
	"testing"
)

func FuzzParseAmountPaise(f *testing.F) {
	for _, s := range []string{"499.07", "1,25,000.50", "Rs.5", "", "1e3", "12.345"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if p, err := ParseAmountPaise(s); err == nil && (p <= 0 || p > maxPaise) {
			t.Fatalf("accepted %q as %d", s, p)
		}
	})
}

func FuzzParseAll(f *testing.F) {
	f.Add("Rs.499.07 credited to a/c XX1234 on 23-10-26 by VPA a@okicici (UPI Ref No 629812345678).")
	f.Add("INR 299.00 credited to A/c no. XX1234 on 23-10-2026 UPI/P2A/629812345678/RAVI K/Axis Bank")
	f.Fuzz(func(t *testing.T, text string) {
		for _, p := range Registry() {
			got, err := p.Parse(text)
			if err != nil {
				continue
			}
			if got.AmountPaise <= 0 || got.OccurredAt.IsZero() {
				t.Fatalf("%s accepted bad fields: %+v", p.Name(), got)
			}
			if got.UTR != "" && (len(got.UTR) != 12 || strings.Trim(got.UTR, "0123456789") != "") {
				t.Fatalf("bad UTR %q", got.UTR)
			}
		}
	})
}
