// Package adminauth signs owners and staff in to the admin listener. TOTP is RFC 6238, written with the
// standard library. Passkeys (WebAuthn) are not in this change; see the PR notes.
package adminauth

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

const step = 30 * time.Second

// code returns the TOTP for t with the given number of digits.
func code(secret []byte, t time.Time, digits int) string {
	counter := uint64(t.Unix() / int64(step/time.Second))
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	mod := uint32(1)
	for i := 0; i < digits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, v%mod)
}

// Verify accepts the code for the current step and one step either side. It returns the matched step.
// A step at or before lastStep is refused, so a code works once.
func Verify(secret []byte, input string, now time.Time, lastStep int64) (int64, bool) {
	input = strings.TrimSpace(input)
	if len(input) != 6 {
		return 0, false
	}
	cur := now.Unix() / int64(step/time.Second)
	for _, s := range []int64{cur - 1, cur, cur + 1} {
		t := time.Unix(s*int64(step/time.Second), 0)
		if hmac.Equal([]byte(code(secret, t, 6)), []byte(input)) {
			if s <= lastStep {
				return 0, false
			}
			return s, true
		}
	}
	return 0, false
}
