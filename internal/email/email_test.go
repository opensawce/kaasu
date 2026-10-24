package email

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-msgauth/dkim"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/evidence"
	"github.com/opensawce/kaasu/internal/store"
	"github.com/opensawce/kaasu/internal/testutil"
)

var t0 = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

type fakeFetcher struct{ msgs []RawMessage }

func (f fakeFetcher) Fetch(context.Context) ([]RawMessage, error) { return f.msgs, nil }

type fixture struct {
	st     *store.Store
	key    *rsa.PrivateKey
	domain string
	dns    map[string][]string
}

func setup(t *testing.T) (*Poller, fixture) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := fixture{st: testutil.OpenStore(t), key: key, domain: "hdfcbank.net", dns: map[string][]string{}}
	der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	f.dns["s1._domainkey.hdfcbank.net"] = []string{"v=DKIM1; k=rsa; p=" + base64.StdEncoding.EncodeToString(der)}
	clk := clock.NewFake(t0)
	v := DKIMVerifier{LookupTXT: func(d string) ([]string, error) { return f.dns[d], nil }}
	p := NewPoller(f.st, clk, evidence.New(clk, &testutil.Seq{}), fakeFetcher{}, v, nil)
	return p, f
}

const body = "Rs.499.07 credited to a/c XX1234 on 23-10-26 by VPA sample.payer@okicici (UPI Ref No 629812345678)."

