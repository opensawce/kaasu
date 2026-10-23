package parsers

import (
	"errors"
	"strings"
)

// maxPaise is ₹1 crore, far above any single UPI payment. Anything bigger is treated as a bad parse.
const maxPaise int64 = 10_000_000_00

var ErrBadAmount = errors.New("bad amount")

// ParseAmountPaise reads an Indian-formatted amount such as "1,25,000.50" as integer paise. Floats are never used.
func ParseAmountPaise(s string) (int64, error) {
	r := strings.NewReplacer("Rs.", "", "Rs", "", "INR", "", "₹", "", " ", "", ",", "")
	t := r.Replace(s)
	whole, frac, hasDot := strings.Cut(t, ".")
	if whole == "" || !allDigits(whole) {
		return 0, ErrBadAmount
	}
	if hasDot {
		if len(frac) == 0 || len(frac) > 2 || !allDigits(frac) {
			return 0, ErrBadAmount
		}
		for len(frac) < 2 {
			frac += "0"
		}
	} else {
		frac = "00"
	}
	var rupees int64
	for _, c := range whole {
		rupees = rupees*10 + int64(c-'0')
		if rupees > maxPaise/100 {
			return 0, ErrBadAmount
		}
	}
	paise := rupees*100 + int64((frac[0]-'0')*10+(frac[1]-'0'))
	if paise <= 0 || paise > maxPaise {
		return 0, ErrBadAmount
	}
	return paise, nil
}

func allDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}
