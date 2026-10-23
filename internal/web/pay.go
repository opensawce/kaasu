// Package web serves the customer pay page and the checkout pages. Customer pages carry no third-party code and no inline script.
package web

import (
	"embed"
	"encoding/json"
	"errors"
	"html/template"
	"io/fs"
	"net/http"

	"github.com/opensawce/kaasu/i18n"
	"github.com/opensawce/kaasu/internal/accounts"
	"github.com/opensawce/kaasu/internal/orders"
)

//go:embed templates/*.html static/*
var assets embed.FS

var payTmpl = template.Must(template.ParseFS(assets, "templates/pay.html"))

// Pay serves /pay/{id} and /pay/{id}/status.
type Pay struct {
	Orders   *orders.Service
	Accounts *accounts.Service // optional: gives each order its receiving account's display name
	ShopName string
	ShopLang i18n.Lang
}

// Register adds the pay routes to m.
func (p *Pay) Register(m *http.ServeMux) {
	m.HandleFunc("GET /pay/{id}", p.page)
	m.HandleFunc("GET /pay/{id}/status", p.status)
}

// StaticHandler serves the embedded assets (CSS, JS).
func StaticHandler() http.Handler {
	return http.StripPrefix("/static/", http.FileServer(http.FS(mustSub(assets, "static"))))
}

// Handler is the pay pages and static assets on one mux.
func (p *Pay) Handler() http.Handler {
	m := http.NewServeMux()
	p.Register(m)
	m.Handle("GET /static/", StaticHandler())
	return m
}

func mustSub(f fs.FS, dir string) fs.FS {
	s, err := fs.Sub(f, dir)
	if err != nil {
		panic(err)
	}
	return s
}

// lang picks the page language: ?lang= (sets a cookie), then the cookie, then the browser, then the shop default.
func (p *Pay) lang(r *http.Request) i18n.Lang {
	if q := r.URL.Query().Get("lang"); q != "" {
		for _, l := range i18n.Languages {
			if string(l) == q {
				return l
			}
		}
	}
	if c, err := r.Cookie("kaasu_lang"); err == nil {
		for _, l := range i18n.Languages {
			if string(l) == c.Value {
				return l
			}
		}
	}
	return i18n.FromAcceptLanguage(r.Header.Get("Accept-Language"), p.ShopLang)
}

// pageData is what the template needs. Strings come from the catalogue only.
type pageData struct {
	Lang, Title, Shop, Amount, AmountLabel, Button   string
	UPILink                                          template.URL
	WaitingTitle, StepOpen, StepApprove, StepConfirm string
	StatusWord, AlreadyPaid                          string
	Payable                                          bool
	State, StatusURL                                 string
}

// stateWord maps an order state to the status shown. Nothing here says "failed": an unknown outcome is "waiting".
func stateWord(l i18n.Lang, state string) string {
	switch state {
	case "confirmed", "late_paid":
		return i18n.T(l, "status.received")
	case "refunded":
		return i18n.T(l, "status.refunded")
	case "review":
		return i18n.T(l, "status.needs_ok")
	case "expired", "cancelled", "rejected":
		return i18n.T(l, "pay.expired")
	}
	return i18n.T(l, "status.waiting")
}

func (p *Pay) page(w http.ResponseWriter, r *http.Request) {
	o, err := p.Orders.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, orders.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}
	l := p.lang(r)
	if c := r.URL.Query().Get("lang"); c != "" {
		http.SetCookie(w, &http.Cookie{Name: "kaasu_lang", Value: string(l), Path: "/", SameSite: http.SameSiteLaxMode, MaxAge: 31536000})
	}
	amount := i18n.Rupees(o.AmountPaise, l)
	shop := p.ShopName
	if p.Accounts != nil {
		if acc, err := p.Accounts.Get(r.Context(), o.AccountID); err == nil && acc.DisplayName != "" {
			shop = acc.DisplayName
		}
	}
	d := pageData{
		Lang: string(l), Title: shop, Shop: shop, Amount: amount, AmountLabel: amount,
		Button: i18n.T(l, "pay.button", "amount", amount), UPILink: template.URL(o.UPILink),
		WaitingTitle: i18n.T(l, "pay.waiting.title"), StepOpen: i18n.T(l, "pay.waiting.step_open"),
		StepApprove: i18n.T(l, "pay.waiting.step_approve"), StepConfirm: i18n.T(l, "pay.waiting.step_confirm"),
		StatusWord: stateWord(l, o.State), AlreadyPaid: i18n.T(l, "pay.already_paid"),
		Payable: o.State == "awaiting" && o.UPILink != "", State: o.State, StatusURL: "/pay/" + o.ID + "/status",
	}
	if o.State == "queued" {
		d.StatusWord = i18n.T(l, "pay.queued")
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = payTmpl.Execute(w, d)
}

// status is the JSON the page polls. It carries the state only, never amounts or payer data.
func (p *Pay) status(w http.ResponseWriter, r *http.Request) {
	o, err := p.Orders.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, orders.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]string{"state": o.State})
}
