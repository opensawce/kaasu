// Package accounts manages the UPI IDs a shop receives money on.
package accounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/ledger"
	"github.com/opensawce/kaasu/internal/store"
)

var (
	ErrNotFound  = errors.New("receiving account not found")
	ErrInvalid   = errors.New("invalid receiving account")
	ErrDuplicate = errors.New("a receiving account with this UPI ID already exists")
	ErrInUse     = errors.New("receiving account has orders; deactivate it instead")
)

var vpaRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,255}@[a-z][a-z0-9]{2,63}$`)

// IDSource makes ids; testutil.Seq and a random source both satisfy it.
type IDSource interface{ New(prefix string) string }

// Account is one receiving UPI ID. VPAs are stored lower-case.
type Account struct {
	ID                    string
	VPA                   string
	Bank                  string
	DisplayName           string
	AutoConfirmLimitPaise int64 // orders above this always need the owner's OK
	Active                bool
	CreatedAt             string
}

// Input is what the owner provides when creating or editing an account.
type Input struct {
	VPA                   string
	Bank                  string
	DisplayName           string
	AutoConfirmLimitPaise int64
}

type Service struct {
	st  *store.Store
	clk clock.Clock
	ids IDSource
}

func New(st *store.Store, clk clock.Clock, ids IDSource) *Service {
	return &Service{st: st, clk: clk, ids: ids}
}

// record writes the admin action to the ledger in the same transaction. Ledger rows have no order, so the account id goes in the payload.
func (s *Service) record(ctx context.Context, tx *sql.Tx, kind, accountID string, extra map[string]any) error {
	p := map[string]any{"account_id": accountID}
	for k, v := range extra {
		p[k] = v
	}
	_, err := ledger.Append(ctx, tx, s.clk, ledger.Entry{Kind: kind, Payload: p})
	return err
}

func (in *Input) normalise() error {
	in.VPA = strings.ToLower(strings.TrimSpace(in.VPA))
	in.Bank = strings.TrimSpace(in.Bank)
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	switch {
	case !vpaRe.MatchString(in.VPA):
		return fmt.Errorf("%w: UPI ID %q is not name@handle", ErrInvalid, in.VPA)
	case in.Bank == "":
		return fmt.Errorf("%w: bank is required", ErrInvalid)
	case in.DisplayName == "":
		return fmt.Errorf("%w: display name is required", ErrInvalid)
	case in.AutoConfirmLimitPaise < 0:
		return fmt.Errorf("%w: auto-confirm limit cannot be negative", ErrInvalid)
	}
	return nil
}

const cols = `id, vpa, bank, display_name, auto_confirm_limit_paise, active, created_at`

func scan(r interface{ Scan(...any) error }) (Account, error) {
	var a Account
	var active int
	err := r.Scan(&a.ID, &a.VPA, &a.Bank, &a.DisplayName, &a.AutoConfirmLimitPaise, &active, &a.CreatedAt)
	a.Active = active == 1
	return a, err
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func (s *Service) Create(ctx context.Context, in Input) (Account, error) {
	if err := in.normalise(); err != nil {
		return Account{}, err
	}
	a := Account{ID: s.ids.New("acc"), VPA: in.VPA, Bank: in.Bank, DisplayName: in.DisplayName,
		AutoConfirmLimitPaise: in.AutoConfirmLimitPaise, Active: true, CreatedAt: s.clk.Now().UTC().Format("2006-01-02T15:04:05.000000Z")}
	err := s.st.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO receiving_accounts(`+cols+`) VALUES(?,?,?,?,?,1,?)`,
			a.ID, a.VPA, a.Bank, a.DisplayName, a.AutoConfirmLimitPaise, a.CreatedAt)
		if err != nil {
			return err
		}
		return s.record(ctx, tx, "account.created", a.ID, map[string]any{"vpa": a.VPA, "bank": a.Bank, "auto_confirm_limit_paise": a.AutoConfirmLimitPaise})
	})
	if isUnique(err) {
		return Account{}, ErrDuplicate
	}
	return a, err
}

func (s *Service) Get(ctx context.Context, id string) (Account, error) {
	a, err := scan(s.st.Read().QueryRowContext(ctx, `SELECT `+cols+` FROM receiving_accounts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	return a, err
}

// List returns active accounts first, then inactive, oldest first within each group.
func (s *Service) List(ctx context.Context) ([]Account, error) {
	rows, err := s.st.Read().QueryContext(ctx, `SELECT `+cols+` FROM receiving_accounts ORDER BY active DESC, created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Service) Update(ctx context.Context, id string, in Input) (Account, error) {
	if err := in.normalise(); err != nil {
		return Account{}, err
	}
	err := s.st.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE receiving_accounts SET vpa=?, bank=?, display_name=?, auto_confirm_limit_paise=? WHERE id=?`,
			in.VPA, in.Bank, in.DisplayName, in.AutoConfirmLimitPaise, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return s.record(ctx, tx, "account.updated", id, map[string]any{"vpa": in.VPA, "bank": in.Bank, "auto_confirm_limit_paise": in.AutoConfirmLimitPaise})
	})
	if isUnique(err) {
		return Account{}, ErrDuplicate
	}
	if err != nil {
		return Account{}, err
	}
	return s.Get(ctx, id)
}

// Deactivate stops new orders on the account; existing orders keep working.
func (s *Service) Deactivate(ctx context.Context, id string) error {
	return s.st.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE receiving_accounts SET active=0 WHERE id=?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return s.record(ctx, tx, "account.deactivated", id, nil)
	})
}

// Delete removes an account that never had orders.
func (s *Service) Delete(ctx context.Context, id string) error {
	return s.st.Tx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM orders WHERE account_id = ?`, id).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return ErrInUse
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM receiving_accounts WHERE id=?`, id)
		if err != nil {
			return err
		}
		if c, _ := res.RowsAffected(); c == 0 {
			return ErrNotFound
		}
		return s.record(ctx, tx, "account.deleted", id, nil)
	})
}
