package collector

import (
	"context"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/process"
)

// processCollector reports the system process count plus the agent's own
// footprint, so you can alert on the watchdog itself misbehaving.
type processCollector struct {
	pid int32

	procs      *prometheus.Desc
	selfCPU    *prometheus.Desc
	selfRSS    *prometheus.Desc
	selfVMS    *prometheus.Desc
	selfFDs    *prometheus.Desc
	selfThread *prometheus.Desc
	selfStart  *prometheus.Desc
}

func newProcessCollector(labels prometheus.Labels) *processCollector {
	return &processCollector{
		pid:        int32(os.Getpid()),
		procs:      desc("", "processes", "Number of processes on the host.", nil, labels),
		selfCPU:    desc("agent", "cpu_seconds_total", "CPU seconds consumed by the agent itself.", nil, labels),
		selfRSS:    desc("agent", "resident_memory_bytes", "Resident memory of the agent process.", nil, labels),
		selfVMS:    desc("agent", "virtual_memory_bytes", "Virtual memory of the agent process.", nil, labels),
		selfFDs:    desc("agent", "open_fds", "Open file descriptors held by the agent.", nil, labels),
		selfThread: desc("agent", "threads", "OS threads used by the agent.", nil, labels),
		selfStart:  desc("agent", "start_time_seconds", "Unix start time of the agent process.", nil, labels),
	}
}

func (c *processCollector) Name() string { return "process" }

func (c *processCollector) Update(ctx context.Context, ch chan<- prometheus.Metric) error {
	if i, err := host.InfoWithContext(ctx); err == nil {
		ch <- prometheus.MustNewConstMetric(c.procs, prometheus.GaugeValue, float64(i.Procs))
	}

	p, err := process.NewProcessWithContext(ctx, c.pid)
	if err != nil {
		return err
	}
	g := func(d *prometheus.Desc, v float64) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v)
	}
	if t, err := p.TimesWithContext(ctx); err == nil {
		ch <- prometheus.MustNewConstMetric(c.selfCPU, prometheus.CounterValue, t.User+t.System)
	}
	if m, err := p.MemoryInfoWithContext(ctx); err == nil && m != nil {
		g(c.selfRSS, float64(m.RSS))
		g(c.selfVMS, float64(m.VMS))
	}
	// Not implemented on every platform; absence is not an error.
	if n, err := p.NumFDsWithContext(ctx); err == nil {
		g(c.selfFDs, float64(n))
	}
	if n, err := p.NumThreadsWithContext(ctx); err == nil {
		g(c.selfThread, float64(n))
	}
	if ms, err := p.CreateTimeWithContext(ctx); err == nil {
		g(c.selfStart, float64(ms)/1000)
	}
	return nil
}
