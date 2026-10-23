package parsers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/opensawce/kaasu/internal/testutil"
)

func TestParseAmountPaise(t *testing.T) {
	tests := map[string]int64{
		"0.05":           5,
		"5":              500,
		".5":             0, // invalid, checked below
		"499.07":         49907,
		"499.7":          49970,
		"1,25,000.50":    12500050,
		"Rs.499.07":      49907,
		"INR 1,00,000":   10000000,
		"₹1,20,000.50":   12000050,
		"1,00,00,00,000": 0, // over the cap
	}
	for in, want := range tests {
		got, err := ParseAmountPaise(in)
		if want == 0 {
			if err == nil {
				t.Errorf("%q should fail, got %d", in, got)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("%q: got %d err=%v want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "12.345", "1e3", "-5", "abc", "1.2.3", "12..3", ".", "1,00,00,00,00,000.00"} {
		if _, err := ParseAmountPaise(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestNoFloatParsing(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, _ := os.ReadFile(f)
		if strings.Contains(string(b), "ParseFloat") {
			t.Errorf("%s uses ParseFloat; money must be integer paise", f)
		}
	}
}

func TestGoldenFixtures(t *testing.T) {
	for _, p := range Registry() {
		dir := filepath.Join("testdata", strings.ToLower(p.Name()))
		files, err := filepath.Glob(filepath.Join(dir, "*.txt"))
		if err != nil || len(files) == 0 {
			t.Fatalf("%s: no fixtures", p.Name())
		}
		for _, f := range files {
			raw, _ := os.ReadFile(f)
			name := strings.TrimSuffix(filepath.Base(f), ".txt")
			got, err := p.Parse(strings.TrimSpace(string(raw)))
			var out any = map[string]string{"error": "unknown format"}
			if err == nil {
				out = got
			}
			b, _ := json.MarshalIndent(out, "", "  ")
			testutil.Golden(t, filepath.Join(strings.ToLower(p.Name()), name), append(b, '\n'))
		}
	}
}

func TestCreditsOnlyAndNoDebitParsing(t *testing.T) {
	for _, p := range Registry() {
		dir := filepath.Join("testdata", strings.ToLower(p.Name()))
		files, _ := filepath.Glob(filepath.Join(dir, "*.txt"))
		for _, f := range files {
			raw, _ := os.ReadFile(f)
			if strings.Contains(strings.ToLower(string(raw)), "debited") {
				if _, err := p.Parse(string(raw)); err != ErrUnknownFormat {
					t.Errorf("%s: debit must be unknown format, got %v", f, err)
				}
			}
		}
	}
}

func TestPromoAndUnknownBecomeUnknownFormat(t *testing.T) {
	p, _ := ForSender("VM-ICICIB")
	if _, err := p.Parse("Offer! Get 20% cashback. Reply STOP."); err != ErrUnknownFormat {
		t.Errorf("promo: %v", err)
	}
}

func TestForSender(t *testing.T) {
	tests := map[string]string{
		"AD-HDFCBK":           "HDFC",
		"VM-HDFCBK-S":         "HDFC",
		"SBIUPI":              "SBI",
		"AD-ICICIB":           "ICICI",
		"AXISBK":              "Axis",
		"KOTAKB":              "Kotak",
		"alerts@hdfcbank.net": "HDFC",
		"mail.sbi.co.in":      "SBI",
		"x@icicibank.com":     "ICICI",
	}
	for in, want := range tests {
		p, ok := ForSender(in)
		if !ok || p.Name() != want {
			t.Errorf("%q: got %v ok=%v want %s", in, p, ok, want)
		}
	}
	for _, bad := range []string{"HDFCBK.evil", "XHDFCBKX", "hdfcbank.net.evil.io", "evil-hdfcbank.net", "random", ""} {
		if p, ok := ForSender(bad); ok {
			t.Errorf("%q must not match, got %s", bad, p.Name())
		}
	}
}

func TestIndianGroupingRoundTrip(t *testing.T) {
	for _, paise := range []int64{100, 5, 49907, 12500050, 10000000_00 - 1, 99999999} {
		s := indianFormat(paise)
		got, err := ParseAmountPaise(s)
		if err != nil || got != paise {
			t.Errorf("%d -> %q -> %d err=%v", paise, s, got, err)
		}
	}
}

func TestHugeBodyIsUnknown(t *testing.T) {
	p := Registry()[0]
	if _, err := p.Parse(strings.Repeat("a", 1<<20)); err != ErrUnknownFormat {
		t.Errorf("huge body: %v", err)
	}
}

func TestUTRMustBeTwelveDigits(t *testing.T) {
	if !utrRe.MatchString("629812345678") || utrRe.MatchString("12345") {
		t.Error("UTR shape")
	}
}

// indianFormat renders paise as an Indian-grouped rupee string, for round-trip tests only.
func indianFormat(paise int64) string {
	digits := strconv.FormatInt(paise/100, 10)
	var b strings.Builder
	for i, c := range digits {
		left := len(digits) - i
		if i > 0 && (left == 3 || (left > 3 && (left-3)%2 == 0)) {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return fmt.Sprintf("%s.%02d", b.String(), paise%100)
}
