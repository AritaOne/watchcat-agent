package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/aritaone/watchcat-agent/internal/config"
)

func newTestServer(t *testing.T, mutate func(*config.Config)) (*Server, *prometheus.Registry) {
	t.Helper()
	cfg := config.Default()
	cfg.Server.ListenAddress = "127.0.0.1:0"
	if mutate != nil {
		mutate(cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config invalid: %v", err)
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "watchcat_test_metric", Help: "test",
	}))

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(cfg, reg, log), reg
}

func do(t *testing.T, s *Server, method, target string, header map[string]string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.http.Handler.ServeHTTP(rec, req)
	return rec.Result()
}

func TestMetricsEndpointServesRegistry(t *testing.T) {
	s, _ := newTestServer(t, nil)

	resp := do(t, s, http.MethodGet, "/metrics", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "watchcat_test_metric") {
		t.Errorf("registry metric missing from response:\n%s", body)
	}
}

func TestHealthz(t *testing.T) {
	s, _ := newTestServer(t, nil)

	resp := do(t, s, http.MethodGet, "/healthz", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestBearerTokenGuardsMetricsOnly(t *testing.T) {
	s, _ := newTestServer(t, func(c *config.Config) { c.Server.BearerToken = "s3cret" })

	cases := []struct {
		name   string
		path   string
		header map[string]string
		want   int
	}{
		{"no header", "/metrics", nil, http.StatusUnauthorized},
		{"wrong token", "/metrics", map[string]string{"Authorization": "Bearer nope"}, http.StatusUnauthorized},
		{"wrong scheme", "/metrics", map[string]string{"Authorization": "Basic s3cret"}, http.StatusUnauthorized},
		{"correct token", "/metrics", map[string]string{"Authorization": "Bearer s3cret"}, http.StatusOK},
		// Health checks stay open so load balancers do not need the secret.
		{"healthz open", "/healthz", nil, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := do(t, s, http.MethodGet, tc.path, tc.header)
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.want)
			}
		})
	}
}

func TestCustomMetricsPath(t *testing.T) {
	s, _ := newTestServer(t, func(c *config.Config) { c.Server.MetricsPath = "/internal/metrics" })

	resp := do(t, s, http.MethodGet, "/internal/metrics", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("custom path status = %d, want 200", resp.StatusCode)
	}

	missing := do(t, s, http.MethodGet, "/metrics", nil)
	defer missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Errorf("default path status = %d, want 404", missing.StatusCode)
	}
}

func TestIndexPage(t *testing.T) {
	s, _ := newTestServer(t, nil)
	s.SetCollectors([]string{"cpu", "memory"})

	resp := do(t, s, http.MethodGet, "/", nil)
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "watchcat-agent") || !strings.Contains(string(body), "cpu, memory") {
		t.Errorf("unexpected index page:\n%s", body)
	}
}

func TestUnknownPathIs404(t *testing.T) {
	s, _ := newTestServer(t, nil)

	resp := do(t, s, http.MethodGet, "/secrets", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestStartAndShutdown(t *testing.T) {
	s, _ := newTestServer(t, nil)

	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Port 0 means the OS picked one; Addr must report the real address.
	if strings.HasSuffix(s.Addr(), ":0") {
		t.Fatalf("Addr = %q, want a bound port", s.Addr())
	}

	resp, err := http.Get("http://" + s.Addr() + "/healthz")
	if err != nil {
		t.Fatalf("GET healthz: %v", err)
	}
	resp.Body.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}
