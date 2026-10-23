package i18n

import (
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestEveryLanguageHasEveryKey(t *testing.T) {
	en := Catalogue("en")
	for _, l := range Languages {
		for k := range en {
			if _, ok := Catalogue(l)[k]; !ok {
				t.Errorf("%s is missing %s", l, k)
			}
		}
		if len(Catalogue(l)) != len(en) {
			t.Errorf("%s has %d keys, English has %d", l, len(Catalogue(l)), len(en))
		}
	}
}

func TestPlaceholdersMatchEnglish(t *testing.T) {
	en := Catalogue("en")
	for _, l := range Languages {
		for k, v := range Catalogue(l) {
			if strings.Join(Placeholders(v), ",") != strings.Join(Placeholders(en[k]), ",") {
				t.Errorf("%s %s: placeholders %v differ from English %v", l, k, Placeholders(v), Placeholders(en[k]))
			}
		}
	}
}

func TestRupeesTable(t *testing.T) {
	tests := map[int64]string{
		0: "₹0.00", 5: "₹0.05", 99: "₹0.99", 100: "₹1.00", 49907: "₹499.07", 12486050: "₹1,24,860.50",
		10000000_00: "₹1,00,00,000.00", 100000: "₹1,000.00", -5: "₹0.00",
	}
	for in, want := range tests {
		if got := Rupees(in, "en"); got != want {
			t.Errorf("Rupees(%d)=%q want %q", in, got, want)
		}
	}
}

// Property: removing the grouping commas gives back the plain amount.
func TestIndianGroupingProperty(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 2000; i++ {
		paise := r.Int63n(10000000_00) + 1
		got := Rupees(paise, "hi")
		plain := strings.ReplaceAll(strings.TrimPrefix(got, "₹"), ",", "")
		want := strconv.FormatInt(paise/100, 10) + "." + pad2(paise%100)
		if plain != want {
			t.Fatalf("%d -> %q", paise, got)
		}
	}
}

func pad2(v int64) string {
	if v < 10 {
		return "0" + strconv.FormatInt(v, 10)
	}
	return strconv.FormatInt(v, 10)
}

func TestDigitsAreLatin(t *testing.T) {
	latin := regexp.MustCompile(`^[0-9,.₹]+$`)
	for _, l := range Languages {
		if got := Rupees(12486050, l); !latin.MatchString(got) {
			t.Errorf("%s: %q has non-Latin digits or symbols", l, got)
		}
	}
}

func TestFromAcceptLanguage(t *testing.T) {
	tests := []struct{ header, want string }{
		{"hi-IN,hi;q=0.9,en;q=0.8", "hi"},
		{"xx", "en"},
		{"", "en"},
		{"ta;q=0, en;q=0.5", "en"},
		{"fr-FR,ta;q=0.7", "ta"},
	}
	for _, tt := range tests {
		if got := FromAcceptLanguage(tt.header, "en"); string(got) != tt.want {
			t.Errorf("%q: got %s want %s", tt.header, got, tt.want)
		}
	}
	if got := FromAcceptLanguage("xx", "ta"); got != "ta" {
		t.Errorf("shop default: %s", got)
	}
}

func TestTFallsBackAndFillsArgs(t *testing.T) {
	if got := T("hi", "pay.button", "amount", "₹499.07"); !strings.Contains(got, "₹499.07") {
		t.Errorf("args: %q", got)
	}
	if got := T("hi", "no.such.key"); got != "no.such.key" {
		t.Errorf("unknown key should return the key: %q", got)
	}
}

func TestSpokenAmounts(t *testing.T) {
	if got := Spoken(49900, "en"); got != "₹499 received" {
		t.Errorf("whole rupees: %q", got)
	}
	if got := Spoken(49907, "en"); got != "₹499.07 received" {
		t.Errorf("with paise: %q", got)
	}
}

func TestLintTemplatesEmptyDirPasses(t *testing.T) {
	bad, err := LintTemplates(t.TempDir())
	if err != nil || len(bad) != 0 {
		t.Errorf("bad=%v err=%v", bad, err)
	}
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "x.html"), []byte("<p>Hello there</p>"), 0o644)
	if bad, _ := LintTemplates(dir); len(bad) != 1 {
		t.Errorf("raw text should be flagged: %v", bad)
	}
}
