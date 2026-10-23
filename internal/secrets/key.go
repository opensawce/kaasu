package secrets

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// LoadMasterKey finds the master key: KAASU_MASTER_KEY, then a systemd credential, then the OS keychain.
// It returns where the key came from so the operator can see it in the logs.
func LoadMasterKey(getenv func(string) string, keychain func() (string, error)) (MasterKey, string, error) {
	if v := getenv("KAASU_MASTER_KEY"); v != "" {
		k, err := ParseMasterKey(v)
		return k, "env", err
	}
	if dir := getenv("CREDENTIALS_DIRECTORY"); dir != "" {
		if b, err := os.ReadFile(filepath.Join(dir, "kaasu-master-key")); err == nil {
			k, err := ParseMasterKey(string(b))
			return k, "systemd", err
		}
	}
	if keychain != nil {
		if v, err := keychain(); err == nil && v != "" {
			k, err := ParseMasterKey(v)
			return k, "keychain", err
		}
	}
	return nil, "", errors.New("no master key found: set KAASU_MASTER_KEY, provide a systemd credential named kaasu-master-key, or store one in the OS keychain (generate with: kaasu keygen)")
}

// MacKeychain reads the key from the macOS login keychain (service "kaasu", account "master-key").
// On other systems it reports not found, so the other sources are used.
func MacKeychain() (string, error) {
	if runtime.GOOS != "darwin" {
		return "", os.ErrNotExist
	}
	out, err := exec.Command("/usr/bin/security", "find-generic-password", "-s", "kaasu", "-a", "master-key", "-w").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
