package systemd

import (
	"os"
	"strings"
	"testing"
)

// Every hardening line the design asks for must be present in the unit.
func TestUnitFileHardening(t *testing.T) {
	b, err := os.ReadFile("kaasu.service")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"User=kaasu", "LoadCredential=kaasu-master-key:/etc/kaasu/master.key", "NoNewPrivileges=yes",
		"ProtectSystem=strict", "ProtectHome=yes", "PrivateTmp=yes", "PrivateDevices=yes",
		"ProtectKernelTunables=yes", "ProtectKernelModules=yes", "ProtectControlGroups=yes",
		"RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX", "RestrictNamespaces=yes", "LockPersonality=yes",
		"MemoryDenyWriteExecute=yes", "CapabilityBoundingSet=CAP_NET_BIND_SERVICE", "SystemCallFilter=@system-service",
		"Restart=on-failure",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("unit is missing %q", want)
		}
	}
	if strings.Contains(s, "Environment=KAASU_MASTER_KEY") {
		t.Error("the master key must come from a credential, never the environment")
	}
}
