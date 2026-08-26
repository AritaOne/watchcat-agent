package collector

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/aritaone/watchcat-agent/internal/config"
	"github.com/prometheus/client_golang/prometheus"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func gather(t *testing.T, cfg *config.Config) (*Collector, string) {
	t.Helper()
	c, err := New(cfg, testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	reg := prometheus.NewPedanticRegistry()
	if err := reg.Register(c); err != nil {
		t.Fatalf("Register: %v", err)
	}
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	var sb strings.Builder
	for _, mf := range families {
		sb.WriteString(mf.GetName())
		sb.WriteString("\n")
	}
	return c, sb.String()
}

func TestCollectEmitsCoreMetrics(t *testing.T) {
	_, names := gather(t, config.Default())

	// These sources work on every platform the agent targets.
	for _, want := range []string{
		"watchcat_agent_build_info",
		"watchcat_cpu_seconds_total",
		"watchcat_memory_total_bytes",
		"watchcat_host_info",
		"watchcat_scrape_success",
		"watchcat_scrape_duration_seconds",
	} {
		if !strings.Contains(names, want) {
			t.Errorf("missing metric %s in:\n%s", want, names)
		}
	}
}

func TestEveryCollectorSucceeds(t *testing.T) {
	c, err := New(config.Default(), testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(c)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}

	success := map[string]float64{}
	for _, mf := range families {
		if mf.GetName() != "watchcat_scrape_success" {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "collector" {
					success[l.GetValue()] = m.GetGauge().GetValue()
				}
			}
		}
	}

	for _, name := range c.Enabled() {
		got, ok := success[name]
		if !ok {
			t.Errorf("collector %q reported no scrape_success metric", name)
			continue
		}
		if got != 1 {
			t.Errorf("collector %q reported scrape_success=%v", name, got)
		}
	}
}

func TestConstLabelsApplied(t *testing.T) {
	cfg := config.Default()
	cfg.Labels = map[string]string{"env": "test"}

	c, err := New(cfg, testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(c)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, mf := range families {
		if mf.GetName() != "watchcat_memory_total_bytes" {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "env" && l.GetValue() == "test" {
					return
				}
			}
		}
	}
	t.Fatal("constant label env=test not found on watchcat_memory_total_bytes")
}

func TestDisabledCollectorsAreNotRun(t *testing.T) {
	cfg := config.Default()
	cfg.Collectors = config.Collectors{Memory: true}

	c, names := gather(t, cfg)
	if got := c.Enabled(); len(got) != 1 || got[0] != "memory" {
		t.Fatalf("enabled = %v, want [memory]", got)
	}
	if strings.Contains(names, "watchcat_cpu_seconds_total") {
		t.Error("cpu metrics present although the collector is disabled")
	}
}

func TestNewRejectsBadRegexp(t *testing.T) {
	cfg := config.Default()
	cfg.Collectors.DiskIOIgnoreDevices = "(["
	if _, err := New(cfg, testLogger()); err == nil {
		t.Fatal("expected an error for an invalid ignore pattern")
	}
}
