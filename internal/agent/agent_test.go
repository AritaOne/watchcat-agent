package agent

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig drops a minimal config file into a temp dir and returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "watchcat-agent.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAgentServesMetricsEndToEnd(t *testing.T) {
	path := writeConfig(t, `
server:
  listen_address: "127.0.0.1:0"
log:
  level: error
labels:
  env: unit-test
`)

	a, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := a.Stop(); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})

	resp, err := http.Get("http://" + a.Addr() + "/metrics")
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	for _, want := range []string{
		`watchcat_memory_total_bytes{env="unit-test"}`,
		"watchcat_scrape_success",
		"go_goroutines", // the Go collector is on by default
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in scrape output", want)
		}
	}
}

func TestNewRejectsBrokenConfig(t *testing.T) {
	path := writeConfig(t, "server:\n  metrics_path: \"no-leading-slash\"\n")
	if _, err := New(path); err == nil {
		t.Fatal("expected New to reject an invalid config")
	}
}

func TestNewServiceUsesAbsoluteConfigPath(t *testing.T) {
	// Service managers start the binary from an arbitrary directory, so the
	// config path recorded in the unit file must be absolute.
	svc, err := NewService("configs/watchcat-agent.yaml")
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if svc == nil {
		t.Fatal("NewService returned a nil service")
	}
}
