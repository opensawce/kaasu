// Package evidence keeps every raw alert exactly as received and turns parsed alerts into deduplicated facts.
// The same payment seen by SMS, notification and email counts once.
package evidence

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/ledger"
)

var ErrInvalid = errors.New("invalid evidence")

// Parsed is the structured content of one alert. The full payer VPA never goes in here.
type Parsed struct {
	Bank        string
	AmountPaise int64
	UTR         string
	PayerMasked string
	AccountHint string
	OccurredAt  time.Time
	Parser      string
}

type IDSource interface{ New(prefix string) string }

type Service struct {
	clk clock.Clock
	ids IDSource
}

func New(clk clock.Clock, ids IDSource) *Service {
	return &Service{clk: clk, ids: ids}
}

// SaveEvidence stores the raw bytes. A body seen before returns its existing id with dup=true.
func (s *Service) SaveEvidence(ctx context.Context, tx *sql.Tx, source, deviceID string, raw []byte) (string, bool, error) {
	switch source {
	case "device", "email", "manual":
	default:
		return "", false, fmt.Errorf("%w: source %q", ErrInvalid, source)
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	var existing string
	err := tx.QueryRowContext(ctx, `SELECT id FROM evidence WHERE raw_hash = ?`, hash).Scan(&existing)
	if err == nil {
		return existing, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", false, err
	}
	id := s.ids.New("evd")
	var dev any
	if deviceID != "" {
		dev = deviceID
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO evidence(id,source,device_id,received_at,raw_hash,body) VALUES(?,?,?,?,?,?)`,
		id, source, dev, s.clk.Now().UTC().Format(time.RFC3339), hash, raw)
	if err != nil {
		return "", false, fmt.Errorf("save evidence: %w", err)
	}
	return id, false, nil
}

// Fingerprint identifies one payment across sources when the UTR is missing. Time is bucketed to 5 minutes.
func Fingerprint(accountHint string, amountPaise int64, utr string, occurred time.Time) string {
	bucket := occurred.Unix() / 300
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s|%d", accountHint, amountPaise, utr, bucket)))
	return hex.EncodeToString(h[:])
}

// SaveAlert records the facts of an alert. A repeat of a known payment adds a source instead of an alert.
func (s *Service) SaveAlert(ctx context.Context, tx *sql.Tx, evidenceID string, p Parsed) (string, bool, error) {
	fp := Fingerprint(p.AccountHint, p.AmountPaise, p.UTR, p.OccurredAt)
	existing, err := findExisting(ctx, tx, p.UTR, fp)
	if err != nil {
		return "", false, err
	}
	if existing != "" {
		if err := addSource(ctx, tx, existing, evidenceID); err != nil {
			return "", false, err
		}
		return existing, false, nil
	}
	id := s.ids.New("alt")
	var utr any
	if p.UTR != "" {
		utr = p.UTR
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO alerts(id,evidence_id,bank,amount_paise,utr,payer_masked,account_hint,occurred_at,parsed_at,parser,fingerprint)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`, id, evidenceID, nullable(p.Bank), p.AmountPaise, utr, nullable(p.PayerMasked),
		nullable(p.AccountHint), p.OccurredAt.UTC().Format(time.RFC3339), s.clk.Now().UTC().Format(time.RFC3339), nullable(p.Parser), fp)
	if err != nil {
		if isUnique(err) {
			// A parallel save with the same UTR won the race: treat this one as a duplicate.
			return s.SaveAlert(ctx, tx, evidenceID, p)
		}
		return "", false, fmt.Errorf("save alert: %w", err)
	}
	if err := addSource(ctx, tx, id, evidenceID); err != nil {
		return "", false, err
	}
	if _, err := ledger.Append(ctx, tx, s.clk, ledger.Entry{Kind: "alert.received", Payload: map[string]any{
		"alert_id": id, "amount_paise": p.AmountPaise, "bank": p.Bank, "payer_masked": p.PayerMasked, "parser": p.Parser,
	}}); err != nil {
		return "", false, err
	}
	return id, true, nil
}

func findExisting(ctx context.Context, tx *sql.Tx, utr, fp string) (string, error) {
	var id string
	if utr != "" {
		err := tx.QueryRowContext(ctx, `SELECT id FROM alerts WHERE utr = ?`, utr).Scan(&id)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}
	if utr == "" {
		err := tx.QueryRowContext(ctx, `SELECT id FROM alerts WHERE fingerprint = ? LIMIT 1`, fp).Scan(&id)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}
	return "", nil
}

func addSource(ctx context.Context, tx *sql.Tx, alertID, evidenceID string) error {
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO alert_sources(alert_id, evidence_id) VALUES(?,?)`, alertID, evidenceID)
	if err != nil {
		return fmt.Errorf("add source: %w", err)
	}
	return nil
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// MaskVPA keeps two characters of the name and the handle: ravi.kumar@okicici becomes ra***@okicici.
func MaskVPA(vpa string) string {
	name, handle, ok := strings.Cut(vpa, "@")
	if !ok {
		return "***"
	}
	keep := 2
	if len(name) <= 2 {
		keep = 1
	}
	if len(name) < keep {
		keep = len(name)
	}
	return name[:keep] + "***@" + handle
}
