package adminauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/secrets"
	"github.com/opensawce/kaasu/internal/store"
	"github.com/opensawce/kaasu/internal/testutil"
)

var t0 = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
var seed = []byte("12345678901234567890")

type env struct {
	st  *store.Store
	clk *clock.Fake
	svc *Service
	ctx context.Context
}

func setup(t *testing.T) env {
	t.Helper()
	st := testutil.OpenStore(t)
	clk := clock.NewFake(t0)
	vault := secrets.NewVault(st, secrets.MasterKey(make([]byte, 32)), clk)
	svc := New(st, clk, vault)
	ctx := context.Background()
	if _, err := svc.CreateAdmin(ctx, "priya", "owner", seed); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateAdmin(ctx, "ravi", "staff", seed); err != nil {
		t.Fatal(err)
	}
	return env{st: st, clk: clk, svc: svc, ctx: ctx}
}

func (e env) totp(at time.Time) string { return code(seed, at, 6) }

func TestLoginWithTOTP(t *testing.T) {
	e := setup(t)
	tok, err := e.svc.Login(e.ctx, "priya", e.totp(e.clk.Now()))
	if err != nil || tok == "" {
		t.Fatalf("login: %v", err)
	}
	a, err := e.svc.Authenticate(e.ctx, tok)
	if err != nil || a.Name != "priya" || a.Role != "owner" {
		t.Fatalf("authenticate: %+v %v", a, err)
	}
}

func TestSameReplyForUnknownAdminAndWrongCode(t *testing.T) {
	e := setup(t)
	_, errUnknown := e.svc.Login(e.ctx, "nobody", "000000")
	_, errWrong := e.svc.Login(e.ctx, "priya", "000000")
	if errUnknown == nil || errWrong == nil || errUnknown.Error() != errWrong.Error() {
		t.Errorf("replies differ: %v / %v", errUnknown, errWrong)
	}
}

func TestLockoutAfterFiveFailures(t *testing.T) {
	e := setup(t)
	for i := 0; i < 5; i++ {
		_, _ = e.svc.Login(e.ctx, "priya", "000000")
	}
	if _, err := e.svc.Login(e.ctx, "priya", e.totp(e.clk.Now())); !errors.Is(err, ErrLocked) {
		t.Fatalf("right code while locked must still be refused, got %v", err)
	}
	e.clk.Advance(5*time.Minute + time.Second)
	if _, err := e.svc.Login(e.ctx, "priya", e.totp(e.clk.Now())); err != nil {
		t.Errorf("after the lock: %v", err)
	}
}

func TestReplayedTOTPRefused(t *testing.T) {
	e := setup(t)
	c := e.totp(e.clk.Now())
	if _, err := e.svc.Login(e.ctx, "priya", c); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Login(e.ctx, "priya", c); err == nil {
		t.Error("the same code must not work twice")
	}
}

func TestSessionAbsoluteAndIdleExpiry(t *testing.T) {
	e := setup(t)
	tok, _ := e.svc.Login(e.ctx, "priya", e.totp(e.clk.Now()))
	e.clk.Advance(29 * time.Minute)
	if _, err := e.svc.Authenticate(e.ctx, tok); err != nil {
		t.Fatalf("active session: %v", err)
	}
	e.clk.Advance(31 * time.Minute)
	if _, err := e.svc.Authenticate(e.ctx, tok); !errors.Is(err, ErrNoSession) {
		t.Errorf("idle 31 minutes should expire: %v", err)
	}
	tok2, _ := e.svc.Login(e.ctx, "priya", e.totp(e.clk.Now()))
	for i := 0; i < 40; i++ {
		e.clk.Advance(20 * time.Minute)
		_, _ = e.svc.Authenticate(e.ctx, tok2)
	}
	if _, err := e.svc.Authenticate(e.ctx, tok2); !errors.Is(err, ErrNoSession) {
		t.Errorf("absolute 12 hours should expire even when active: %v", err)
	}
}

func TestSessionStoredHashed(t *testing.T) {
	e := setup(t)
	tok, _ := e.svc.Login(e.ctx, "priya", e.totp(e.clk.Now()))
	var id string
	_ = e.st.Read().QueryRow(`SELECT id_hash FROM sessions`).Scan(&id)
	if id == "" || strings.Contains(id, tok) || id == tok {
		t.Error("session id must be stored only as a hash")
	}
}

