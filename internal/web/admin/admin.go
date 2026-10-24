// Package admin serves the merchant dashboard on the admin listener only. Every page except sign-in needs a session.
package admin

import (
	"context"
	"database/sql"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/opensawce/kaasu/i18n"
	"github.com/opensawce/kaasu/internal/adminauth"
	"github.com/opensawce/kaasu/internal/evidence"
)

// Dashboard holds what the pages need.
type Dashboard struct {
	Auth *adminauth.Service
	DB   *sql.DB // read pool
	Lang i18n.Lang
	Now  func() time.Time
}

var (
	loginTmpl = template.Must(template.New("login").Parse(`<!doctype html><html lang="{{.Lang}}"><head><meta charset="utf-8"><title>Sign in</title></head>
<body><main><h1>Sign in</h1><form method="post" action="/admin/login">
<label>Name <input name="name" autocomplete="username"></label>
<label>Code <input name="code" inputmode="numeric" autocomplete="one-time-code"></label>
<button type="submit">Sign in</button></form>{{if .Error}}<p role="alert">{{.Error}}</p>{{end}}</main></body></html>`))
	todayTmpl = template.Must(template.New("today").Parse(`<!doctype html><html lang="{{.Lang}}"><head><meta charset="utf-8"><title>Today</title></head>
<body><main><h1>Today</h1>
{{if .Warn}}<p class="warn" role="status">{{.WarnText}}</p>{{end}}
<section aria-label="Totals"><p>{{.TotalLabel}}: <strong>{{.Total}}</strong> ({{.Count}})</p></section>
<section aria-label="Sources"><h2>Sources</h2><ul>{{range .Sources}}<li><span class="chip">{{.Word}}</span> {{.Label}}</li>{{else}}<li>{{.NoSources}}</li>{{end}}</ul></section>
<p><a href="/admin/orders">Orders</a></p></main></body></html>`))
	ordersTmpl = template.Must(template.New("orders").Parse(`<!doctype html><html lang="{{.Lang}}"><head><meta charset="utf-8"><title>Orders</title></head>
<body><main><h1>Orders</h1><table><thead><tr><th scope="col">Order</th><th scope="col">Amount</th><th scope="col">Status</th><th scope="col">Payer</th></tr></thead>
<tbody>{{range .Rows}}<tr><td>{{.ID}}</td><td class="num">{{.Amount}}</td><td><span class="chip">{{.Word}}</span></td><td>{{.Payer}}</td></tr>{{else}}<tr><td colspan="4">No orders yet</td></tr>{{end}}</tbody></table></main></body></html>`))
)

// Register adds the routes to m.
func (d *Dashboard) Register(m *http.ServeMux) {
	m.HandleFunc("GET /admin/login", d.loginPage)
	m.HandleFunc("POST /admin/login", d.loginPost)
	m.Handle("GET /admin/{$}", d.auth(http.HandlerFunc(d.today)))
	m.Handle("GET /admin/orders", d.auth(http.HandlerFunc(d.orders)))
}

func (d *Dashboard) auth(next http.Handler) http.Handler { return d.Auth.Require(next) }

func (d *Dashboard) loginPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = loginTmpl.Execute(w, map[string]any{"Lang": string(d.Lang), "Error": ""})
}

func (d *Dashboard) loginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	token, err := d.Auth.Login(r.Context(), strings.TrimSpace(r.PostFormValue("name")), strings.TrimSpace(r.PostFormValue("code")))
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_ = loginTmpl.Execute(w, map[string]any{"Lang": string(d.Lang), "Error": err.Error()})
		return
	}
	http.SetCookie(w, adminauth.CookieFor(token, r.TLS != nil || !isLocal(r)))
	http.Redirect(w, r, "/admin/", http.StatusSeeOther)
}

func isLocal(r *http.Request) bool {
	return strings.HasPrefix(r.Host, "localhost") || strings.HasPrefix(r.Host, "127.0.0.1") || strings.HasPrefix(r.Host, "[::1]")
}

type sourceRow struct{ Label, Word string }

type todayData struct {
	Lang, TotalLabel, Total, Count, WarnText, NoSources string
	Warn                                                bool
	Sources                                             []sourceRow
}

