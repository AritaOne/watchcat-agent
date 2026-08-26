package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultIsValid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}
}

func TestLoadMissingFileFallsBackToDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.MetricsPath != "/metrics" {
		t.Errorf("metrics path = %q, want /metrics", cfg.Server.MetricsPath)
	}
}

func TestLoadYAMLOverlaysDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	body := `
server:
  listen_address: "127.0.0.1:1234"
  scrape_timeout: 3s
collectors:
  network: false
labels:
  env: staging
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.ListenAddress != "127.0.0.1:1234" {
		t.Errorf("listen = %q", cfg.Server.ListenAddress)
	}
	if cfg.Server.ScrapeTimeout != 3*time.Second {
		t.Errorf("scrape timeout = %v", cfg.Server.ScrapeTimeout)
	}
	if cfg.Collectors.Network {
		t.Error("network collector should be disabled")
	}
	// Untouched keys must keep their defaults.
	if !cfg.Collectors.CPU {
		t.Error("cpu collector should stay enabled")
	}
	if cfg.Labels["env"] != "staging" {
		t.Errorf("labels = %v", cfg.Labels)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	t.Setenv("WATCHCAT_LISTEN_ADDRESS", "0.0.0.0:9999")
	t.Setenv("WATCHCAT_BEARER_TOKEN", "tok")
	t.Setenv("WATCHCAT_LABELS", "env=prod, role=api")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.ListenAddress != "0.0.0.0:9999" {
		t.Errorf("listen = %q", cfg.Server.ListenAddress)
	}
	if cfg.Server.BearerToken != "tok" {
		t.Errorf("token = %q", cfg.Server.BearerToken)
	}
	if cfg.Labels["env"] != "prod" || cfg.Labels["role"] != "api" {
		t.Errorf("labels = %v", cfg.Labels)
	}
}

func TestValidateRejectsBadInput(t *testing.T) {
	tests := map[string]func(*Config){
		"empty listen address":  func(c *Config) { c.Server.ListenAddress = "" },
		"relative metrics path": func(c *Config) { c.Server.MetricsPath = "metrics" },
		"half a TLS pair":       func(c *Config) { c.Server.TLSCertFile = "cert.pem" },
		"zero scrape timeout":   func(c *Config) { c.Server.ScrapeTimeout = 0 },
		"broken regexp":         func(c *Config) { c.Collectors.NetworkIgnoreDevices = "([" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := Default()
			mutate(cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}

func TestCompileIgnorePatterns(t *testing.T) {
	cfg := Default()
	c, err := cfg.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !c.IgnoreNetDevices.MatchString("lo") {
		t.Error("loopback should be ignored by default")
	}
	if c.IgnoreNetDevices.MatchString("eth0") {
		t.Error("eth0 should not be ignored")
	}
	if !c.IgnoreFSTypes.MatchString("tmpfs") && !c.IgnoreFSTypes.MatchString("sysfs") {
		t.Error("pseudo filesystems should be ignored")
	}
}

func TestTLSEnabled(t *testing.T) {
	cfg := Default()
	if cfg.TLSEnabled() {
		t.Error("TLS should be off by default")
	}
	cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile = "c.pem", "k.pem"
	if !cfg.TLSEnabled() {
		t.Error("TLS should be on when both files are set")
	}
}
