// Package config loads Kaasu settings from an optional JSON file and KAASU_* environment variables.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
)

// Config holds process-wide settings. Money and secrets never live here.
type Config struct {
	PublicAddr            string `json:"public_addr"`
	IngestAddr            string `json:"ingest_addr"`
	AdminAddr             string `json:"admin_addr"`
	AdminAllowNonLoopback bool   `json:"admin_allow_non_loopback"`
	DatabasePath          string `json:"database_path"`
	LogLevel              string `json:"log_level"`
}

func defaults() Config {
	return Config{
		PublicAddr:   ":8080",
		IngestAddr:   ":8443",
		AdminAddr:    "127.0.0.1:9000",
		DatabasePath: "kaasu.db",
		LogLevel:     "info",
	}
}

// Load reads path (if not empty), then lets KAASU_* variables override it, then validates.
func Load(getenv func(string) string, path string) (Config, error) {
	c := defaults()
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("read config file: %w", err)
		}
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&c); err != nil {
			return Config{}, fmt.Errorf("parse config file %s: %w", path, err)
		}
	}
	str := func(dst *string, key string) {
		if v := getenv(key); v != "" {
			*dst = v
		}
	}
	str(&c.PublicAddr, "KAASU_PUBLIC_ADDR")
	str(&c.IngestAddr, "KAASU_INGEST_ADDR")
	str(&c.AdminAddr, "KAASU_ADMIN_ADDR")
	str(&c.DatabasePath, "KAASU_DATABASE_PATH")
	str(&c.LogLevel, "KAASU_LOG_LEVEL")
	if v := getenv("KAASU_ADMIN_ALLOW_NON_LOOPBACK"); v != "" {
		c.AdminAllowNonLoopback = v == "1" || strings.EqualFold(v, "true")
	}
	return c, c.validate()
}

func (c Config) validate() error {
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log level %q: want debug, info, warn or error", c.LogLevel)
	}
	if !c.AdminAllowNonLoopback && !isLoopback(c.AdminAddr) {
		return fmt.Errorf("admin address %q is not loopback; set admin_allow_non_loopback only behind a VPN", c.AdminAddr)
	}
	return nil
}

func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
