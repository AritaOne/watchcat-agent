package collector

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/shirou/gopsutil/v4/disk"

	"github.com/aritaone/watchcat-agent/internal/config"
)

// diskIOCollector reports cumulative block device I/O counters.
type diskIOCollector struct {
	compiled *config.Compiled

	reads, writes            *prometheus.Desc
	readBytes, writeBytes    *prometheus.Desc
	readTime, writeTime      *prometheus.Desc
	ioTime, mergedR, mergedW *prometheus.Desc
	inFlight                 *prometheus.Desc
}

func newDiskIOCollector(labels prometheus.Labels, compiled *config.Compiled) *diskIOCollector {
	vars := []string{"device"}
	d := func(name, help string) *prometheus.Desc { return desc("disk", name, help, vars, labels) }
	return &diskIOCollector{
		compiled:   compiled,
		reads:      d("reads_completed_total", "Reads completed successfully."),
		writes:     d("writes_completed_total", "Writes completed successfully."),
		readBytes:  d("read_bytes_total", "Bytes read from the device."),
		writeBytes: d("written_bytes_total", "Bytes written to the device."),
		readTime:   d("read_time_seconds_total", "Seconds spent reading."),
		writeTime:  d("write_time_seconds_total", "Seconds spent writing."),
		ioTime:     d("io_time_seconds_total", "Seconds the device spent doing I/O."),
		mergedR:    d("reads_merged_total", "Reads merged before reaching the device."),
		mergedW:    d("writes_merged_total", "Writes merged before reaching the device."),
		inFlight:   d("io_now", "I/O operations currently in flight."),
	}
}

func (c *diskIOCollector) Name() string { return "diskio" }

func (c *diskIOCollector) Update(ctx context.Context, ch chan<- prometheus.Metric) error {
	stats, err := disk.IOCountersWithContext(ctx)
	if err != nil {
		return err
	}
	for name, s := range stats {
		if c.compiled.IgnoreDiskDevices != nil && c.compiled.IgnoreDiskDevices.MatchString(name) {
			continue
		}
		cnt := func(d *prometheus.Desc, v float64) {
			ch <- prometheus.MustNewConstMetric(d, prometheus.CounterValue, v, name)
		}
		cnt(c.reads, float64(s.ReadCount))
		cnt(c.writes, float64(s.WriteCount))
		cnt(c.readBytes, float64(s.ReadBytes))
		cnt(c.writeBytes, float64(s.WriteBytes))
		// gopsutil reports these in milliseconds; Prometheus wants base units.
		cnt(c.readTime, float64(s.ReadTime)/1000)
		cnt(c.writeTime, float64(s.WriteTime)/1000)
		cnt(c.ioTime, float64(s.IoTime)/1000)
		cnt(c.mergedR, float64(s.MergedReadCount))
		cnt(c.mergedW, float64(s.MergedWriteCount))
		ch <- prometheus.MustNewConstMetric(c.inFlight, prometheus.GaugeValue, float64(s.IopsInProgress), name)
	}
	return nil
}
