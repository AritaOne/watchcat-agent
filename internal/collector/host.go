package collector

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/shirou/gopsutil/v4/host"
)

// hostCollector reports identity and uptime for the machine.
type hostCollector struct {
	info     *prometheus.Desc
	bootTime *prometheus.Desc
	uptime   *prometheus.Desc
	now      *prometheus.Desc
}

func newHostCollector(labels prometheus.Labels) *hostCollector {
	return &hostCollector{
		info: desc("host", "info",
			"Constant 1 carrying host identity as labels.",
			[]string{"hostname", "os", "platform", "platform_family", "platform_version", "kernel_version", "kernel_arch", "virtualization"},
			labels),
		bootTime: desc("host", "boot_time_seconds", "Unix timestamp of the last boot.", nil, labels),
		uptime:   desc("host", "uptime_seconds", "Seconds since the last boot.", nil, labels),
		now:      desc("host", "time_seconds", "Current system time as a Unix timestamp, for clock-skew checks.", nil, labels),
	}
}

func (c *hostCollector) Name() string { return "host" }

func (c *hostCollector) Update(ctx context.Context, ch chan<- prometheus.Metric) error {
	ch <- prometheus.MustNewConstMetric(c.now, prometheus.GaugeValue, float64(time.Now().UnixNano())/1e9)

	i, err := host.InfoWithContext(ctx)
	if err != nil {
		return err
	}
	ch <- prometheus.MustNewConstMetric(c.info, prometheus.GaugeValue, 1,
		i.Hostname, i.OS, i.Platform, i.PlatformFamily, i.PlatformVersion,
		i.KernelVersion, i.KernelArch, i.VirtualizationSystem)
	ch <- prometheus.MustNewConstMetric(c.bootTime, prometheus.GaugeValue, float64(i.BootTime))
	ch <- prometheus.MustNewConstMetric(c.uptime, prometheus.GaugeValue, float64(i.Uptime))
	return nil
}
