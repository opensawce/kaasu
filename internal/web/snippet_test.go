package web

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestSnippetSizeUnder3KB(t *testing.T) {
	b, err := os.ReadFile("static/kaasu.js")
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > 3*1024 {
		t.Errorf("snippet is %d bytes, limit is 3 KB", len(b))
	}
}

func TestSnippetHasNoInlineHandlersOrExternalURLs(t *testing.T) {
	b, _ := os.ReadFile("static/kaasu.js")
	s := string(b)
	if regexp.MustCompile(`https?://`).MatchString(s) {
		t.Error("snippet must not reference external URLs")
	}
	if strings.Contains(s, "eval(") || strings.Contains(s, "innerHTML") {
		t.Error("snippet must not use eval or innerHTML")
	}
}

// Messages are accepted only from the pay page's own origin.
func TestSnippetChecksMessageOrigin(t *testing.T) {
	b, _ := os.ReadFile("static/kaasu.js")
	if !strings.Contains(string(b), "e.origin !== origin") {
		t.Error("postMessage handler must check the origin")
	}
}
