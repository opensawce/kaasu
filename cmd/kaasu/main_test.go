package main

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/opensawce/kaasu/internal/clock"
	"github.com/opensawce/kaasu/internal/ledger"
	"github.com/opensawce/kaasu/internal/store"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string
		wantErr  string
	}{
		{"version", []string{"version"}, 0, "kaasu ", ""},
		{"no command", nil, 2, "", "usage:"},
		{"unknown command", []string{"frobnicate"}, 2, "", "unknown command"},
		{"stub pair", []string{"pair"}, 1, "", "not implemented"},
		{"stub backup", []string{"backup"}, 1, "", "not implemented"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			code := run(tt.args, func(string) string { return "" }, &out, &errb)
			if code != tt.wantCode {
				t.Errorf("code=%d want %d (stderr=%q)", code, tt.wantCode, errb.String())
			}
			if !strings.Contains(out.String(), tt.wantOut) {
				t.Errorf("stdout=%q want %q", out.String(), tt.wantOut)
			}
			if !strings.Contains(errb.String(), tt.wantErr) {
				t.Errorf("stderr=%q want %q", errb.String(), tt.wantErr)
			}
		})
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	db := filepath.Join(t.TempDir(), "k.db")
	getenv := func(k string) string {
		if k == "KAASU_DATABASE_PATH" {
			return db
		}
		return ""
	}
	for i := 0; i < 2; i++ {
		var out, errb bytes.Buffer
		if code := run([]string{"migrate"}, getenv, &out, &errb); code != 0 {
			t.Fatalf("run %d: code=%d stderr=%q", i, code, errb.String())
		}
		if !strings.Contains(out.String(), "schema up to date") {
			t.Errorf("run %d: stdout=%q", i, out.String())
		}
	}
}

func TestKeygenAndRotateKey(t *testing.T) {
	dir := t.TempDir()
	envs := map[string]string{"KAASU_DATABASE_PATH": filepath.Join(dir, "k.db")}
	getenv := func(k string) string { return envs[k] }

	var out, errb bytes.Buffer
	if code := run([]string{"keygen"}, getenv, &out, &errb); code != 0 {
		t.Fatalf("keygen code=%d %q", code, errb.String())
	}
	oldKey := strings.TrimSpace(out.String())
	out.Reset()
	run([]string{"keygen"}, getenv, &out, &errb)
	newKey := strings.TrimSpace(out.String())
	if oldKey == newKey || len(oldKey) < 40 {
		t.Fatalf("keys must be random and long: %q %q", oldKey, newKey)
	}

	envs["KAASU_MASTER_KEY"] = oldKey
	if code := run([]string{"migrate"}, getenv, &out, &errb); code != 0 {
		t.Fatalf("migrate: %q", errb.String())
	}
	out.Reset()
	if code := run([]string{"rotate-key"}, getenv, &out, &errb); code != 1 || !strings.Contains(errb.String(), "KAASU_NEW_MASTER_KEY") {
		t.Errorf("rotate-key without a new key should explain: code=%d err=%q", code, errb.String())
	}
	envs["KAASU_NEW_MASTER_KEY"] = newKey
	errb.Reset()
	if code := run([]string{"rotate-key"}, getenv, &out, &errb); code != 0 || !strings.Contains(out.String(), "rotated 0 secrets") {
		t.Errorf("rotate-key: code=%d out=%q err=%q", code, out.String(), errb.String())
	}
}

