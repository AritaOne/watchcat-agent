package config

import (
	"os"
	"path/filepath"
	"slices"
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
	// Loopback is "lo" on Linux and "lo0" on macOS and the BSDs; both must go.
	for _, dev := range []string{"lo", "lo0", "lo1"} {
		if !c.IgnoreNetDevices.MatchString(dev) {
			t.Errorf("loopback device %q should be ignored by default", dev)
		}
	}
	// Real interfaces must survive, including ones that merely start with "lo".
	for _, dev := range []string{"eth0", "en0", "ens5", "lolcat0"} {
		if c.IgnoreNetDevices.MatchString(dev) {
			t.Errorf("%q should not be ignored", dev)
		}
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

func TestCertificatePathsFromYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	body := `
collectors:
  certificate_paths:
    - /etc/ssl/site.pem
    - /etc/nginx/certs/*.pem
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"/etc/ssl/site.pem", "/etc/nginx/certs/*.pem"}
	if got := cfg.Collectors.CertificatePaths; !slices.Equal(got, want) {
		t.Errorf("certificate_paths = %v, want %v", got, want)
	}
	if !cfg.Collectors.Certificate {
		t.Error("certificate collector should be on by default")
	}
}

func TestCertificatePathsFromEnv(t *testing.T) {
	t.Setenv("WATCHCAT_CERTIFICATE_PATHS", "/etc/ssl/a.pem, /etc/ssl/b.pem ,")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"/etc/ssl/a.pem", "/etc/ssl/b.pem"}
	if got := cfg.Collectors.CertificatePaths; !slices.Equal(got, want) {
		t.Errorf("certificate_paths = %v, want %v", got, want)
	}
}

func TestValidateRejectsBadCertificateGlob(t *testing.T) {
	cfg := Default()
	cfg.Collectors.CertificatePaths = []string{"/etc/ssl/[.pem"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected a validation error for a malformed glob")
	}
}

func TestCustomDir(t *testing.T) {
	// A temp dir is absolute on every OS; "/etc/..." has no drive letter, so
	// Windows does not consider it absolute. Single quotes keep Windows
	// backslashes literal in YAML.
	dir := filepath.Join(t.TempDir(), "custom.d")
	path := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(path, []byte("collectors:\n  custom_dir: '"+dir+"'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Collectors.CustomDir; got != dir {
		t.Errorf("custom_dir = %q", got)
	}

	env := filepath.Join(t.TempDir(), "custom.d")
	t.Setenv("WATCHCAT_CUSTOM_DIR", env)
	if cfg, err = Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Collectors.CustomDir; got != env {
		t.Errorf("env should override file: custom_dir = %q, want %q", got, env)
	}
}

func TestValidateRejectsRelativeCustomDir(t *testing.T) {
	cfg := Default()
	cfg.Collectors.CustomDir = "custom.d"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected a validation error for a relative custom_dir")
	}
}
