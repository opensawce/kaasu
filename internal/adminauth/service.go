package adminauth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/ledger"
	"github.com/opensawce/kaasu/internal/secrets"
	"github.com/opensawce/kaasu/internal/store"
)

const (
	CookieName  = "kaasu_session"
	absoluteTTL = 12 * time.Hour
	idleTTL     = 30 * time.Minute
	freshTTL    = 5 * time.Minute
	maxFailures = 5
	lockFor     = 5 * time.Minute
)

var (
	ErrBadLogin  = errors.New("sign-in failed: check your code")
	ErrLocked    = errors.New("too many attempts: wait a few minutes")
	ErrNoSession = errors.New("sign in again")
	ErrStale     = errors.New("confirm with your code to continue")
	ErrForbidden = errors.New("only the owner can do this")
	ErrCSRF      = errors.New("form expired: reload the page")
)

// Admin is the signed-in person.
type Admin struct {
	ID, Name, Role string
}

// Service signs admins in and out and checks freshness.
type Service struct {
	st    *store.Store
	clk   clock.Clock
	vault *secrets.Vault
}

func New(st *store.Store, clk clock.Clock, vault *secrets.Vault) *Service {
	return &Service{st: st, clk: clk, vault: vault}
}

func hashOf(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func (s *Service) now() string { return s.clk.Now().UTC().Format(time.RFC3339) }

// CreateAdmin adds a person with their TOTP secret, stored through the vault.
func (s *Service) CreateAdmin(ctx context.Context, name, role string, totpSecret []byte) (string, error) {
	if role != "owner" && role != "staff" {
		return "", fmt.Errorf("role must be owner or staff")
	}
	id := "adm_" + hashOf(name)[:12]
	// The row goes in first, so a duplicate name fails before it can touch the existing person's secret.
	// The vault opens its own transaction, so its write cannot sit inside this one.
	err := s.st.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO admins(id, name, role, created_at) VALUES(?,?,?,?)`, id, name, role, s.now())
		return err
	})
	if err != nil {
		return "", fmt.Errorf("create admin: %w", err)
	}
	if err := s.vault.Put(ctx, "totp."+id, totpSecret); err != nil {
		derr := s.st.Tx(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `DELETE FROM admins WHERE id = ?`, id)
			return err
		})
		return "", errors.Join(fmt.Errorf("store totp secret: %w", err), derr)
	}
	return id, nil
}

// Enroll returns the otpauth:// link an authenticator app scans.
func (s *Service) Enroll(ctx context.Context, name string) (string, error) {
	a, err := s.byName(ctx, name)
	if err != nil {
		return "", err
	}
	secret, err := s.vault.Get(ctx, "totp."+a.ID)
	if err != nil {
		return "", err
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
	return "otpauth://totp/Kaasu:" + url.PathEscape(name) + "?secret=" + enc + "&issuer=Kaasu&digits=6&period=30", nil
}

func (s *Service) byName(ctx context.Context, name string) (Admin, error) {
	var a Admin
	err := s.st.Read().QueryRowContext(ctx, `SELECT id, name, role FROM admins WHERE name = ? AND disabled_at IS NULL`, name).Scan(&a.ID, &a.Name, &a.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return Admin{}, ErrBadLogin
	}
	return a, err
}

// checkCode verifies a TOTP code for an admin and records the step so it cannot be replayed.
func (s *Service) checkCode(ctx context.Context, tx *sql.Tx, a Admin, code string) error {
	secret, err := s.vault.Get(ctx, "totp."+a.ID)
	if err != nil {
		return ErrBadLogin
	}
	var last int64
	if err := tx.QueryRowContext(ctx, `SELECT totp_step FROM admins WHERE id = ?`, a.ID).Scan(&last); err != nil {
		return err
	}
	step, ok := Verify(secret, code, s.clk.Now(), last)
	if !ok {
		return ErrBadLogin
	}
	_, err = tx.ExecContext(ctx, `UPDATE admins SET totp_step = ? WHERE id = ?`, step, a.ID)
	return err
}

// lockedUntil reports an active lock for a key.
func (s *Service) lockedUntil(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, key string) (bool, error) {
	var until sql.NullString
	err := q.QueryRowContext(ctx, `SELECT locked_until FROM auth_failures WHERE key = ?`, key).Scan(&until)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !until.Valid {
		return false, nil
	}
	t, _ := time.Parse(time.RFC3339, until.String)
	return s.clk.Now().Before(t), nil
}

// Login checks name and code and returns a new session token. Unknown names and wrong codes get the same reply.
func (s *Service) Login(ctx context.Context, name, code string) (string, error) {
	key := "login:" + name
	var token string
	ok := false
	err := s.st.Tx(ctx, func(tx *sql.Tx) error {
		// Checked inside the writer, so two parallel guesses cannot both slip past a lock that is about to be set.
		locked, err := s.lockedUntil(ctx, tx, key)
		if err != nil {
			return err
		}
		if locked {
			return ErrLocked
		}
		a, err := s.byName(ctx, name)
		if err == nil {
			err = s.checkCode(ctx, tx, a, code)
		}
		if err != nil {
			if !errors.Is(err, ErrBadLogin) {
				return err
			}
			// Record the failure and commit it. Returning an error here would roll the count back.
			return s.failure(ctx, tx, key)
		}
		ok = true
		if err := s.clearFailures(ctx, tx, key); err != nil {
			return err
		}
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return err
		}
		token = base64.RawURLEncoding.EncodeToString(raw)
		now := s.clk.Now().UTC()
		_, err = tx.ExecContext(ctx, `INSERT INTO sessions(id_hash, admin_id, created_at, last_seen_at, expires_at, fresh_until, csrf_hash)
			VALUES(?,?,?,?,?,?,?)`, hashOf(token), a.ID, now.Format(time.RFC3339), now.Format(time.RFC3339),
			now.Add(absoluteTTL).Format(time.RFC3339), now.Add(freshTTL).Format(time.RFC3339), hashOf(s.csrfFor(token)))
		if err != nil {
			return err
		}
		_, err = ledger.Append(ledger.WithActor(ctx, "admin:"+a.Name), tx, s.clk, ledger.Entry{Kind: "admin.signed_in"})
		return err
	})
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrBadLogin
	}
	return token, nil
}

func (s *Service) failure(ctx context.Context, tx *sql.Tx, key string) error {
	var n int
	_ = tx.QueryRowContext(ctx, `SELECT failures FROM auth_failures WHERE key = ?`, key).Scan(&n)
	n++
	var until any
	if n >= maxFailures {
		until = s.clk.Now().Add(lockFor).UTC().Format(time.RFC3339)
		n = 0
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO auth_failures(key, failures, locked_until) VALUES(?,?,?)
		ON CONFLICT(key) DO UPDATE SET failures=excluded.failures, locked_until=excluded.locked_until`, key, n, until); err != nil {
		return err
	}
	return nil
}

