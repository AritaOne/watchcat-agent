package collector

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

// maxCertFileSize bounds how much of a configured path we are willing to read.
// Real certificate bundles are a few kilobytes; anything larger is a misconfigured
// path and reading it would only waste memory during a scrape.
const maxCertFileSize = 1 << 20

// certificateCollector reports the validity window of X.509 certificates held
// on this host, so a certificate can be alerted on by the machine that serves it.
//
// It reads certificates only. PEM blocks that are not CERTIFICATE — private keys
// above all — are skipped without being parsed, and no key material is ever
// turned into a label.
type certificateCollector struct {
	patterns []string

	notAfter  *prometheus.Desc
	notBefore *prometheus.Desc
	info      *prometheus.Desc
	readError *prometheus.Desc
}

func newCertificateCollector(labels prometheus.Labels, patterns []string) *certificateCollector {
	// path plus position in the file, so a bundle's leaf and its intermediates
	// stay distinguishable. These labels are stable across renewals: the
	// changing details live on the _info metric instead.
	vars := []string{"path", "index"}
	return &certificateCollector{
		patterns: patterns,
		notAfter: desc("certificate", "not_after_seconds",
			"Unix timestamp at which the certificate expires.", vars, labels),
		notBefore: desc("certificate", "not_before_seconds",
			"Unix timestamp from which the certificate is valid.", vars, labels),
		info: desc("certificate", "info",
			"Constant 1 carrying certificate identity as labels.",
			append(slices.Clone(vars), "subject", "issuer", "serial", "dns_names"), labels),
		readError: desc("certificate", "read_error",
			"1 if the path could not be read or held no certificate.", []string{"path"}, labels),
	}
}

func (c *certificateCollector) Name() string { return "certificate" }

func (c *certificateCollector) Update(_ context.Context, ch chan<- prometheus.Metric) error {
	paths, err := c.expand()
	if err != nil {
		return err
	}

	for _, path := range paths {
		certs, err := readCertificates(path)
		if err != nil {
			// One unreadable path must not hide the certificates that did load.
			ch <- prometheus.MustNewConstMetric(c.readError, prometheus.GaugeValue, 1, path)
			continue
		}
		ch <- prometheus.MustNewConstMetric(c.readError, prometheus.GaugeValue, 0, path)

		for i, cert := range certs {
			idx := strconv.Itoa(i)
			ch <- prometheus.MustNewConstMetric(c.notAfter, prometheus.GaugeValue,
				float64(cert.NotAfter.Unix()), path, idx)
			ch <- prometheus.MustNewConstMetric(c.notBefore, prometheus.GaugeValue,
				float64(cert.NotBefore.Unix()), path, idx)
			ch <- prometheus.MustNewConstMetric(c.info, prometheus.GaugeValue, 1,
				path, idx,
				name(cert.Subject.CommonName, cert.Subject.String()),
				name(cert.Issuer.CommonName, cert.Issuer.String()),
				cert.SerialNumber.String(),
				strings.Join(cert.DNSNames, ","))
		}
	}
	return nil
}

// expand turns the configured patterns into a sorted, de-duplicated file list.
//
// A glob matching nothing is silently empty, which is what you want for
// /etc/nginx/certs/*.pem on a host without nginx. A literal path that does not
// exist is kept, so it surfaces as a read_error rather than disappearing: a
// certificate you asked to watch going missing is itself worth alerting on.
func (c *certificateCollector) expand() ([]string, error) {
	var paths []string
	for _, pattern := range c.patterns {
		if !strings.ContainsAny(pattern, "*?[") {
			paths = append(paths, pattern)
			continue
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("certificate path %q: %w", pattern, err)
		}
		paths = append(paths, matches...)
	}
	slices.Sort(paths)
	return slices.Compact(paths), nil
}

// readCertificates parses every certificate in a PEM bundle, falling back to
// raw DER for files that are not PEM-encoded.
func readCertificates(path string) ([]*x509.Certificate, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxCertFileSize {
		return nil, fmt.Errorf("%s: %d bytes exceeds the %d byte limit", path, info.Size(), maxCertFileSize)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var certs []*x509.Certificate
	rest := data
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		// Anything else in the bundle, a private key included, is left alone.
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		certs = append(certs, cert)
	}

	if len(certs) == 0 {
		if der, err := x509.ParseCertificates(data); err == nil && len(der) > 0 {
			return der, nil
		}
		return nil, fmt.Errorf("%s: no certificate found", path)
	}
	return certs, nil
}

// name prefers the common name, falling back to the full distinguished name for
// the modern certificates that leave CN empty and rely on SANs.
func name(commonName, distinguished string) string {
	if commonName != "" {
		return commonName
	}
	return distinguished
}
