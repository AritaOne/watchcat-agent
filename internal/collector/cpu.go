package collector

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/shirou/gopsutil/v4/cpu"
)

// cpuCollector exports cumulative CPU time per mode as counters, leaving the
// rate() arithmetic to PromQL the way node_exporter does.
type cpuCollector struct {
	perCPU bool

	seconds  *prometheus.Desc
	count    *prometheus.Desc
	logical  *prometheus.Desc
	physical *prometheus.Desc
}

func newCPUCollector(labels prometheus.Labels, perCPU bool) *cpuCollector {
	return &cpuCollector{
		perCPU:   perCPU,
		seconds:  desc("cpu", "seconds_total", "Seconds the CPUs spent in each mode.", []string{"cpu", "mode"}, labels),
		count:    desc("cpu", "cores", "Number of logical CPU cores.", nil, labels),
		logical:  desc("cpu", "logical_cores", "Number of logical CPU cores.", nil, labels),
		physical: desc("cpu", "physical_cores", "Number of physical CPU cores.", nil, labels),
	}
}

func (c *cpuCollector) Name() string { return "cpu" }

func (c *cpuCollector) Update(ctx context.Context, ch chan<- prometheus.Metric) error {
	times, err := cpu.TimesWithContext(ctx, c.perCPU)
	if err != nil {
		return err
	}
	for _, t := range times {
		name := t.CPU
		if !c.perCPU {
			name = "all"
		}
		emit := func(mode string, v float64) {
			ch <- prometheus.MustNewConstMetric(c.seconds, prometheus.CounterValue, v, name, mode)
		}
		emit("user", t.User)
		emit("system", t.System)
		emit("idle", t.Idle)
		emit("nice", t.Nice)
		emit("iowait", t.Iowait)
		emit("irq", t.Irq)
		emit("softirq", t.Softirq)
		emit("steal", t.Steal)
		emit("guest", t.Guest)
		emit("guest_nice", t.GuestNice)
	}

	if n, err := cpu.CountsWithContext(ctx, true); err == nil {
		ch <- prometheus.MustNewConstMetric(c.count, prometheus.GaugeValue, float64(n))
		ch <- prometheus.MustNewConstMetric(c.logical, prometheus.GaugeValue, float64(n))
	}
	if n, err := cpu.CountsWithContext(ctx, false); err == nil {
		ch <- prometheus.MustNewConstMetric(c.physical, prometheus.GaugeValue, float64(n))
	}
	return nil
}
