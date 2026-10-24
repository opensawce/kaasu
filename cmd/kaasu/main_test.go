package main

import (
	"bytes"
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
