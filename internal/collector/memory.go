package collector

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/shirou/gopsutil/v4/mem"
)

type memoryCollector struct {
	total, available, used, free  *prometheus.Desc
	cached, buffers               *prometheus.Desc
	swapTotal, swapUsed, swapFree *prometheus.Desc
	swapSwappedIn, swapSwappedOut *prometheus.Desc
}

func newMemoryCollector(labels prometheus.Labels) *memoryCollector {
	d := func(name, help string) *prometheus.Desc { return desc("memory", name, help, nil, labels) }
	return &memoryCollector{
		total:          d("total_bytes", "Total installed physical memory."),
		available:      d("available_bytes", "Memory available for new applications without swapping."),
		used:           d("used_bytes", "Memory in use."),
		free:           d("free_bytes", "Memory not in use at all."),
		cached:         d("cached_bytes", "Memory used by the page cache."),
		buffers:        d("buffers_bytes", "Memory used by kernel buffers."),
		swapTotal:      d("swap_total_bytes", "Total swap space."),
		swapUsed:       d("swap_used_bytes", "Swap space in use."),
		swapFree:       d("swap_free_bytes", "Swap space free."),
		swapSwappedIn:  d("swap_in_bytes_total", "Bytes swapped in from disk."),
		swapSwappedOut: d("swap_out_bytes_total", "Bytes swapped out to disk."),
	}
}

func (c *memoryCollector) Name() string { return "memory" }

func (c *memoryCollector) Update(ctx context.Context, ch chan<- prometheus.Metric) error {
	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return err
	}
	g := func(d *prometheus.Desc, v float64) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v)
	}
	g(c.total, float64(vm.Total))
	g(c.available, float64(vm.Available))
	g(c.used, float64(vm.Used))
	g(c.free, float64(vm.Free))
	g(c.cached, float64(vm.Cached))
	g(c.buffers, float64(vm.Buffers))

	// Swap is optional: a machine without swap should not fail the scrape.
	sm, err := mem.SwapMemoryWithContext(ctx)
	if err != nil {
		return nil
	}
	g(c.swapTotal, float64(sm.Total))
	g(c.swapUsed, float64(sm.Used))
	g(c.swapFree, float64(sm.Free))
	ch <- prometheus.MustNewConstMetric(c.swapSwappedIn, prometheus.CounterValue, float64(sm.Sin))
	ch <- prometheus.MustNewConstMetric(c.swapSwappedOut, prometheus.CounterValue, float64(sm.Sout))
	return nil
}
