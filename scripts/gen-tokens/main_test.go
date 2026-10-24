package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The checked-in stylesheet must be exactly what the generator renders from tokens.json, for every token category.
func TestTokensCSSIsCurrent(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "assets", "tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	var tk Tokens
	if err := json.Unmarshal(raw, &tk); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join("..", "..", "internal", "web", "static", "tokens.css"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != CSS(tk) {
		t.Fatal("internal/web/static/tokens.css is stale: run go run ./scripts/gen-tokens")
	}
}
