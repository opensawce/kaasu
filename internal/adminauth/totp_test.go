package adminauth

import (
	"testing"
	"time"
)

// RFC 6238 appendix B, SHA-1, 8 digits, secret "12345678901234567890".
func TestRFC6238Vectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	vectors := map[int64]string{
		59:          "94287082",
		1111111109:  "07081804",
		1111111111:  "14050471",
		1234567890:  "89005924",
		2000000000:  "69279037",
		20000000000: "65353130",
	}
	for unix, want := range vectors {
		got := code(secret, time.Unix(unix, 0), 8)
		if got != want {
			t.Errorf("t=%d: got %s want %s", unix, got, want)
		}
	}
}

func TestVerifySkewAndReplay(t *testing.T) {
	secret := []byte("12345678901234567890")
	now := time.Unix(1111111109, 0)
	cur := code(secret, now, 6)
	step, ok := Verify(secret, cur, now, -1)
	if !ok {
		t.Fatal("current code must verify")
	}
	if _, ok := Verify(secret, cur, now, step); ok {
		t.Error("replayed code in the same step must be refused")
	}
	prev := code(secret, now.Add(-30*time.Second), 6)
	if _, ok := Verify(secret, prev, now, -1); !ok {
		t.Error("one step of skew should be accepted")
	}
	far := code(secret, now.Add(-90*time.Second), 6)
	if _, ok := Verify(secret, far, now, -1); ok {
		t.Error("codes outside one step must fail")
	}
	if _, ok := Verify(secret, "12345", now, -1); ok {
		t.Error("wrong length must fail")
	}
}
