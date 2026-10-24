package accounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/ledger"
	"github.com/opensawce/kaasu/internal/testutil"
)

func newSvc(t *testing.T) *Service {
	t.Helper()
	return New(testutil.OpenStore(t), clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), &testutil.Seq{})
}

func TestCreateAndGet(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	a, err := s.Create(ctx, Input{VPA: "sharma.sweets@okhdfcbank", Bank: "HDFC", DisplayName: "Sharma Sweets", AutoConfirmLimitPaise: 500000})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != "acc_000001" || !a.Active || a.CreatedAt != "2026-01-01T00:00:00.000000Z" {
		t.Errorf("unexpected account %+v", a)
	}
	got, err := s.Get(ctx, a.ID)
	if err != nil || got != a {
		t.Fatalf("get=%+v err=%v", got, err)
	}
	if _, err := s.Get(ctx, "acc_nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestValidation(t *testing.T) {
	s := newSvc(t)
	good := Input{VPA: "shop@okbank", Bank: "Bank", DisplayName: "Shop"}
	tests := []struct {
		name string
		mut  func(*Input)
	}{
		{"no at sign", func(i *Input) { i.VPA = "shopokbank" }},
		{"space in vpa", func(i *Input) { i.VPA = "my shop@okbank" }},
		{"handle too short", func(i *Input) { i.VPA = "shop@ab" }},
		{"empty name part", func(i *Input) { i.VPA = "@okbank" }},
		{"two at signs", func(i *Input) { i.VPA = "a@b@okbank" }},
		{"missing bank", func(i *Input) { i.Bank = " " }},
		{"missing display name", func(i *Input) { i.DisplayName = "" }},
		{"negative limit", func(i *Input) { i.AutoConfirmLimitPaise = -1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := good
			tt.mut(&in)
			if _, err := s.Create(context.Background(), in); !errors.Is(err, ErrInvalid) {
				t.Errorf("want ErrInvalid, got %v", err)
			}
		})
	}
}

func TestDuplicateVPAIsCaseInsensitive(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	if _, err := s.Create(ctx, Input{VPA: "shop@okbank", Bank: "B", DisplayName: "S"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, Input{VPA: "SHOP@OkBank", Bank: "B", DisplayName: "S2"}); !errors.Is(err, ErrDuplicate) {
		t.Errorf("want ErrDuplicate, got %v", err)
	}
}

func TestUpdateListDeactivate(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	a, _ := s.Create(ctx, Input{VPA: "a@okbank", Bank: "B", DisplayName: "A"})
	b, _ := s.Create(ctx, Input{VPA: "b@okbank", Bank: "B", DisplayName: "B"})
	up, err := s.Update(ctx, a.ID, Input{VPA: "a@okbank", Bank: "ICICI", DisplayName: "A shop", AutoConfirmLimitPaise: 100})
	if err != nil || up.Bank != "ICICI" || up.AutoConfirmLimitPaise != 100 {
		t.Fatalf("update: %+v %v", up, err)
	}
	if err := s.Deactivate(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	list, err := s.List(ctx)
	if err != nil || len(list) != 2 || list[0].ID != b.ID || list[1].Active {
		t.Errorf("active accounts should list first: %+v %v", list, err)
	}
}

func TestDeleteBlockedWhenOrdersExist(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	a, _ := s.Create(ctx, Input{VPA: "a@okbank", Bank: "B", DisplayName: "A"})
	if err := s.st.Tx(ctx, func(tx *sql.Tx) error {
		_, e := tx.Exec(`INSERT INTO orders(id,account_id,base_amount_paise,amount_paise,state,expires_at,created_at,updated_at)
			VALUES('o1',?,100,100,'awaiting','2026-01-01T00:15:00Z','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, a.ID)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, a.ID); !errors.Is(err, ErrInUse) {
		t.Errorf("want ErrInUse, got %v", err)
	}
	b, _ := s.Create(ctx, Input{VPA: "b@okbank", Bank: "B", DisplayName: "B"})
	if err := s.Delete(ctx, b.ID); err != nil {
		t.Errorf("unused account should delete: %v", err)
	}
}

func TestAdminChangesAreLedgered(t *testing.T) {
	s := newSvc(t)
	ctx := ledger.WithActor(context.Background(), "admin:priya")
	a, _ := s.Create(ctx, Input{VPA: "a@okbank", Bank: "B", DisplayName: "A"})
	_, _ = s.Update(ctx, a.ID, Input{VPA: "a@okbank", Bank: "B2", DisplayName: "A"})
	_ = s.Deactivate(ctx, a.ID)
	_ = s.Delete(ctx, a.ID)
	rows, err := s.st.Read().Query(`SELECT kind, actor FROM ledger ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var k, a string
		_ = rows.Scan(&k, &a)
		got = append(got, k+"/"+a)
	}
	want := []string{"account.created/admin:priya", "account.updated/admin:priya", "account.deactivated/admin:priya", "account.deleted/admin:priya"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("got %v want %v", got, want)
	}
}
