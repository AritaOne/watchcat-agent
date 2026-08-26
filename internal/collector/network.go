package collector

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	psnet "github.com/shirou/gopsutil/v4/net"

	"github.com/aritaone/watchcat-agent/internal/config"
)

// networkCollector reports per-interface traffic counters.
type networkCollector struct {
	compiled *config.Compiled

	rxBytes, txBytes     *prometheus.Desc
	rxPackets, txPackets *prometheus.Desc
	rxErrs, txErrs       *prometheus.Desc
	rxDrop, txDrop       *prometheus.Desc
}

func newNetworkCollector(labels prometheus.Labels, compiled *config.Compiled) *networkCollector {
	vars := []string{"device"}
	d := func(name, help string) *prometheus.Desc { return desc("network", name, help, vars, labels) }
	return &networkCollector{
		compiled:  compiled,
		rxBytes:   d("receive_bytes_total", "Bytes received on the interface."),
		txBytes:   d("transmit_bytes_total", "Bytes transmitted on the interface."),
		rxPackets: d("receive_packets_total", "Packets received on the interface."),
		txPackets: d("transmit_packets_total", "Packets transmitted on the interface."),
		rxErrs:    d("receive_errs_total", "Receive errors on the interface."),
		txErrs:    d("transmit_errs_total", "Transmit errors on the interface."),
		rxDrop:    d("receive_drop_total", "Received packets dropped on the interface."),
		txDrop:    d("transmit_drop_total", "Transmitted packets dropped on the interface."),
	}
}

func (c *networkCollector) Name() string { return "network" }

func (c *networkCollector) Update(ctx context.Context, ch chan<- prometheus.Metric) error {
	stats, err := psnet.IOCountersWithContext(ctx, true)
	if err != nil {
		return err
	}
	for _, s := range stats {
		if c.compiled.IgnoreNetDevices != nil && c.compiled.IgnoreNetDevices.MatchString(s.Name) {
			continue
		}
		cnt := func(d *prometheus.Desc, v float64) {
			ch <- prometheus.MustNewConstMetric(d, prometheus.CounterValue, v, s.Name)
		}
		cnt(c.rxBytes, float64(s.BytesRecv))
		cnt(c.txBytes, float64(s.BytesSent))
		cnt(c.rxPackets, float64(s.PacketsRecv))
		cnt(c.txPackets, float64(s.PacketsSent))
		cnt(c.rxErrs, float64(s.Errin))
		cnt(c.txErrs, float64(s.Errout))
		cnt(c.rxDrop, float64(s.Dropin))
		cnt(c.txDrop, float64(s.Dropout))
	}
	return nil
}
