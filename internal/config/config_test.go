package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDefaults(t *testing.T) {
	c, err := Load(env(nil), "")
	if err != nil {
		t.Fatal(err)
	}
	if c.PublicAddr != ":8080" || c.IngestAddr != ":8443" || c.AdminAddr != "127.0.0.1:9000" {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	if c.DatabasePath != "kaasu.db" || c.LogLevel != "info" {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kaasu.json")
	if err := os.WriteFile(path, []byte(`{"public_addr":":9090","database_path":"from-file.db"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(env(map[string]string{"KAASU_PUBLIC_ADDR": ":7070"}), path)
	if err != nil {
		t.Fatal(err)
	}
	if c.PublicAddr != ":7070" {
		t.Errorf("env should beat file, got %q", c.PublicAddr)
	}
	if c.DatabasePath != "from-file.db" {
		t.Errorf("file value lost, got %q", c.DatabasePath)
	}
}

func TestAdminMustBeLoopback(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		allow   string
		wantErr bool
	}{
		{"loopback v4", "127.0.0.1:9000", "", false},
		{"loopback v6", "[::1]:9000", "", false},
		{"localhost", "localhost:9000", "", false},
		{"all interfaces", ":9000", "", true},
		{"public ip", "203.0.113.5:9000", "", true},
		{"vpn ip with explicit allow", "10.8.0.2:9000", "1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(env(map[string]string{
				"KAASU_ADMIN_ADDR":               tt.addr,
				"KAASU_ADMIN_ALLOW_NON_LOOPBACK": tt.allow,
			}), "")
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestRejectsUnknownFileKeysAndBadLevel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kaasu.json")
	if err := os.WriteFile(path, []byte(`{"public_adr":":1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(env(nil), path); err == nil || !strings.Contains(err.Error(), "public_adr") {
		t.Errorf("typo in config file should be reported, got %v", err)
	}
	if _, err := Load(env(map[string]string{"KAASU_LOG_LEVEL": "shout"}), ""); err == nil {
		t.Error("bad log level should fail")
	}
}
