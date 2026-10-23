// Package links makes payment links (one page per shop, with an amount) and counter links (the shop enters the amount).
// Every visit creates or reuses an order through the same path as the API, so the matcher treats them identically.
package links

import (
	"context"
	"database/sql"
	"errors"
	"html"
	"net/url"
	"strings"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/orders"
	"github.com/opensawce/kaasu/internal/parsers"
	"github.com/opensawce/kaasu/internal/store"
)

var (
	ErrGone    = errors.New("this link is no longer available")
	ErrInvalid = errors.New("invalid link")
)

// Link is a payment link or a counter link.
type Link struct {
	ID, AccountID, Kind, Note string
	AmountPaise               int64
	ExpiresAt                 string
}

// Input creates a link.
type Input struct {
	AccountID   string
	Kind        string // link | counter
	AmountPaise int64  // required for link, zero for counter
	Note        string
	ExpiresIn   time.Duration
}

type IDSource interface{ New(prefix string) string }

type Service struct {
	st     *store.Store
	clk    clock.Clock
	ids    IDSource
	orders *orders.Service
}

func New(st *store.Store, clk clock.Clock, ids IDSource, o *orders.Service) *Service {
	return &Service{st: st, clk: clk, ids: ids, orders: o}
}

// Create stores a link. A payment link needs an amount; a counter link takes its amount at the counter.
func (s *Service) Create(ctx context.Context, in Input) (Link, error) {
	switch in.Kind {
	case "link":
		if in.AmountPaise < 100 || in.AmountPaise > 10_000_000 {
			return Link{}, ErrInvalid
		}
	case "counter":
		if in.AmountPaise != 0 {
			return Link{}, ErrInvalid
		}
	default:
		return Link{}, ErrInvalid
	}
	now := s.clk.Now().UTC()
	l := Link{ID: s.ids.New("lnk"), AccountID: in.AccountID, Kind: in.Kind, AmountPaise: in.AmountPaise, Note: in.Note}
	var exp any
	if in.ExpiresIn > 0 {
		l.ExpiresAt = now.Add(in.ExpiresIn).Format(time.RFC3339)
		exp = l.ExpiresAt
	}
	err := s.st.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO payment_links(id, account_id, kind, amount_paise, note, expires_at, created_at) VALUES(?,?,?,?,?,?,?)`,
			l.ID, l.AccountID, l.Kind, nullInt(l.AmountPaise), nullStr(l.Note), exp, now.Format(time.RFC3339))
		return err
	})
	return l, err
}

func (s *Service) load(ctx context.Context, id string) (Link, error) {
	var l Link
	var amount sql.NullInt64
	var note, exp, disabled sql.NullString
	err := s.st.Read().QueryRowContext(ctx, `SELECT id, account_id, kind, amount_paise, note, expires_at, disabled_at FROM payment_links WHERE id = ?`, id).
		Scan(&l.ID, &l.AccountID, &l.Kind, &amount, &note, &exp, &disabled)
	if errors.Is(err, sql.ErrNoRows) {
		return l, ErrGone
	}
	if err != nil {
		return l, err
	}
	if disabled.Valid {
		return l, ErrGone
	}
	if exp.Valid {
		t, _ := time.Parse(time.RFC3339, exp.String)
		if !s.clk.Now().Before(t) {
			return l, ErrGone
		}
		l.ExpiresAt = exp.String
	}
	l.AmountPaise = amount.Int64
	l.Note = note.String
	return l, nil
}

// Open is a visit to a payment link. The same visitor gets the same order on refresh.
func (s *Service) Open(ctx context.Context, id, visitor string) (orders.Order, error) {
	l, err := s.load(ctx, id)
	if err != nil {
		return orders.Order{}, err
	}
	if l.Kind != "link" {
		return orders.Order{}, ErrInvalid
	}
	return s.order(ctx, l, l.AmountPaise, visitor)
}

// OpenCounter is a counter visit once the shop has entered the amount.
func (s *Service) OpenCounter(ctx context.Context, id string, amount int64, visitor string) (orders.Order, error) {
	l, err := s.load(ctx, id)
	if err != nil {
		return orders.Order{}, err
	}
	if l.Kind != "counter" {
		return orders.Order{}, ErrInvalid
	}
	return s.order(ctx, l, amount, visitor)
}

func (s *Service) order(ctx context.Context, l Link, amount int64, visitor string) (orders.Order, error) {
	key := "link:" + l.ID + ":" + visitor + ":" + itoa(amount)
	o, _, err := s.orders.Create(ctx, orders.CreateInput{AmountPaise: amount, AccountID: l.AccountID, Note: l.Note, Reference: "link:" + l.ID}, key)
	return o, err
}

// Disable stops a link from creating more orders.
func (s *Service) Disable(ctx context.Context, id string) error {
	return s.st.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE payment_links SET disabled_at = ? WHERE id = ? AND disabled_at IS NULL`, s.clk.Now().UTC().Format(time.RFC3339), id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrGone
		}
		return nil
	})
}

// ParseCounterAmount reads what the shop types at the counter: rupees with optional decimals, at least ₹1, at most ₹1,00,000.
func ParseCounterAmount(s string) (int64, error) {
	paise, err := parsers.ParseAmountPaise(strings.TrimSpace(s))
	if err != nil {
		return 0, ErrInvalid
	}
	if paise < 100 || paise > 10_000_000 {
		return 0, ErrInvalid
	}
	return paise, nil
}

// WhatsAppURL builds a share link carrying text and the payment link.
func WhatsAppURL(link, text string) string {
	body := url.QueryEscape(text + "\n" + link)
	return "https://wa.me/?text=" + strings.ReplaceAll(body, "+", "%20")
}

// Standee is the printable A6 counter sign. It loads nothing from outside.
func Standee(shop, link string) string {
	return `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>` + html.EscapeString(shop) + `</title><style>
@page { size: 105mm 148mm; margin: 8mm; }
body { font: 16px/1.5 system-ui, sans-serif; text-align: center; color: #14171A; }
.shop { font-size: 22px; font-weight: 700; }
.lead { margin-top: 12px; }
</style></head><body>
<p class="shop">` + html.EscapeString(shop) + `</p>
<p class="lead">Scan, enter the amount, pay with any UPI app</p>
<p><a href="` + html.EscapeString(link) + `">` + html.EscapeString(link) + `</a></p>
</body></html>`
}

func nullInt(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}
