package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/opensawce/kaasu/i18n"
	"github.com/opensawce/kaasu/internal/orders"
	"github.com/opensawce/kaasu/internal/store"
)

var doneTmpl = template.Must(template.ParseFS(assets, "templates/done.html"))

// Confirmed serves the success page and the receipt. Both appear only for a real confirmation.
type Confirmed struct {
	Orders   *orders.Service
	Store    *store.Store
	ShopName string
	ShopLang i18n.Lang
	secret   []byte
	now      func() time.Time
}

// NewConfirmed makes the page. The verification key is random per process unless the caller supplies one.
func NewConfirmed(o *orders.Service, st *store.Store, shop string, lang i18n.Lang, now func() time.Time) *Confirmed {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	return &Confirmed{Orders: o, Store: st, ShopName: shop, ShopLang: lang, secret: key, now: now}
}

// Code is a short check the shopkeeper can compare with their own app: it binds the order, the time and the amount.
func (c *Confirmed) Code(orderID, confirmedAt string, amount int64) string {
	m := hmac.New(sha256.New, c.secret)
	fmt.Fprintf(m, "%s|%s|%d", orderID, confirmedAt, amount)
	return strings.ToUpper(hex.EncodeToString(m.Sum(nil))[:6])
}

// Register adds the success and receipt routes to m.
func (c *Confirmed) Register(m *http.ServeMux) {
	m.HandleFunc("GET /pay/{id}/done", c.done)
	m.HandleFunc("GET /pay/{id}/receipt", c.receipt)
}

// Handler is the success and receipt pages on their own mux.
func (c *Confirmed) Handler() http.Handler {
	m := http.NewServeMux()
	c.Register(m)
	return m
}

type doneData struct {
	Lang, Title, Title2, Amount, Ago, Shop, UTR, ConfirmedAt, Reference, Code, OrderID string
	PaidTo, UTRLabel, TimeLabel, RefLabel, CodeLabel, SaveReceipt                      string
	ConfirmedUnix                                                                      int64
}

func (c *Confirmed) confirmed(r *http.Request) (orders.Order, string, error) {
	o, err := c.Orders.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return o, "", err
	}
	var utr string
	_ = c.Store.Read().QueryRowContext(r.Context(),
		`SELECT COALESCE(a.utr,'') FROM matches m JOIN alerts a ON a.id = m.alert_id WHERE m.order_id = ? AND m.decision = 'auto' LIMIT 1`, o.ID).Scan(&utr)
	return o, utr, nil
}

func (c *Confirmed) done(w http.ResponseWriter, r *http.Request) {
	o, utr, err := c.confirmed(r)
	if errors.Is(err, orders.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}
	// Only a real confirmation shows success. Anything else goes back to the pay page; never render success from the URL.
	if o.State != "confirmed" && o.State != "late_paid" {
		http.Redirect(w, r, "/pay/"+o.ID, http.StatusSeeOther)
		return
	}
	l := c.lang(r)
	at, _ := time.Parse(time.RFC3339, o.ConfirmedAt)
	d := doneData{
		Lang: string(l), Title: c.ShopName, Title2: i18n.T(l, "confirmed.title"), Amount: i18n.Rupees(o.AmountPaise, l),
		Ago: "0 s", Shop: c.ShopName, UTR: utr, ConfirmedAt: at.In(istZone).Format("02 Jan 2006, 15:04"),
		Reference: o.Reference, Code: c.Code(o.ID, o.ConfirmedAt, o.AmountPaise), OrderID: o.ID,
		PaidTo: i18n.T(l, "confirmed.paid_to"), UTRLabel: "UTR", TimeLabel: "Time", RefLabel: "Reference", CodeLabel: "Check code",
		SaveReceipt: "Receipt", ConfirmedUnix: at.Unix(),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = doneTmpl.Execute(w, d)
}

// receipt is the printable record. ?format=txt gives plain text.
func (c *Confirmed) receipt(w http.ResponseWriter, r *http.Request) {
	o, utr, err := c.confirmed(r)
	if err != nil || (o.State != "confirmed" && o.State != "late_paid") {
		http.NotFound(w, r)
		return
	}
	at, _ := time.Parse(time.RFC3339, o.ConfirmedAt)
	code := c.Code(o.ID, o.ConfirmedAt, o.AmountPaise)
	if r.URL.Query().Get("format") == "txt" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "%s\nPaid to: %s\nAmount: %s\nUTR: %s\nTime: %s\nReference: %s\nCheck code: %s\nOrder: %s\n",
			c.ShopName, c.ShopName, i18n.Rupees(o.AmountPaise, c.ShopLang), utr, at.In(istZone).Format(time.RFC3339), o.Reference, code, o.ID)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><html lang="%s"><head><meta charset="utf-8"><title>Receipt</title><link rel="stylesheet" href="/static/done.css"></head>
<body><main class="pay done"><h1>%s</h1><p class="amount">%s</p><p>UTR %s</p><p>%s</p><p>Check code <code>%s</code></p><p>Order %s</p></main></body></html>`,
		c.ShopLang, template.HTMLEscapeString(c.ShopName), i18n.Rupees(o.AmountPaise, c.ShopLang), template.HTMLEscapeString(utr),
		at.In(istZone).Format("02 Jan 2006, 15:04"), code, template.HTMLEscapeString(o.ID))
}

var istZone = time.FixedZone("IST", 5*3600+30*60)

func (c *Confirmed) lang(r *http.Request) i18n.Lang {
	return i18n.FromAcceptLanguage(r.Header.Get("Accept-Language"), c.ShopLang)
}
