// Package recon imports bank statements and proves each confirmed payment against them.
// Amounts are integer paise throughout.
package recon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/opensawce/kaasu/internal/parsers"
	"github.com/opensawce/kaasu/internal/states"
	"github.com/opensawce/kaasu/internal/store"
)

const (
	maxFileBytes = 10 << 20
	maxRows      = 100_000
)

// Mapping says which CSV columns hold what. Names are the exact header text in the file.
type Mapping struct {
	DateCol, DescCol, CreditCol, DebitCol, UTRCol string
	DateLayout                                    string
	Delimiter                                     rune
}

// MappingFor returns the known layout for a bank, or a generic one for "generic".
func MappingFor(bank string) (Mapping, bool) {
	switch strings.ToLower(bank) {
	case "hdfc":
		return Mapping{DateCol: "Date", DescCol: "Narration", CreditCol: "Deposit Amt.", DebitCol: "Withdrawal Amt.", UTRCol: "Reference No.", DateLayout: "02/01/2006", Delimiter: ','}, true
	case "generic":
		return Mapping{DateCol: "date", DescCol: "description", CreditCol: "credit", DebitCol: "debit", UTRCol: "utr", DateLayout: "2006-01-02", Delimiter: ','}, true
	}
	return Mapping{}, false
}

// paise reads an amount; an empty cell is zero.
func paise(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	return parsers.ParseAmountPaise(s)
}