func TestCookieFlags(t *testing.T) {
	c := CookieFor("tok", true)
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/admin" || c.MaxAge <= 0 {
		t.Errorf("cookie flags: %+v", c)
	}
	// Only plain local http (a dev machine) may drop Secure.
	if CookieFor("tok", false).Secure {
		t.Error("Secure is only dropped for local http")
	}
}

func TestStaffCannotDoOwnerActions(t *testing.T) {
	e := setup(t)
	tok, _ := e.svc.Login(e.ctx, "ravi", e.totp(e.clk.Now()))
	a, _ := e.svc.Authenticate(e.ctx, tok)
	if err := RequireOwner(a); err == nil {
		t.Error("staff must not pass RequireOwner")
	}
	owner, _ := e.svc.Login(e.ctx, "priya", e.totp(e.clk.Now()))
	o, _ := e.svc.Authenticate(e.ctx, owner)
	if err := RequireOwner(o); err != nil {
		t.Errorf("owner: %v", err)
	}
}

func TestStaleSessionNeedsReauthForSensitive(t *testing.T) {
	e := setup(t)
	tok, _ := e.svc.Login(e.ctx, "priya", e.totp(e.clk.Now()))
	if err := e.svc.RequireFresh(e.ctx, tok); err != nil {
		t.Errorf("a login is fresh straight away: %v", err)
	}
	e.clk.Advance(6 * time.Minute)
	if err := e.svc.RequireFresh(e.ctx, tok); !errors.Is(err, ErrStale) {
		t.Fatalf("after 6 minutes it must be stale: %v", err)
	}
	if err := e.svc.Reauth(e.ctx, tok, e.totp(e.clk.Now())); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RequireFresh(e.ctx, tok); err != nil {
		t.Errorf("after reauth: %v", err)
	}
}

func TestCSRFRequired(t *testing.T) {
	e := setup(t)
	tok, _ := e.svc.Login(e.ctx, "priya", e.totp(e.clk.Now()))
	csrf, err := e.svc.CSRFToken(e.ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.CheckCSRF(e.ctx, tok, csrf); err != nil {
		t.Errorf("right token: %v", err)
	}
	if err := e.svc.CheckCSRF(e.ctx, tok, ""); err == nil {
		t.Error("missing token must fail")
	}
	if err := e.svc.CheckCSRF(e.ctx, tok, "forged"); err == nil {
		t.Error("forged token must fail")
	}
}

func TestMiddlewareRejectsWithoutSession(t *testing.T) {
	e := setup(t)
	h := e.svc.Require(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/admin/", nil))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("no session: %d", rec.Code)
	}
}

func TestEnrollmentURI(t *testing.T) {
	e := setup(t)
	uri, err := e.svc.Enroll(e.ctx, "priya")
	if err != nil || !strings.HasPrefix(uri, "otpauth://totp/Kaasu:priya?secret=") {
		t.Errorf("uri=%s err=%v", uri, err)
	}
}

func TestReauthLocksAfterFiveBadCodes(t *testing.T) {
	e := setup(t)
	tok, _ := e.svc.Login(e.ctx, "priya", e.totp(e.clk.Now()))
	e.clk.Advance(6 * time.Minute)
	for i := 0; i < 5; i++ {
		if err := e.svc.Reauth(e.ctx, tok, "000000"); !errors.Is(err, ErrBadLogin) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if err := e.svc.Reauth(e.ctx, tok, e.totp(e.clk.Now())); !errors.Is(err, ErrLocked) {
		t.Fatalf("a correct code must not get through while locked: %v", err)
	}
}

func TestDuplicateAdminKeepsTheFirstSecret(t *testing.T) {
	e := setup(t)
	if id, err := e.svc.CreateAdmin(e.ctx, "priya", "staff", []byte("another-secret-20-by")); err == nil || id != "" {
		t.Fatalf("duplicate name must fail with an empty id: %q %v", id, err)
	}
	if _, err := e.svc.Login(e.ctx, "priya", e.totp(e.clk.Now())); err != nil {
		t.Fatalf("the original secret must still work: %v", err)
	}
}
