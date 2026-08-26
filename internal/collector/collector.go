// Package collector turns host telemetry into Prometheus metrics.
//
// Metrics are gathered lazily, once per scrape, so an idle agent costs
// almost nothing. Each sub-collector is independent: one failing source
// (an unreadable mount point, say) never blocks the rest of the scrape.
package collector

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/aritaone/watchcat-agent/internal/buildinfo"
	"github.com/aritaone/watchcat-agent/internal/config"
)

// Namespace prefixes every metric this agent exports.
const Namespace = "watchcat"

// Subcollector is one source of metrics, e.g. cpu or filesystem.
type Subcollector interface {
	// Name is the identifier used in logs and in the per-collector meta metrics.
	Name() string
	// Update sends the current values for this source to ch.
	Update(ctx context.Context, ch chan<- prometheus.Metric) error
}

// Collector fans a scrape out across every enabled sub-collector.
// It implements prometheus.Collector as an unchecked collector, which lets
// sub-collectors emit a variable set of series (mount points come and go).
type Collector struct {
	subs    []Subcollector
	log     *slog.Logger
	timeout time.Duration

	scrapeDuration *prometheus.Desc
	scrapeSuccess  *prometheus.Desc
	buildInfo      *prometheus.Desc
}

// New builds the collector set described by cfg.
func New(cfg *config.Config, log *slog.Logger) (*Collector, error) {
	compiled, err := cfg.Compile()
	if err != nil {
		return nil, err
	}
	labels := prometheus.Labels(cfg.Labels)

	c := &Collector{
		log:     log,
		timeout: cfg.Server.ScrapeTimeout,
		scrapeDuration: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "scrape", "duration_seconds"),
			"Time spent collecting metrics from one source.",
			[]string{"collector"}, labels,
		),
		scrapeSuccess: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "scrape", "success"),
			"1 if the source was collected without error, 0 otherwise.",
			[]string{"collector"}, labels,
		),
		buildInfo: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "agent", "build_info"),
			"Build metadata of the running agent. Always 1.",
			[]string{"version", "commit", "build_date", "goversion"}, labels,
		),
	}

	add := func(enabled bool, s Subcollector) {
		if enabled {
			c.subs = append(c.subs, s)
		}
	}
	cc := cfg.Collectors
	add(cc.CPU, newCPUCollector(labels, cc.PerCPU))
	add(cc.Memory, newMemoryCollector(labels))
	add(cc.Filesystem, newFilesystemCollector(labels, compiled))
	add(cc.DiskIO, newDiskIOCollector(labels, compiled))
	add(cc.Network, newNetworkCollector(labels, compiled))
	add(cc.Load, newLoadCollector(labels))
	add(cc.Host, newHostCollector(labels))
	add(cc.Process, newProcessCollector(labels))

	return c, nil
}

// Enabled returns the names of the active sub-collectors.
func (c *Collector) Enabled() []string {
	names := make([]string, 0, len(c.subs))
	for _, s := range c.subs {
		names = append(names, s.Name())
	}
	return names
}

// Describe implements prometheus.Collector. Sending no descriptors marks this
// as an unchecked collector, which is what we want: the series set is dynamic.
func (c *Collector) Describe(chan<- *prometheus.Desc) {}

// Collect implements prometheus.Collector.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	ch <- prometheus.MustNewConstMetric(c.buildInfo, prometheus.GaugeValue, 1,
		buildinfo.Version, buildinfo.Commit, buildinfo.BuildDate, runtimeVersion())

	var wg sync.WaitGroup
	for _, sub := range c.subs {
		wg.Add(1)
		go func(s Subcollector) {
			defer wg.Done()
			start := time.Now()
			err := s.Update(ctx, ch)
			elapsed := time.Since(start)

			success := 1.0
			if err != nil {
				success = 0
				c.log.Warn("collector failed", "collector", s.Name(), "err", err)
			}
			ch <- prometheus.MustNewConstMetric(c.scrapeDuration, prometheus.GaugeValue, elapsed.Seconds(), s.Name())
			ch <- prometheus.MustNewConstMetric(c.scrapeSuccess, prometheus.GaugeValue, success, s.Name())
		}(sub)
	}
	wg.Wait()
}

// desc is a small helper for building a fully qualified metric descriptor.
func desc(subsystem, name, help string, variable []string, constant prometheus.Labels) *prometheus.Desc {
	return prometheus.NewDesc(prometheus.BuildFQName(Namespace, subsystem, name), help, variable, constant)
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
