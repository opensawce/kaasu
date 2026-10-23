package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
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
