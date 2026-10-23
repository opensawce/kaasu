package recon

import (
	"bytes"
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/opensawce/kaasu/internal/testutil"
)

func FuzzImportCSV(f *testing.F) {
	f.Add("Date,Narration,Withdrawal Amt.,Deposit Amt.,Reference No.\n23/10/2026,x,,1.00,123456789012\n")
	f.Fuzz(func(t *testing.T, data string) {
		st := testutil.OpenStore(t)
		_ = st.Tx(context.Background(), func(tx *sql.Tx) error {
			_, _, _ = ImportCSV(context.Background(), tx, bytes.NewReader([]byte(data)), hdfcMapping())
			return nil
		})
		var n int
		_ = st.Read().QueryRow(`SELECT count(*) FROM statement_rows WHERE credit_paise < 0 OR debit_paise < 0`).Scan(&n)
		if n != 0 {
			t.Fatal("negative amounts stored")
		}
		_ = strings.TrimSpace
	})
}
