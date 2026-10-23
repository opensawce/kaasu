package web

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

type tokens struct {
	Color  map[string]string `json:"color"`
	Status map[string]struct {
		FG, BG, Icon, Word string
	} `json:"status"`
}

func load(t *testing.T) tokens {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "assets", "tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	var tk tokens
	if err := json.Unmarshal(b, &tk); err != nil {
		t.Fatal(err)
	}
	return tk
}

func kebab(s string) string {
	return strings.ToLower(regexp.MustCompile(`([a-z])([A-Z])`).ReplaceAllString(s, "$1-$2"))
}

// Every token in tokens.json must appear in the generated CSS with the same value.
func TestGeneratedCSSMatchesTokens(t *testing.T) {
	tk := load(t)
	css, err := os.ReadFile("static/tokens.css")
	if err != nil {
		t.Fatal(err)
	}
	for name, v := range tk.Color {
		want := "--" + kebab(name) + ": " + v + ";"
		if !strings.Contains(string(css), want) {
			t.Errorf("tokens.css missing %q: run go run ./scripts/gen-tokens", want)
		}
	}
}

// The marketing site and the app must agree on colour: both read the same brand values.
func TestTokensMatchMarketingSite(t *testing.T) {
	tk := load(t)
	b, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "styles", "tokens.css"))
	if err != nil {
		t.Fatal(err)
	}
	site := map[string]string{}
	for _, m := range regexp.MustCompile(`--([a-z-]+):\s*(#[0-9A-Fa-f]{6})`).FindAllStringSubmatch(string(b), -1) {
		site[m[1]] = strings.ToUpper(m[2])
	}
	for name, v := range tk.Color {
		key := kebab(name)
		if got, ok := site[key]; ok && got != strings.ToUpper(v) {
			t.Errorf("%s: app %s, site %s", name, v, got)
		}
	}
}

// Every status shows an icon and a word, never colour alone.
func TestStatusHasIconAndWord(t *testing.T) {
	tk := load(t)
	for name, st := range tk.Status {
		if st.Icon == "" || st.Word == "" {
			t.Errorf("status %s needs both an icon and a word", name)
		}
		if _, err := os.Stat(filepath.Join("..", "..", "assets", "icons", st.Icon+".svg")); err != nil {
			t.Errorf("status %s: icon %s missing", name, st.Icon)
		}
	}
}

// Icons sit on the 24px grid, use a 2px stroke and inherit colour.
func TestIconsOnGrid(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("..", "..", "assets", "icons", "*.svg"))
	if len(files) == 0 {
		t.Fatal("no icons")
	}
	for _, f := range files {
		b, _ := os.ReadFile(f)
		s := string(b)
		for _, want := range []string{`viewBox="0 0 24 24"`, `stroke-width="2"`, `currentColor`} {
			if !strings.Contains(s, want) {
				t.Errorf("%s: missing %s", filepath.Base(f), want)
			}
		}
		if strings.Contains(s, "<script") || regexp.MustCompile(`fill="#|stroke="#`).MatchString(s) {
			t.Errorf("%s: hard-coded colour or script", filepath.Base(f))
		}
	}
}

func luminance(hex string) float64 {
	c := make([]float64, 3)
	for i := 0; i < 3; i++ {
		v, _ := strconv.ParseInt(hex[1+2*i:3+2*i], 16, 64)
		x := float64(v) / 255
		if x <= 0.03928 {
			c[i] = x / 12.92
		} else {
			c[i] = math.Pow((x+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2]
}

func contrast(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// Status text must meet WCAG AA (4.5:1) on its background.
func TestStatusContrastAA(t *testing.T) {
	tk := load(t)
	for name, st := range tk.Status {
		fg, bg := tk.Color[st.FG], tk.Color[st.BG]
		if r := contrast(fg, bg); r < 4.5 {
			t.Errorf("status %s: %s on %s is %.2f:1", name, fg, bg, r)
		}
	}
}
