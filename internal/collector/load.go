package collector

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/shirou/gopsutil/v4/load"
)

// loadCollector reports the 1/5/15 minute run-queue averages.
// On Windows these are emulated by gopsutil and read 0 until it has sampled.
type loadCollector struct {
	load1, load5, load15 *prometheus.Desc
}

func newLoadCollector(labels prometheus.Labels) *loadCollector {
	return &loadCollector{
		load1:  desc("", "load1", "1 minute load average.", nil, labels),
		load5:  desc("", "load5", "5 minute load average.", nil, labels),
		load15: desc("", "load15", "15 minute load average.", nil, labels),
	}
}

func (c *loadCollector) Name() string { return "load" }

func (c *loadCollector) Update(ctx context.Context, ch chan<- prometheus.Metric) error {
	avg, err := load.AvgWithContext(ctx)
	if err != nil {
		return err
	}
	ch <- prometheus.MustNewConstMetric(c.load1, prometheus.GaugeValue, avg.Load1)
	ch <- prometheus.MustNewConstMetric(c.load5, prometheus.GaugeValue, avg.Load5)
	ch <- prometheus.MustNewConstMetric(c.load15, prometheus.GaugeValue, avg.Load15)
	return nil
}