func TestVerifyDetectsTampering(t *testing.T) {
	db := filepath.Join(t.TempDir(), "k.db")
	getenv := func(k string) string {
		if k == "KAASU_DATABASE_PATH" {
			return db
		}
		return ""
	}
	var out, errb bytes.Buffer
	if code := run([]string{"verify"}, getenv, &out, &errb); code != 0 || !strings.Contains(out.String(), "ledger intact: 0 rows") {
		t.Fatalf("fresh db: code=%d out=%q err=%q", code, out.String(), errb.String())
	}
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	for i := 0; i < 3; i++ {
		_ = st.Tx(context.Background(), func(tx *sql.Tx) error {
			_, err := ledger.Append(context.Background(), tx, clk, ledger.Entry{Kind: "order.created", OrderID: "ord_1"})
			return err
		})
	}
	out.Reset()
	if code := run([]string{"verify"}, getenv, &out, &errb); code != 0 || !strings.Contains(out.String(), "3 rows") {
		t.Fatalf("clean ledger: code=%d out=%q", code, out.String())
	}
	_ = st.Tx(context.Background(), func(tx *sql.Tx) error {
		_, _ = tx.Exec(`DROP TRIGGER ledger_no_update`)
		_, err := tx.Exec(`UPDATE ledger SET actor='mallory' WHERE seq=2`)
		return err
	})
	st.Close()
	out.Reset()
	code := run([]string{"verify"}, getenv, &out, &errb)
	if code != 1 || !strings.Contains(out.String(), "BROKEN at row 2") {
		t.Errorf("tampered: code=%d out=%q", code, out.String())
	}
}

func TestAccountAndAPIKeyCommands(t *testing.T) {
	var kb bytes.Buffer
	run([]string{"keygen"}, func(string) string { return "" }, &kb, &bytes.Buffer{})
	envs := map[string]string{"KAASU_DATABASE_PATH": filepath.Join(t.TempDir(), "k.db"), "KAASU_MASTER_KEY": strings.TrimSpace(kb.String())}
	getenv := func(k string) string { return envs[k] }

	var out, errb bytes.Buffer
	if code := run([]string{"account", "add", "--vpa", "shop@okbank", "--bank", "HDFC", "--name", "Shop", "--limit-rupees", "5000"}, getenv, &out, &errb); code != 0 || !strings.Contains(out.String(), "acc_") {
		t.Fatalf("account add: code=%d out=%q err=%q", code, out.String(), errb.String())
	}
	out.Reset()
	if code := run([]string{"account", "add", "--vpa", "bad", "--bank", "x", "--name", "y"}, getenv, &out, &errb); code != 1 {
		t.Errorf("invalid vpa should fail, code=%d", code)
	}
	out.Reset()
	if code := run([]string{"apikey", "new", "WooCommerce"}, getenv, &out, &errb); code != 0 || !strings.Contains(out.String(), "kaasu_live_") || !strings.Contains(out.String(), "shown once") {
		t.Fatalf("apikey new: code=%d out=%q", code, out.String())
	}
}

func TestAdminInitPrintsEnrollmentOnce(t *testing.T) {
	var kb bytes.Buffer
	run([]string{"keygen"}, func(string) string { return "" }, &kb, &bytes.Buffer{})
	envs := map[string]string{"KAASU_DATABASE_PATH": filepath.Join(t.TempDir(), "k.db"), "KAASU_MASTER_KEY": strings.TrimSpace(kb.String())}
	getenv := func(k string) string { return envs[k] }
	var out, errb bytes.Buffer
	if code := run([]string{"admin", "init", "--name", "priya"}, getenv, &out, &errb); code != 0 || !strings.Contains(out.String(), "otpauth://totp/Kaasu:priya?secret=") {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errb.String())
	}
	out.Reset()
	if code := run([]string{"admin", "init", "--name", "priya"}, getenv, &out, &errb); code == 0 {
		t.Error("creating the same admin twice must fail")
	}
}

func TestDoctorFailsWithoutKeyAndPassesWithOne(t *testing.T) {
	var out, errb bytes.Buffer
	envs := map[string]string{"KAASU_DATABASE_PATH": filepath.Join(t.TempDir(), "d.db")}
	getenv := func(k string) string { return envs[k] }
	if code := run([]string{"doctor"}, getenv, &out, &errb); code != 1 || !strings.Contains(out.String(), "FAIL master key") {
		t.Errorf("no key should FAIL: code=%d out=%q", code, out.String())
	}
	var kb bytes.Buffer
	run([]string{"keygen"}, func(string) string { return "" }, &kb, &bytes.Buffer{})
	envs["KAASU_MASTER_KEY"] = strings.TrimSpace(kb.String())
	out.Reset()
	if code := run([]string{"doctor"}, getenv, &out, &errb); code != 0 || strings.Contains(out.String(), "FAIL") {
		t.Errorf("healthy install: code=%d out=%q", code, out.String())
	}
}