func (d *Dashboard) today(w http.ResponseWriter, r *http.Request) {
	now := d.Now().In(istZone)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, istZone).UTC().Format(time.RFC3339)
	var total int64
	var count int
	if err := d.DB.QueryRowContext(r.Context(), `SELECT COALESCE(SUM(amount_paise),0), count(*) FROM orders
		WHERE state IN ('confirmed','late_paid') AND confirmed_at >= ?`, start).Scan(&total, &count); err != nil {
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	srcs, active, err := d.sources(r.Context(), now.UTC())
	if err != nil {
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	l := d.Lang
	// Fewer than two live sources is a warning: one source is a single point of failure, none means nothing is seen.
	data := todayData{
		Lang: string(l), TotalLabel: i18n.T(l, "status.received"), Total: i18n.Rupees(total, l), Count: itoa(count),
		Warn: active < 2, Sources: srcs, NoSources: i18n.T(l, "status.phone_offline"),
		WarnText: "Only one way to see payments. Add a second source.",
	}
	if active == 0 {
		data.WarnText = "No source is sending payments right now. Pair a phone or connect email."
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = todayTmpl.Execute(w, data)
}

// sources lists the paired devices with a health word. A device quiet for 15 minutes shows "Phone offline".
func (d *Dashboard) sources(ctx context.Context, now time.Time) ([]sourceRow, int, error) {
	rows, err := d.DB.QueryContext(ctx, `SELECT label, COALESCE(last_seen_at,'') FROM devices WHERE revoked_at IS NULL ORDER BY label`)
	if err != nil {
		return nil, 0, fmt.Errorf("dashboard: list sources: %w", err)
	}
	defer rows.Close()
	var out []sourceRow
	active := 0
	for rows.Next() {
		var label, seen string
		if err := rows.Scan(&label, &seen); err != nil {
			return nil, 0, fmt.Errorf("dashboard: scan source: %w", err)
		}
		word := i18n.T(d.Lang, "status.phone_offline")
		if t, err := time.Parse(time.RFC3339, seen); err == nil && now.Sub(t) <= 15*time.Minute {
			word = i18n.T(d.Lang, "status.received")
			active++
		}
		out = append(out, sourceRow{Label: label, Word: word})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("dashboard: list sources: %w", err)
	}
	return out, active, nil
}

type orderRow struct{ ID, Amount, Word, Payer string }

func (d *Dashboard) orders(w http.ResponseWriter, r *http.Request) {
	rows, err := d.DB.QueryContext(r.Context(), `SELECT o.id, o.state, o.amount_paise,
		COALESCE((SELECT al.payer_masked FROM matches m JOIN alerts al ON al.id = m.alert_id WHERE m.order_id = o.id ORDER BY m.created_at DESC LIMIT 1), '')
		FROM (SELECT id, state, amount_paise, created_at FROM orders ORDER BY created_at DESC, id DESC LIMIT 50) o
		ORDER BY o.created_at DESC, o.id DESC`)
	if err != nil {
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var out []orderRow
	for rows.Next() {
		var id, state, payer string
		var amt int64
		if err := rows.Scan(&id, &state, &amt, &payer); err != nil {
			http.Error(w, "unavailable", http.StatusInternalServerError)
			return
		}
		out = append(out, orderRow{ID: id, Amount: i18n.Rupees(amt, d.Lang), Word: stateWord(d.Lang, state), Payer: maskPayer(payer)})
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = ordersTmpl.Execute(w, map[string]any{"Lang": string(d.Lang), "Rows": out})
}

func maskPayer(p string) string {
	if p == "" {
		return "-"
	}
	// Already masked when stored; mask again so a full VPA can never reach this page.
	return evidence.MaskVPA(p)
}

func stateWord(l i18n.Lang, state string) string {
	switch state {
	case "confirmed", "late_paid", "refunded":
		return i18n.T(l, "status.received")
	case "review":
		return i18n.T(l, "status.needs_ok")
	}
	return i18n.T(l, "status.waiting")
}

var istZone = time.FixedZone("IST", 5*3600+30*60)

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}
