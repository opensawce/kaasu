// Package email reads bank alert emails from the owner's inbox. A message counts as an alert only when its
// DKIM signature verifies on the raw bytes and the signing domain is an allow-listed bank domain.
package email

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"regexp"
	"strings"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/evidence"
	"github.com/opensawce/kaasu/internal/ledger"
	"github.com/opensawce/kaasu/internal/parsers"
	"github.com/opensawce/kaasu/internal/store"
)

// MaxBodyBytes bounds the text read from a message.
const MaxBodyBytes = 8 << 10

// RawMessage is one message exactly as the server returned it.
type RawMessage struct {
	UID string
	Raw []byte
}

// Fetcher returns messages that have not been seen yet.
type Fetcher interface {
	Fetch(ctx context.Context) ([]RawMessage, error)
}

// Verification is the outcome of one DKIM signature.
type Verification struct {
	Domain string
	Err    error
}

// Verifier checks DKIM on the raw bytes of a message.
type Verifier interface {
	Verify(raw []byte) ([]Verification, error)
}

// Matcher is the same optional hook ingest uses.
type Matcher interface {
	Process(ctx context.Context, alertID string) error
}

// Poller reads, verifies and stores bank mail.
type Poller struct {
	st      *store.Store
	clk     clock.Clock
	ev      *evidence.Service
	fetch   Fetcher
	verify  Verifier
	matcher Matcher
}

func NewPoller(st *store.Store, clk clock.Clock, ev *evidence.Service, f Fetcher, v Verifier, m Matcher) *Poller {
	return &Poller{st: st, clk: clk, ev: ev, fetch: f, verify: v, matcher: m}
}

// Once polls the inbox a single time. It returns how many alerts were accepted and how many were rejected.
func (p *Poller) Once(ctx context.Context) (accepted, rejected int, err error) {
	msgs, err := p.fetch.Fetch(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("fetch mail: %w", err)
	}
	for _, m := range msgs {
		ok, err := p.handle(ctx, m.Raw)
		if err != nil {
			return accepted, rejected, err
		}
		if ok {
			accepted++
		} else {
			rejected++
		}
	}
	return accepted, rejected, nil
}

var (
	errNoDKIM     = errors.New("no_dkim")
	errBadDKIM    = errors.New("bad_dkim")
	errNotAllowed = errors.New("domain_not_allowed")
	errNotAligned = errors.New("from_not_aligned")
)

// handle returns true when the message became (or already was) evidence for an alert.
func (p *Poller) handle(ctx context.Context, raw []byte) (bool, error) {
	domain, reason := p.trustedDomain(raw)
	if reason != nil {
		return false, p.reject(ctx, reason.Error())
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return false, p.reject(ctx, "parse")
	}
	parser, ok := parsers.ForSender(domain)
	if !ok {
		return false, p.reject(ctx, errNotAllowed.Error())
	}
	text, err := bodyText(msg.Header.Get("Content-Type"), msg.Body)
	if err != nil {
		return false, p.reject(ctx, "parse")
	}
	parsed, perr := parser.Parse(text)
	var alertID string
	err = p.st.Tx(ctx, func(tx *sql.Tx) error {
		evID, dup, err := p.ev.SaveEvidence(ctx, tx, "email", "", raw)
		if err != nil || dup {
			return err
		}
		if perr != nil {
			_, err := ledger.Append(ctx, tx, p.clk, ledger.Entry{Kind: "alert.unparsed", Payload: map[string]any{"evidence_id": evID, "parser": parser.Name()}})
			return err
		}
		alertID, _, err = p.ev.SaveAlert(ctx, tx, evID, parsed)
		return err
	})
	if err != nil {
		return false, err
	}
	if alertID != "" && p.matcher != nil {
		if err := p.matcher.Process(ctx, alertID); err != nil {
			return false, err
		}
	}
	return true, nil
}

// trustedDomain returns the DKIM domain that passed and is aligned with the From header.
func (p *Poller) trustedDomain(raw []byte) (string, error) {
	vs, err := p.verify.Verify(raw)
	if err != nil || len(vs) == 0 {
		return "", errNoDKIM
	}
	var sawFail bool
	for _, v := range vs {
		if v.Err != nil {
			sawFail = true
			continue
		}
		if !aligned(raw, v.Domain) {
			return "", errNotAligned
		}
		return v.Domain, nil
	}
	if sawFail {
		return "", errBadDKIM
	}
	return "", errNoDKIM
}

func aligned(raw []byte, domain string) bool {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return false
	}
	addr, err := mail.ParseAddress(msg.Header.Get("From"))
	if err != nil {
		return false
	}
	at := strings.LastIndex(addr.Address, "@")
	if at < 0 {
		return false
	}
	from := strings.ToLower(addr.Address[at+1:])
	d := strings.ToLower(domain)
	return from == d || strings.HasSuffix(from, "."+d)
}

func (p *Poller) reject(ctx context.Context, reason string) error {
	return p.st.Tx(ctx, func(tx *sql.Tx) error {
		_, err := ledger.Append(ctx, tx, p.clk, ledger.Entry{Kind: "email.rejected", Payload: map[string]any{"reason": reason}})
		return err
	})
}

// bodyText prefers text/plain, falls back to text/html with tags removed, and never reads past MaxBodyBytes.
func bodyText(contentType string, body io.Reader) (string, error) {
	mt, params, err := mime.ParseMediaType(contentType)
	if err != nil || mt == "" {
		mt = "text/plain"
	}
	if strings.HasPrefix(mt, "multipart/") {
		plain, html := "", ""
		mr := multipart.NewReader(body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			t, _ := bodyTextPart(part)
			ct, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
			if ct == "text/plain" && plain == "" {
				plain = t
			} else if ct == "text/html" && html == "" {
				html = stripTags(t)
			}
		}
		if plain != "" {
			return plain, nil
		}
		return html, nil
	}
	b, err := io.ReadAll(io.LimitReader(body, MaxBodyBytes))
	if err != nil {
		return "", err
	}
	if mt == "text/html" {
		return stripTags(string(b)), nil
	}
	return string(b), nil
}

func bodyTextPart(part io.Reader) (string, error) {
	b, err := io.ReadAll(io.LimitReader(part, MaxBodyBytes))
	return string(b), err
}

var tagRe = regexp.MustCompile(`<[^>]*>`)

func stripTags(s string) string { return strings.TrimSpace(tagRe.ReplaceAllString(s, " ")) }
