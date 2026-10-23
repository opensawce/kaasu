package upi

import (
	"net/url"
	"strings"
	"testing"
)

func TestFormatPaise(t *testing.T) {
	tests := map[int64]string{5: "0.05", 100: "1.00", 49907: "499.07", 120000: "1200.00", 10000000: "100000.00", 99: "0.99", 1000001: "10000.01"}
	for in, want := range tests {
		if got := FormatPaise(in); got != want {
			t.Errorf("FormatPaise(%d)=%q want %q", in, got, want)
		}
	}
}

func TestLinkUsesOnlyPersonalVPAFields(t *testing.T) {
	l := Link("sharma.sweets@okhdfcbank", "Sharma Sweets", 49907, "Order 1042")
	want := "upi://pay?pa=sharma.sweets%40okhdfcbank&pn=Sharma%20Sweets&am=499.07&cu=INR&tn=Order%201042"
	if l != want {
		t.Fatalf("got  %s\nwant %s", l, want)
	}
	u, err := url.Parse(l)
	if err != nil {
		t.Fatal(err)
	}
	for k := range u.Query() {
		switch k {
		case "pa", "pn", "am", "cu", "tn":
		default:
			t.Errorf("forbidden field %q in link", k)
		}
	}
	for _, bad := range []string{"mc=", "tr=", "sign=", "mode="} {
		if strings.Contains(l, bad) {
			t.Errorf("link contains %s", bad)
		}
	}
}

func TestLinkOmitsEmptyNoteAndTrimsLongOnes(t *testing.T) {
	if l := Link("a@okbank", "A", 100, ""); strings.Contains(l, "tn=") {
		t.Errorf("empty note should be omitted: %s", l)
	}
	long := strings.Repeat("ऑ", 80) // multi-byte runes
	l := Link("a@okbank", "A", 100, long)
	u, _ := url.Parse(l)
	if n := len([]rune(u.Query().Get("tn"))); n != 50 {
		t.Errorf("note should be cut to 50 runes, got %d", n)
	}
}

func TestLinkEscapesInjection(t *testing.T) {
	l := Link("a@okbank", "Shop&am=1", 100, "x&pa=evil@bank")
	u, _ := url.Parse(l)
	q := u.Query()
	if q.Get("pa") != "a@okbank" || q.Get("am") != "1.00" || q.Get("pn") != "Shop&am=1" || q.Get("tn") != "x&pa=evil@bank" {
		t.Errorf("fields were not isolated: %v", q)
	}
}