func (s *Service) clearFailures(ctx context.Context, tx *sql.Tx, key string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM auth_failures WHERE key = ?`, key)
	return err
}

// Authenticate returns the admin for a live session and slides the idle timer.
func (s *Service) Authenticate(ctx context.Context, token string) (Admin, error) {
	now := s.clk.Now().UTC()
	var a Admin
	var lastSeen, expires string
	err := s.st.Read().QueryRowContext(ctx, `SELECT a.id, a.name, a.role, se.last_seen_at, se.expires_at
		FROM sessions se JOIN admins a ON a.id = se.admin_id WHERE se.id_hash = ? AND a.disabled_at IS NULL`, hashOf(token)).
		Scan(&a.ID, &a.Name, &a.Role, &lastSeen, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return Admin{}, ErrNoSession
	}
	if err != nil {
		return Admin{}, err
	}
	ls, _ := time.Parse(time.RFC3339, lastSeen)
	ex, _ := time.Parse(time.RFC3339, expires)
	if !now.Before(ex) || now.After(ls.Add(idleTTL)) {
		return Admin{}, ErrNoSession
	}
	if err := s.st.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE sessions SET last_seen_at = ? WHERE id_hash = ?`, now.Format(time.RFC3339), hashOf(token))
		return err
	}); err != nil {
		return Admin{}, err
	}
	return a, nil
}

