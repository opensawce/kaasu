package railway

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestRailwayJSONValid(t *testing.T) {
	b, err := os.ReadFile("railway.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Deploy struct {
			NumReplicas     int    `json:"numReplicas"`
			HealthcheckPath string `json:"healthcheckPath"`
		} `json:"deploy"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Deploy.NumReplicas != 1 {
		t.Errorf("replicas=%d; SQLite allows one writer", cfg.Deploy.NumReplicas)
	}
	if cfg.Deploy.HealthcheckPath != "/healthz" {
		t.Errorf("healthcheck=%s", cfg.Deploy.HealthcheckPath)
	}
}

func TestRailwayReadmeHasRequiredSections(t *testing.T) {
	b, _ := os.ReadFile("README.md")
	for _, s := range []string{"## Services", "## Variables", "## Domains", "## Checks after deploy", "One replica only"} {
		if !strings.Contains(string(b), s) {
			t.Errorf("README missing %q", s)
		}
	}
}
