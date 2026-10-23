package id

import (
	"regexp"
	"testing"
)

func TestNewFormatAndUniqueness(t *testing.T) {
	re := regexp.MustCompile(`^ord_[0-9a-z]{20}$`)
	seen := map[string]bool{}
	for i := 0; i < 10000; i++ {
		v := New("ord")
		if !re.MatchString(v) {
			t.Fatalf("bad id %q", v)
		}
		if seen[v] {
			t.Fatalf("duplicate id %q", v)
		}
		seen[v] = true
	}
}
