// Package parsers turns raw bank alert text (SMS, notification or email) into structured payments.
// Parsers only read credits. Anything unrecognised is ErrUnknownFormat and never confirms an order.
package parsers

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/opensawce/kaasu/internal/evidence"
)

var (
	ErrUnknownFormat = errors.New("unknown alert format")
	istZone          = time.FixedZone("IST", 5*3600+30*60)
	utrRe            = regexp.MustCompile(`^[0-9]{12}$`)
)

// MaxTextBytes bounds what a parser reads. Longer text is an unknown format.
const MaxTextBytes = 8 << 10

// Parser reads one bank's alerts.
type Parser interface {
	Name() string
	// Senders are SMS sender ids such as "AD-HDFCBK". Domains are for email.
	Senders() []*regexp.Regexp
	Domains() []string
	Parse(text string) (evidence.Parsed, error)
}

// bank is one bank's message shape. Patterns use named groups: amount, date, plus whatever extract reads.
type bank struct {
	name    string
	senders []*regexp.Regexp
	domains []string
	credit  *regexp.Regexp
	layout  string // Go time layout for the date group, read in IST
	extract func(m map[string]string) (utr, payer, account string)
}

func (b bank) Name() string              { return b.name }
func (b bank) Senders() []*regexp.Regexp { return b.senders }
func (b bank) Domains() []string         { return b.domains }

func (b bank) Parse(text string) (evidence.Parsed, error) {
	if len(text) > MaxTextBytes || strings.Contains(strings.ToLower(text), "debited") {
		return evidence.Parsed{}, ErrUnknownFormat
	}
	m := groups(b.credit, text)
	if m == nil {
		return evidence.Parsed{}, ErrUnknownFormat
	}
	paise, err := ParseAmountPaise(m["amount"])
	if err != nil {
		return evidence.Parsed{}, ErrUnknownFormat
	}
	when, err := time.ParseInLocation(b.layout, m["date"], istZone)
	if err != nil {
		return evidence.Parsed{}, ErrUnknownFormat
	}
	utr, payer, account := b.extract(m)
	if !utrRe.MatchString(utr) {
		utr = ""
	}
	return evidence.Parsed{
		Bank: b.name, AmountPaise: paise, UTR: utr, PayerMasked: payer, AccountHint: account,
		OccurredAt: when.UTC(), Parser: b.name,
	}, nil
}

func groups(re *regexp.Regexp, text string) map[string]string {
	match := re.FindStringSubmatch(text)
	if match == nil {
		return nil
	}
	out := map[string]string{}
	for i, name := range re.SubexpNames() {
		if name != "" {
			out[name] = match[i]
		}
	}
	return out
}

// maskName keeps the first initial only: RAVI KUMAR becomes R***.
func maskName(name string) string {
	f := strings.Fields(name)
	if len(f) == 0 {
		return ""
	}
	return strings.ToUpper(f[0][:1]) + "***"
}

// Registry lists every bank parser.
func Registry() []Parser {
	return []Parser{hdfc, sbi, icici, axis, kotak}
}

// ForSender finds the parser for an SMS sender id, or for an email address or domain.
// Domains match exactly or as a real subdomain, so hdfcbank.net.evil.io never matches hdfcbank.net.
func ForSender(sender string) (Parser, bool) {
	raw := strings.TrimSpace(sender)
	domain := strings.ToLower(raw)
	if at := strings.LastIndex(domain, "@"); at >= 0 {
		domain = domain[at+1:]
	}
	for _, p := range Registry() {
		for _, re := range p.Senders() {
			if re.MatchString(strings.ToUpper(raw)) {
				return p, true
			}
		}
		for _, d := range p.Domains() {
			if domain == d || strings.HasSuffix(domain, "."+d) {
				return p, true
			}
		}
	}
	return nil, false
}

// maskPayer masks a VPA the same way the evidence store does.
func maskPayer(vpa string) string { return evidence.MaskVPA(vpa) }
