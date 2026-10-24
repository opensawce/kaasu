// Package i18n holds every user-facing string. Templates and code ask for keys; they never hard-code copy.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

//go:embed *.json
var files embed.FS

// Lang is a language code.
type Lang string

// Languages are the eleven supported languages, English first.
var Languages = []Lang{"en", "hi", "ta", "te", "kn", "ml", "mr", "bn", "gu", "pa", "or"}

var native = map[Lang]string{
	"en": "English", "hi": "हिन्दी", "ta": "தமிழ்", "te": "తెలుగు", "kn": "ಕನ್ನಡ", "ml": "മലയാളം",
	"mr": "मराठी", "bn": "বাংলা", "gu": "ગુજરાતી", "pa": "ਪੰਜਾਬੀ", "or": "ଓଡ଼ିଆ",
}

// Native returns the language's name in its own script.
func (l Lang) Native() string {
	if n, ok := native[l]; ok {
		return n
	}
	return string(l)
}

var catalogue = map[Lang]map[string]string{}

func init() {
	for _, l := range Languages {
		b, err := files.ReadFile(string(l) + ".json")
		if err != nil {
			panic("i18n: missing catalogue for " + string(l))
		}
		m := map[string]string{}
		if err := json.Unmarshal(b, &m); err != nil {
			panic("i18n: " + string(l) + ": " + err.Error())
		}
		catalogue[l] = m
	}
}

var placeholder = regexp.MustCompile(`\{([a-z_]+)\}`)

// T returns the text for key in l, falling back to English, then to the key itself.
// args are name/value pairs: T(l, "pay.button", "amount", "₹499.07").
func T(l Lang, key string, args ...string) string {
	s, ok := catalogue[l][key]
	if !ok {
		s, ok = catalogue["en"][key]
	}
	if !ok {
		return key
	}
	for i := 0; i+1 < len(args); i += 2 {
		s = strings.ReplaceAll(s, "{"+args[i]+"}", args[i+1])
	}
	return s
}

// Rupees formats paise with Indian grouping and two decimals, with Latin digits: 12486050 -> "₹1,24,860.50".
// Negative amounts are not valid money here and render as zero.
func Rupees(paise int64, _ Lang) string {
	if paise < 0 {
		paise = 0
	}
	digits := strconv.FormatInt(paise/100, 10)
	var b strings.Builder
	for i, c := range digits {
		left := len(digits) - i
		if i > 0 && (left == 3 || (left > 3 && (left-3)%2 == 0)) {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return fmt.Sprintf("₹%s.%02d", b.String(), paise%100)
}

// Spoken is the text read aloud for an amount: whole rupees when the paise are zero, otherwise rupees and paise.
func Spoken(paise int64, l Lang) string {
	amount := Rupees(paise, l)
	if paise%100 == 0 {
		amount = strings.TrimSuffix(amount, ".00")
	}
	return T(l, "voice.received", "amount", amount)
}

// FromAcceptLanguage picks the first supported language from an Accept-Language header, else the shop default.
func FromAcceptLanguage(header string, shopDefault Lang) Lang {
	type cand struct {
		l Lang
		q float64
	}
	var cs []cand
	for _, part := range strings.Split(header, ",") {
		tag, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		q := 1.0
		if params != "" {
			v, err := strconv.ParseFloat(strings.TrimPrefix(strings.TrimSpace(params), "q="), 64)
			if err != nil {
				continue // a malformed weight is not a usable candidate
			}
			q = v
		}
		if math.IsNaN(q) || q <= 0 || q > 1 {
			continue
		}
		base := strings.ToLower(strings.Split(tag, "-")[0])
		for _, l := range Languages {
			if string(l) == base {
				cs = append(cs, cand{l, q})
			}
		}
	}
	if len(cs) == 0 {
		return shopDefault
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].q > cs[j].q })
	return cs[0].l
}

// Keys lists the English keys, sorted.
func Keys() []string {
	out := make([]string, 0, len(catalogue["en"]))
	for k := range catalogue["en"] {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Placeholders returns the sorted {name} placeholders in a string.
func Placeholders(s string) []string {
	var out []string
	for _, m := range placeholder.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	sort.Strings(out)
	return out
}

// Catalogue returns the raw map for one language (for tests and the review list).
func Catalogue(l Lang) map[string]string { return catalogue[l] }

// LintTemplates reports text nodes in templates that are not wrapped in a translation call. Empty dir passes.
func LintTemplates(dir string) ([]string, error) {
	var bad []string
	files, _ := filepath.Glob(filepath.Join(dir, "*.html"))
	text := regexp.MustCompile(`>\s*[^<{}\s][^<]*[A-Za-z]{2,}[^<]*<`)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		for _, m := range text.FindAllString(string(b), -1) {
			bad = append(bad, filepath.Base(f)+": "+strings.TrimSpace(m))
		}
	}
	return bad, nil
}