// RequireFresh reports ErrStale unless the admin confirmed with a code in the last five minutes.
func (s *Service) RequireFresh(ctx context.Context, token string) error {
	var fresh string
	err := s.st.Read().QueryRowContext(ctx, `SELECT fresh_until FROM sessions WHERE id_hash = ?`, hashOf(token)).Scan(&fresh)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoSession
	}
	if err != nil {
		return err
	}
	t, _ := time.Parse(time.RFC3339, fresh)
	if !s.clk.Now().Before(t) {
		return ErrStale
	}
	return nil
}

// Reauth confirms the admin's code and refreshes freshness.
func (s *Service) Reauth(ctx context.Context, token, code string) error {
	a, err := s.Authenticate(ctx, token)
	if err != nil {
		return err
	}
	key := "reauth:" + a.ID
	bad := false
	err = s.st.Tx(ctx, func(tx *sql.Tx) error {
		locked, err := s.lockedUntil(ctx, tx, key)
		if err != nil {
			return err
		}
		if locked {
			return ErrLocked
		}
		if err := s.checkCode(ctx, tx, a, code); err != nil {
			if !errors.Is(err, ErrBadLogin) {
				return err
			}
			bad = true
			return s.failure(ctx, tx, key) // commits the count; the bad-code error is returned after
		}
		if err := s.clearFailures(ctx, tx, key); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE sessions SET fresh_until = ? WHERE id_hash = ?`,
			s.clk.Now().Add(freshTTL).UTC().Format(time.RFC3339), hashOf(token))
		return err
	})
	if err != nil {
		return err
	}
	if bad {
		return ErrBadLogin
	}
	return nil
}

func (s *Service) csrfFor(token string) string {
	m := hmac.New(sha256.New, []byte("kaasu-csrf:"+token))
	m.Write([]byte("csrf"))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// CSRFToken is the token a form must send back. It is derived from the session, so it is never stored.
func (s *Service) CSRFToken(ctx context.Context, token string) (string, error) {
	if _, err := s.Authenticate(ctx, token); err != nil {
		return "", err
	}
	return s.csrfFor(token), nil
}

// CheckCSRF compares the form token with the session's in constant time.
func (s *Service) CheckCSRF(ctx context.Context, token, given string) error {
	want, err := s.CSRFToken(ctx, token)
	if err != nil {
		return err
	}
	if !hmac.Equal([]byte(want), []byte(given)) {
		return ErrCSRF
	}
	return nil
}

// RequireOwner refuses staff. Staff are read-only.
func RequireOwner(a Admin) error {
	if a.Role != "owner" {
		return ErrForbidden
	}
	return nil
}

// CookieFor builds the session cookie. Secure is off only for plain local http.
func CookieFor(token string, secure bool) *http.Cookie {
	return &http.Cookie{Name: CookieName, Value: token, Path: "/admin", HttpOnly: true, Secure: secure,
		SameSite: http.SameSiteStrictMode, MaxAge: int(absoluteTTL.Seconds())}
}

type ctxKey struct{}

// AdminFrom returns the admin set by Require.
func AdminFrom(ctx context.Context) (Admin, bool) {
	a, ok := ctx.Value(ctxKey{}).(Admin)
	return a, ok
}

// Require admits only signed-in admins and sends everyone else to the sign-in page.
func (s *Service) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(CookieName)
		if err != nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		a, err := s.Authenticate(r.Context(), c.Value)
		if err != nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, a)))
	})
}
