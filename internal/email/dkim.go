package email

import (
	"bytes"

	"github.com/emersion/go-msgauth/dkim"
)

// DKIMVerifier checks signatures with DNS lookups. Tests inject LookupTXT.
type DKIMVerifier struct {
	LookupTXT func(domain string) ([]string, error)
}

// Verify returns one entry per signature. A message with no signatures returns an empty list.
func (v DKIMVerifier) Verify(raw []byte) ([]Verification, error) {
	opts := &dkim.VerifyOptions{LookupTXT: v.LookupTXT, MaxVerifications: 5}
	results, err := dkim.VerifyWithOptions(bytes.NewReader(raw), opts)
	if err != nil {
		return nil, err
	}
	out := make([]Verification, 0, len(results))
	for _, r := range results {
		out = append(out, Verification{Domain: r.Domain, Err: r.Err})
	}
	return out, nil
}