func (f fixture) message(from, text string) []byte {
	return []byte("From: alerts@" + from + "\r\nTo: owner@example.com\r\nSubject: Credit alert\r\nDate: Thu, 01 Jan 2026 10:00:00 +0000\r\n" +
		"Message-ID: <" + fmt.Sprint(time.Now().UnixNano()) + "@" + from + ">\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + text + "\r\n")
}

// signed signs the message with the fixture key for domain d, like a bank's mail server would.
func (f fixture) signed(t *testing.T, d string, raw []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := dkim.Sign(&out, bytes.NewReader(raw), &dkim.SignOptions{Domain: d, Selector: "s1", Signer: crypto.Signer(f.key), HeaderKeys: []string{"From", "To", "Subject", "Date", "Message-ID"}}); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func count(t *testing.T, st *store.Store, q string) int {
	t.Helper()
	var n int
	if err := st.Read().QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func rejectReason(t *testing.T, st *store.Store) string {
	t.Helper()
	var p string
	_ = st.Read().QueryRow(`SELECT payload FROM ledger WHERE kind='email.rejected' ORDER BY seq DESC LIMIT 1`).Scan(&p)
	return p
}

func TestValidDKIMAccepted(t *testing.T) {
	p, f := setup(t)
	raw := f.signed(t, f.domain, f.message(f.domain, body))
	ok, err := p.handle(context.Background(), raw)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if n := count(t, f.st, `SELECT count(*) FROM alerts`); n != 1 {
		t.Errorf("alerts=%d", n)
	}
	if n := count(t, f.st, `SELECT count(*) FROM alert_sources s JOIN evidence e ON e.id=s.evidence_id WHERE e.source='email'`); n != 1 {
		t.Errorf("email sources=%d", n)
	}
}

func TestTamperedBodyRejected(t *testing.T) {
	p, f := setup(t)
	raw := f.signed(t, f.domain, f.message(f.domain, body))
	tampered := bytes.Replace(raw, []byte("499.07"), []byte("999.07"), 1)
	if _, err := p.handle(context.Background(), tampered); err != nil {
		t.Fatal(err)
	}
	if n := count(t, f.st, `SELECT count(*) FROM alerts`); n != 0 {
		t.Error("tampered mail must not create an alert")
	}
	if !strings.Contains(rejectReason(t, f.st), "bad_dkim") {
		t.Errorf("reason: %s", rejectReason(t, f.st))
	}
}

func TestNoSignatureRejected(t *testing.T) {
	p, f := setup(t)
	if _, err := p.handle(context.Background(), f.message(f.domain, body)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rejectReason(t, f.st), "no_dkim") {
		t.Errorf("reason: %s", rejectReason(t, f.st))
	}
}

func TestLookalikeDomainRejected(t *testing.T) {
	p, f := setup(t)
	// Signed as hdfcbank.net.evil.io with a key published for that name, so the signature itself is valid.
	evil := "hdfcbank.net.evil.io"
	f.dns["s1._domainkey."+evil] = f.dns["s1._domainkey.hdfcbank.net"]
	raw := f.signed(t, evil, f.message(evil, body))
	if _, err := p.handle(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	if n := count(t, f.st, `SELECT count(*) FROM alerts`); n != 0 {
		t.Error("look-alike domain must not create an alert")
	}
	if !strings.Contains(rejectReason(t, f.st), "domain_not_allowed") {
		t.Errorf("reason: %s", rejectReason(t, f.st))
	}
}

func TestFromNotAlignedRejected(t *testing.T) {
	p, f := setup(t)
	// Signed by hdfcbank.net, but From claims another domain.
	raw := f.signed(t, f.domain, []byte("From: alerts@other.example\r\nTo: x@example.com\r\nSubject: Credit alert\r\nDate: Thu, 01 Jan 2026 10:00:00 +0000\r\nMessage-ID: <a1@hdfcbank.net>\r\nContent-Type: text/plain\r\n\r\n"+body+"\r\n"))
	if _, err := p.handle(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	if n := count(t, f.st, `SELECT count(*) FROM alerts`); n != 0 {
		t.Error("misaligned From must not create an alert")
	}
}

func TestDuplicateMessageIgnored(t *testing.T) {
	p, f := setup(t)
	raw := f.signed(t, f.domain, f.message(f.domain, body))
	for i := 0; i < 2; i++ {
		if _, err := p.handle(context.Background(), raw); err != nil {
			t.Fatal(err)
		}
	}
	if n := count(t, f.st, `SELECT count(*) FROM evidence`); n != 1 {
		t.Errorf("evidence=%d want 1", n)
	}
}

func TestHTMLOnlyBodyParsed(t *testing.T) {
	p, f := setup(t)
	html := "From: alerts@" + f.domain + "\r\nTo: x@example.com\r\nSubject: s\r\nDate: Thu, 01 Jan 2026 10:00:00 +0000\r\nMessage-ID: <h1@hdfcbank.net>\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>" + body + "</p>\r\n"
	raw := f.signed(t, f.domain, []byte(html))
	if _, err := p.handle(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	if n := count(t, f.st, `SELECT count(*) FROM alerts`); n != 1 {
		t.Errorf("html body should parse, alerts=%d", n)
	}
}

func TestHugeBodyDoesNotHang(t *testing.T) {
	p, f := setup(t)
	big := strings.Repeat("x", 1<<20)
	raw := f.signed(t, f.domain, f.message(f.domain, big))
	start := time.Now()
	if _, err := p.handle(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("large body took too long")
	}
}

func TestOnceCountsAcceptedAndRejected(t *testing.T) {
	p, f := setup(t)
	good := f.signed(t, f.domain, f.message(f.domain, body))
	bad := f.message(f.domain, body)
	p.fetch = fakeFetcher{msgs: []RawMessage{{UID: "1", Raw: good}, {UID: "2", Raw: bad}}}
	acc, rej, err := p.Once(context.Background())
	if err != nil || acc != 1 || rej != 1 {
		t.Fatalf("acc=%d rej=%d err=%v", acc, rej, err)
	}
}

func FuzzEmailBody(f *testing.F) {
	f.Add("text/plain", "hello")
	f.Add("multipart/alternative; boundary=x", "--x\r\nContent-Type: text/html\r\n\r\n<b>hi</b>\r\n--x--")
	f.Fuzz(func(t *testing.T, ct string, body string) {
		_, _ = bodyText(ct, strings.NewReader(body))
	})
}

func TestBodyTextDecodesTransferEncodingsAndNestedParts(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString([]byte(body))
	nested := "--outer\r\nContent-Type: multipart/alternative; boundary=inner\r\n\r\n" +
		"--inner\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n" + b64[:20] + "\r\n" + b64[20:] + "\r\n--inner--\r\n" +
		"--outer--\r\n"
	got, err := bodyText("multipart/mixed; boundary=outer", strings.NewReader(nested))
	if err != nil || got != body {
		t.Fatalf("nested base64: %q %v", got, err)
	}
	qp := "Rs.499.07 credited =\r\nto a/c XX1234"
	got, err = bodyText("text/plain", strings.NewReader("x"))
	if err != nil || got != "x" {
		t.Fatalf("plain: %q %v", got, err)
	}
	pl, _, err := walkPart("text/plain", "quoted-printable", strings.NewReader(qp), 0)
	if err != nil || pl != "Rs.499.07 credited to a/c XX1234" {
		t.Fatalf("qp: %q %v", pl, err)
	}
}

type markingFetcher struct {
	fakeFetcher
	marked []string
}

func (m *markingFetcher) MarkSeen(_ context.Context, uids []string) error {
	m.marked = append(m.marked, uids...)
	return nil
}

func TestHandledMessagesAreMarkedSeen(t *testing.T) {
	p, f := setup(t)
	good := f.signed(t, f.domain, f.message(f.domain, body))
	bad := f.message("evil.example", body)
	mf := &markingFetcher{fakeFetcher: fakeFetcher{msgs: []RawMessage{{UID: "7", Raw: good}, {UID: "8", Raw: bad}}}}
	p.fetch = mf
	if _, _, err := p.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Join(mf.marked, ",") != "7,8" {
		t.Errorf("marked %v, want both accepted and rejected messages", mf.marked)
	}
}
