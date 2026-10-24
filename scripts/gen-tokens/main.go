// Command gen-tokens writes the generated token files from assets/tokens.json. Run: go run ./scripts/gen-tokens
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Tokens struct {
	Color  map[string]string `json:"color"`
	Status map[string]struct {
		FG   string `json:"fg"`
		BG   string `json:"bg"`
		Icon string `json:"icon"`
		Word string `json:"word"`
	} `json:"status"`
	Space  []int          `json:"space"`
	Radius map[string]int `json:"radius"`
	Touch  int            `json:"touch"`
}

// CSS renders the custom properties. Used by the generator and by the test that checks the file is current.
func CSS(t Tokens) string {
	var b strings.Builder
	b.WriteString("/* Code generated from assets/tokens.json; DO NOT EDIT. */\n:root {\n")
	keys := sortedKeys(t.Color)
	for _, k := range keys {
		fmt.Fprintf(&b, "  --%s: %s;\n", kebab(k), t.Color[k])
	}
	statuses := make([]string, 0, len(t.Status))
	for k := range t.Status {
		statuses = append(statuses, k)
	}
	sort.Strings(statuses)
	for _, s := range statuses {
		st := t.Status[s]
		fmt.Fprintf(&b, "  --status-%s-fg: %s;\n  --status-%s-bg: %s;\n", kebab(s), t.Color[st.FG], kebab(s), t.Color[st.BG])
	}
	for i, v := range t.Space {
		fmt.Fprintf(&b, "  --space-%d: %dpx;\n", i, v)
	}
	for _, k := range sortedKeys(t.Radius) {
		fmt.Fprintf(&b, "  --radius-%s: %dpx;\n", k, t.Radius[k])
	}
	fmt.Fprintf(&b, "  --touch: %dpx;\n}\n", t.Touch)
	return b.String()
}

func kebab(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r + ('a' - 'A'))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	raw, err := os.ReadFile(filepath.Join(root, "assets", "tokens.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var t Tokens
	if err := json.Unmarshal(raw, &t); err != nil {
		fmt.Fprintln(os.Stderr, "tokens.json:", err)
		os.Exit(1)
	}
	out := filepath.Join(root, "internal", "web", "static", "tokens.css")
	if err := os.WriteFile(out, []byte(CSS(t)), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("wrote", out)
}