// ImportCSV stores a statement. Importing the same file twice stores it once.
func ImportCSV(ctx context.Context, tx *sql.Tx, r io.Reader, m Mapping, accountID string) (string, int, error) {
	if accountID == "" {
		return "", 0, errors.New("statement needs the receiving account it belongs to")
	}
	data, err := io.ReadAll(io.LimitReader(r, maxFileBytes+1))
	if err != nil {
		return "", 0, err
	}
	if len(data) > maxFileBytes {
		return "", 0, errors.New("statement is larger than 10 MB")
	}
	sum := sha256.Sum256(data)
	fileHash := hex.EncodeToString(sum[:])
	scoped := sha256.Sum256([]byte(accountID + "|" + fileHash)) // the same file for another account is a different batch
	batch := "bat_" + hex.EncodeToString(scoped[:])[:16]
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT id FROM statement_batches WHERE account_id = ? AND file_hash = ?`, accountID, fileHash).Scan(&existing)
	if err == nil {
		return existing, 0, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", 0, err
	}
	cr := csv.NewReader(bytes.NewReader(data))
	if m.Delimiter != 0 {
		cr.Comma = m.Delimiter
	}
	cr.FieldsPerRecord = -1
	header, err := cr.Read()
	if err != nil {
		return "", 0, fmt.Errorf("read header: %w", err)
	}
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.TrimSpace(h)] = i
	}
	need := -1
	for _, col := range []string{m.DateCol, m.DescCol, m.CreditCol, m.DebitCol, m.UTRCol} {
		i, ok := idx[col]
		if !ok {
			return "", 0, fmt.Errorf("statement has no %q column", col)
		}
		need = max(need, i)
	}
	rows := 0
	for n := 1; ; n++ {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", 0, fmt.Errorf("row %d: %w", n, err)
		}
		if rows >= maxRows {
			return "", 0, errors.New("statement has too many rows")
		}
		if len(rec) <= need {
			return "", 0, fmt.Errorf("row %d: has %d columns, need at least %d", n, len(rec), need+1)
		}
		cell := func(col string) string { return rec[idx[col]] }
		when, err := time.Parse(m.DateLayout, strings.TrimSpace(cell(m.DateCol)))
		if err != nil {
			return "", 0, fmt.Errorf("row %d: bad date: %w", n, err)
		}
		credit, err := paise(cell(m.CreditCol))
		if err != nil {
			return "", 0, fmt.Errorf("row %d: credit: %w", n, err)
		}
		debit, err := paise(cell(m.DebitCol))
		if err != nil {
			return "", 0, fmt.Errorf("row %d: debit: %w", n, err)
		}
		utr := strings.TrimSpace(cell(m.UTRCol))
		if !regexpUTR(utr) {
			utr = ""
		}
		var utrVal any
		if utr != "" {
			utrVal = utr
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO statement_rows(id, batch_id, row_no, posted_on, description, credit_paise, debit_paise, utr)
			VALUES(?,?,?,?,?,?,?,?)`, fmt.Sprintf("%s_%06d", batch, n), batch, n, when.UTC().Format(time.RFC3339),
			cell(m.DescCol), credit, debit, utrVal); err != nil {
			return "", 0, err
		}
		rows++
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO statement_batches(id, account_id, file_hash, rows, imported_at) VALUES(?,?,?,?,?)`,
		batch, accountID, fileHash, rows, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return "", 0, err
	}
	return batch, rows, nil
}

func regexpUTR(s string) bool {
	if len(s) != 12 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// Pair is an order and the statement credit it matched.
type Pair struct {
	OrderID     string
	StatementID string
	AmountPaise int64
}

// Report is the three buckets plus amount mismatches.
type Report struct {
	Matched              []Pair
	ConfirmedButMissing  []string // confirmed orders with no statement credit: move these to disputed
	CreditedButUnmatched []string // statement credits with no order
	AmountMismatch       []Pair
}

// IDSource makes ids for future records (reconciliation actions).
type IDSource interface{ New(prefix string) string }

type Service struct {
	st  *store.Store
	sm  *states.Machine
	ids IDSource
}

func New(st *store.Store, sm *states.Machine, ids IDSource) *Service {
	return &Service{st: st, sm: sm, ids: ids}
}

type credit struct {
	id, utr, account string
	paise            int64
}

// Reconcile compares confirmed orders in [from, to) with the imported statement. It is read-only.
func (s *Service) Reconcile(ctx context.Context, from, to time.Time) (Report, error) {
	var rep Report
	credits := map[string]credit{} // by statement id
	used := map[string]bool{}
	rows, err := s.st.Read().QueryContext(ctx, `SELECT r.id, COALESCE(r.utr,''), b.account_id, r.credit_paise
		FROM statement_rows r JOIN statement_batches b ON b.id = r.batch_id WHERE r.credit_paise > 0 ORDER BY r.id`)
	if err != nil {
		return rep, err
	}
	var all []credit
	for rows.Next() {
		var c credit
		if err := rows.Scan(&c.id, &c.utr, &c.account, &c.paise); err != nil {
			rows.Close()
			return rep, err
		}
		credits[c.id] = c
		all = append(all, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return rep, fmt.Errorf("recon: read statement credits: %w", err)
	}
	if err := rows.Close(); err != nil {
		return rep, err
	}
	orders, err := s.st.Read().QueryContext(ctx, `SELECT o.id, o.account_id, o.amount_paise, COALESCE(a.utr,''), o.confirmed_at
		FROM orders o LEFT JOIN (SELECT m.order_id, al.utr FROM matches m JOIN alerts al ON al.id = m.alert_id WHERE m.decision = 'auto') a ON a.order_id = o.id
		WHERE o.state IN ('confirmed','late_paid','refunded','disputed') AND o.confirmed_at <> ''`)
	if err != nil {
		return rep, err
	}
	type paid struct {
		id, account, utr, at string
		paise                int64
	}
	var ps []paid
	for orders.Next() {
		var p paid
		if err := orders.Scan(&p.id, &p.account, &p.paise, &p.utr, &p.at); err != nil {
			orders.Close()
			return rep, err
		}
		ps = append(ps, p)
	}
	if err := orders.Err(); err != nil {
		orders.Close()
		return rep, fmt.Errorf("recon: read confirmed orders: %w", err)
	}
	if err := orders.Close(); err != nil {
		return rep, err
	}
	var inRange []paid
	for _, p := range ps {
		if at, err := time.Parse(time.RFC3339, p.at); err == nil && !at.Before(from) && at.Before(to) {
			inRange = append(inRange, p)
		}
	}
	// Credits only match orders of the same receiving account.
	// Pass 1 claims credits by UTR, so no amount fallback can take a credit that belongs to a UTR match.
	foundBy := map[string]string{}
	for _, p := range inRange {
		if p.utr == "" {
			continue
		}
		for _, c := range all {
			if !used[c.id] && c.account == p.account && c.utr == p.utr {
				used[c.id] = true
				foundBy[p.id] = c.id
				break
			}
		}
	}
	// Pass 2: exact amount. An order that has a UTR may only use a credit that carries none, since a credit with
	// another UTR is a different payment.
	for _, p := range inRange {
		if foundBy[p.id] != "" {
			continue
		}
		for _, c := range all {
			if !used[c.id] && c.account == p.account && c.paise == p.paise && (p.utr == "" || c.utr == "") {
				used[c.id] = true
				foundBy[p.id] = c.id
				break
			}
		}
	}
	for _, p := range inRange {
		found := foundBy[p.id]
		if found == "" {
			rep.ConfirmedButMissing = append(rep.ConfirmedButMissing, p.id)
			continue
		}
		c := credits[found]
		if c.paise != p.paise {
			rep.AmountMismatch = append(rep.AmountMismatch, Pair{OrderID: p.id, StatementID: found, AmountPaise: c.paise})
			continue
		}
		rep.Matched = append(rep.Matched, Pair{OrderID: p.id, StatementID: found, AmountPaise: p.paise})
	}
	for _, c := range all {
		if !used[c.id] {
			rep.CreditedButUnmatched = append(rep.CreditedButUnmatched, c.id)
		}
	}
	return rep, nil
}

// MarkDisputed moves a confirmed order to disputed. The owner does this from the reconciliation view.
func (s *Service) MarkDisputed(ctx context.Context, orderID string) error {
	return s.st.Tx(ctx, func(tx *sql.Tx) error {
		_, err := s.sm.Transition(ctx, tx, orderID, "disputed", map[string]any{"reason": "reconciliation"})
		return err
	})
}

// ValidateMapping parses every row with a mapping and reports the first row that fails. Nothing is stored.
func ValidateMapping(data []byte, m Mapping) (rows int, err error) {
	cr := csv.NewReader(bytes.NewReader(data))
	if m.Delimiter != 0 {
		cr.Comma = m.Delimiter
	}
	cr.FieldsPerRecord = -1
	header, err := cr.Read()
	if err != nil {
		return 0, fmt.Errorf("read header: %w", err)
	}
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.TrimSpace(h)] = i
	}
	for _, col := range []string{m.DateCol, m.DescCol, m.CreditCol, m.DebitCol, m.UTRCol} {
		if _, ok := idx[col]; !ok {
			return 0, fmt.Errorf("no %q column", col)
		}
	}
	for n := 1; ; n++ {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return rows, nil
		}
		if err != nil {
			return rows, fmt.Errorf("row %d: %w", n, err)
		}
		if _, err := time.Parse(m.DateLayout, strings.TrimSpace(rec[idx[m.DateCol]])); err != nil {
			return rows, fmt.Errorf("row %d: date does not match the layout", n)
		}
		if _, err := paise(rec[idx[m.CreditCol]]); err != nil {
			return rows, fmt.Errorf("row %d: credit is not an amount", n)
		}
		if _, err := paise(rec[idx[m.DebitCol]]); err != nil {
			return rows, fmt.Errorf("row %d: debit is not an amount", n)
		}
		rows++
	}
}
