// Package id makes short, prefixed, random identifiers such as ord_x7k2m9q4w8r5t1y3u6p0.
package id

import (
	"crypto/rand"
	"encoding/base32"
	"strings"
)

// New returns prefix + "_" + 20 random base32 characters (100 bits).
func New(prefix string) string {
	b := make([]byte, 13)
	if _, err := rand.Read(b); err != nil {
		panic("id: crypto/rand failed: " + err.Error()) // no safe way to continue
	}
	return prefix + "_" + strings.ToLower(base32.HexEncoding.WithPadding(base32.NoPadding).EncodeToString(b))[:20]
}
