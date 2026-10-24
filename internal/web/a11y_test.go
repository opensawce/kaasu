package web

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// These checks read the templates and stylesheets directly. They are deliberately simple: they catch the
// regressions that matter most, and a browser audit (card 40) covers the rest.

func templates(t *testing.T) map[string]string {
	t.Helper()
	files, _ := filepath.Glob("templates/*.html")
	out := map[string]string{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		out[filepath.Base(f)] = string(b)
	}
	if len(out) == 0 {
		t.Fatal("no templates")
	}
	return out
}

func TestLangAttributeAndTitle(t *testing.T) {
	for name, s := range templates(t) {
		if !strings.Contains(s, `<html lang="`) {
			t.Errorf("%s: missing lang attribute", name)
		}
		if !strings.Contains(s, "<title>") {
			t.Errorf("%s: missing title", name)
		}
	}
}

func TestExactlyOneH1PerPage(t *testing.T) {
	for name, s := range templates(t) {
		if n := strings.Count(s, "<h1"); n != 1 {
			t.Errorf("%s: %d h1 elements, want 1", name, n)
		}
	}
}

func TestImagesHaveAlt(t *testing.T) {
	img := regexp.MustCompile(`<img\b[^>]*>`)
	for name, s := range templates(t) {
		for _, tag := range img.FindAllString(s, -1) {
			if !strings.Contains(tag, "alt=") {
				t.Errorf("%s: image without alt: %s", name, tag)
			}
		}
	}
}

var svgTag = regexp.MustCompile(`<svg\b[^>]*>`)

func TestSvgIconsAreHidden(t *testing.T) {
	for name, s := range templates(t) {
		for _, tag := range svgTag.FindAllString(s, -1) {
			if !strings.Contains(tag, `aria-hidden="true"`) {
				t.Errorf("%s: decorative svg must be aria-hidden: %s", name, tag)
			}
		}
	}
}

func TestStatusHasWordNotColourAlone(t *testing.T) {
	pill := regexp.MustCompile(`class="pill"[^>]*>([^<]*)<`)
	for name, s := range templates(t) {
		for _, m := range pill.FindAllStringSubmatch(s, -1) {
			if strings.TrimSpace(m[1]) == "" {
				t.Errorf("%s: a status pill without its word", name)
			}
		}
	}
}

func TestFocusVisibleAndTouchTargets(t *testing.T) {
	b, err := os.ReadFile("static/pay.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(b)
	if regexp.MustCompile(`outline:\s*none`).MatchString(css) {
		t.Error("removing outlines needs a visible replacement; none found")
	}
	focus := regexp.MustCompile(`\.btn:focus-visible\s*\{([^}]*)\}`).FindStringSubmatch(css)
	if focus == nil || !regexp.MustCompile(`outline:\s*[1-9][0-9]*px\s+solid`).MatchString(focus[1]) {
		t.Error(".btn:focus-visible must draw a visible focus ring")
	}
	if !strings.Contains(css, "min-height: var(--touch)") {
		t.Error("buttons must be at least the 44 px touch size")
	}
}

func TestReducedMotionOverridesAnimation(t *testing.T) {
	for _, f := range []string{"static/pay.css", "static/done.css"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		if (strings.Contains(s, "animation:") || strings.Contains(s, "transition:")) && !strings.Contains(s, "prefers-reduced-motion") {
			t.Errorf("%s: animation without a reduced-motion override", f)
		}
	}
}
