package collector

import (
	"context"
	"slices"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/shirou/gopsutil/v4/disk"

	"github.com/aritaone/watchcat-agent/internal/config"
)

// filesystemCollector reports capacity per mounted filesystem.
type filesystemCollector struct {
	compiled *config.Compiled

	size, free, avail, used *prometheus.Desc
	files, filesFree        *prometheus.Desc
	readonly                *prometheus.Desc
	deviceError             *prometheus.Desc
}

func newFilesystemCollector(labels prometheus.Labels, compiled *config.Compiled) *filesystemCollector {
	vars := []string{"device", "mountpoint", "fstype"}
	d := func(name, help string) *prometheus.Desc { return desc("filesystem", name, help, vars, labels) }
	return &filesystemCollector{
		compiled:    compiled,
		size:        d("size_bytes", "Total size of the filesystem."),
		free:        d("free_bytes", "Free space on the filesystem, including root-reserved space."),
		avail:       d("avail_bytes", "Space available to unprivileged users."),
		used:        d("used_bytes", "Space in use on the filesystem."),
		files:       d("files", "Total inodes on the filesystem."),
		filesFree:   d("files_free", "Free inodes on the filesystem."),
		readonly:    d("readonly", "1 if the filesystem is mounted read-only."),
		deviceError: d("device_error", "1 if the filesystem could not be stat'ed."),
	}
}

func (c *filesystemCollector) Name() string { return "filesystem" }

func (c *filesystemCollector) Update(ctx context.Context, ch chan<- prometheus.Metric) error {
	parts, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		return err
	}

	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		if c.compiled.IgnoreMountPoints != nil && c.compiled.IgnoreMountPoints.MatchString(p.Mountpoint) {
			continue
		}
		if c.compiled.IgnoreFSTypes != nil && c.compiled.IgnoreFSTypes.MatchString(p.Fstype) {
			continue
		}
		// Bind mounts surface the same device twice; keep the first mount point only.
		if _, dup := seen[p.Mountpoint]; dup {
			continue
		}
		seen[p.Mountpoint] = struct{}{}

		lbl := []string{p.Device, p.Mountpoint, p.Fstype}
		ro := slices.Contains(p.Opts, "ro")
		ch <- prometheus.MustNewConstMetric(c.readonly, prometheus.GaugeValue, boolToFloat(ro), lbl...)

		usage, uerr := disk.UsageWithContext(ctx, p.Mountpoint)
		if uerr != nil || usage == nil {
			// A disconnected network mount must not fail the whole scrape.
			ch <- prometheus.MustNewConstMetric(c.deviceError, prometheus.GaugeValue, 1, lbl...)
			continue
		}
		ch <- prometheus.MustNewConstMetric(c.deviceError, prometheus.GaugeValue, 0, lbl...)

		g := func(d *prometheus.Desc, v float64) {
			ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v, lbl...)
		}
		g(c.size, float64(usage.Total))
		g(c.free, float64(usage.Free))
		g(c.avail, float64(usage.Free))
		g(c.used, float64(usage.Used))
		g(c.files, float64(usage.InodesTotal))
		g(c.filesFree, float64(usage.InodesFree))
	}
	return nil
}
