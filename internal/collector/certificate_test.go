package collector

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/aritaone/watchcat-agent/internal/config"
)

// writeCert generates a self-signed certificate and writes it to path in PEM
// form, returning its expiry.
func writeCert(t *testing.T, path string, validFor time.Duration, dnsNames ...string) time.Time {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	notAfter := time.Now().Add(validFor).Truncate(time.Second)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(4242),
		Subject:      pkix.Name{CommonName: "watchcat-test"},
		NotBefore:    time.Now().Add(-time.Hour).Truncate(time.Second),
		NotAfter:     notAfter,
		DNSNames:     dnsNames,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(path, pemBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	return notAfter
}

// certMetrics gathers the certificate collector and indexes the results by
// metric name.
func certMetrics(t *testing.T, cfg *config.Config) map[string][]metricSample {
	t.Helper()

	c, err := New(cfg, testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(c)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}

	out := map[string][]metricSample{}
	for _, mf := range families {
		for _, m := range mf.GetMetric() {
			s := metricSample{labels: map[string]string{}, value: m.GetGauge().GetValue()}
			for _, l := range m.GetLabel() {
				s.labels[l.GetName()] = l.GetValue()
			}
			out[mf.GetName()] = append(out[mf.GetName()], s)
		}
	}
	return out
}

type metricSample struct {
	labels map[string]string
	value  float64
}

// certOnly disables every other collector so the assertions stay focused.
func certOnly(paths ...string) *config.Config {
	cfg := config.Default()
	cfg.Collectors = config.Collectors{Certificate: true, CertificatePaths: paths}
	return cfg
}

func TestCertificateExpiryIsReported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "site.pem")
	notAfter := writeCert(t, path, 30*24*time.Hour, "example.internal", "www.example.internal")

	got := certMetrics(t, certOnly(path))

	after := got["watchcat_certificate_not_after_seconds"]
	if len(after) != 1 {
		t.Fatalf("got %d not_after series, want 1", len(after))
	}
	if want := float64(notAfter.Unix()); after[0].value != want {
		t.Errorf("not_after = %v, want %v", after[0].value, want)
	}
	if after[0].labels["path"] != path || after[0].labels["index"] != "0" {
		t.Errorf("labels = %v", after[0].labels)
	}

	if before := got["watchcat_certificate_not_before_seconds"]; len(before) != 1 {
		t.Errorf("got %d not_before series, want 1", len(before))
	}

	info := got["watchcat_certificate_info"]
	if len(info) != 1 {
		t.Fatalf("got %d info series, want 1", len(info))
	}
	if cn := info[0].labels["subject"]; cn != "watchcat-test" {
		t.Errorf("subject = %q", cn)
	}
	if sans := info[0].labels["dns_names"]; sans != "example.internal,www.example.internal" {
		t.Errorf("dns_names = %q", sans)
	}
	if serial := info[0].labels["serial"]; serial != "4242" {
		t.Errorf("serial = %q", serial)
	}

	if errs := got["watchcat_certificate_read_error"]; len(errs) != 1 || errs[0].value != 0 {
		t.Errorf("read_error = %v, want a single 0", errs)
	}
}

func TestCertificateGlobExpansion(t *testing.T) {
	dir := t.TempDir()
	writeCert(t, filepath.Join(dir, "a.pem"), time.Hour)
	writeCert(t, filepath.Join(dir, "b.pem"), time.Hour)
	// Must not be picked up by *.pem.
	writeCert(t, filepath.Join(dir, "c.crt"), time.Hour)

	got := certMetrics(t, certOnly(filepath.Join(dir, "*.pem")))

	if n := len(got["watchcat_certificate_not_after_seconds"]); n != 2 {
		t.Errorf("got %d certificates, want 2", n)
	}
}

func TestCertificateGlobMatchingNothingIsQuiet(t *testing.T) {
	// A host without nginx should not produce a permanent read_error.
	got := certMetrics(t, certOnly(filepath.Join(t.TempDir(), "*.pem")))

	if n := len(got["watchcat_certificate_read_error"]); n != 0 {
		t.Errorf("got %d read_error series, want 0", n)
	}
}

func TestCertificateMissingLiteralPathReportsError(t *testing.T) {
	// A path named explicitly going missing is worth alerting on.
	missing := filepath.Join(t.TempDir(), "gone.pem")

	got := certMetrics(t, certOnly(missing))

	errs := got["watchcat_certificate_read_error"]
	if len(errs) != 1 || errs[0].value != 1 {
		t.Fatalf("read_error = %v, want a single 1", errs)
	}
	if n := len(got["watchcat_certificate_not_after_seconds"]); n != 0 {
		t.Errorf("got %d not_after series for a missing file, want 0", n)
	}
}

func TestCertificateBundleReportsEveryCertificate(t *testing.T) {
	dir := t.TempDir()
	leaf := filepath.Join(dir, "leaf.pem")
	inter := filepath.Join(dir, "inter.pem")
	writeCert(t, leaf, 24*time.Hour)
	writeCert(t, inter, 48*time.Hour)

	bundle := filepath.Join(dir, "chain.pem")
	a, _ := os.ReadFile(leaf)
	b, _ := os.ReadFile(inter)
	if err := os.WriteFile(bundle, append(a, b...), 0o644); err != nil {
		t.Fatal(err)
	}

	got := certMetrics(t, certOnly(bundle))

	after := got["watchcat_certificate_not_after_seconds"]
	if len(after) != 2 {
		t.Fatalf("got %d series for a 2-certificate bundle, want 2", len(after))
	}
	seen := map[string]bool{}
	for _, s := range after {
		seen[s.labels["index"]] = true
	}
	if !seen["0"] || !seen["1"] {
		t.Errorf("expected index 0 and 1, got %v", seen)
	}
}

func TestPrivateKeyBlocksAreIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "combined.pem")
	writeCert(t, path, time.Hour)

	// Many deployments concatenate the key after the certificate. The key must
	// never be parsed, and must never reach a label.
	certPEM, _ := os.ReadFile(path)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(path, append(certPEM, keyPEM...), 0o600); err != nil {
		t.Fatal(err)
	}

	got := certMetrics(t, certOnly(path))

	if n := len(got["watchcat_certificate_not_after_seconds"]); n != 1 {
		t.Errorf("got %d certificates, want 1 (the key must be skipped)", n)
	}
	for _, s := range got["watchcat_certificate_info"] {
		for name, v := range s.labels {
			if len(v) > 0 && (v == string(keyPEM) || len(v) > 512) {
				t.Errorf("label %q looks like it carries key material", name)
			}
		}
	}
}

func TestGarbageFileReportsReadError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-cert.pem")
	if err := os.WriteFile(path, []byte("hello, not a certificate"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := certMetrics(t, certOnly(path))

	errs := got["watchcat_certificate_read_error"]
	if len(errs) != 1 || errs[0].value != 1 {
		t.Errorf("read_error = %v, want a single 1", errs)
	}
}

func TestAgentServingCertIsWatchedAutomatically(t *testing.T) {
	dir := t.TempDir()
	serving := filepath.Join(dir, "serving.pem")
	writeCert(t, serving, 90*24*time.Hour)

	cfg := certOnly() // no explicit paths at all
	cfg.Server.TLSCertFile = serving
	cfg.Server.TLSKeyFile = filepath.Join(dir, "serving.key")

	got := certMetrics(t, cfg)

	after := got["watchcat_certificate_not_after_seconds"]
	if len(after) != 1 {
		t.Fatalf("got %d series, want the serving certificate to be watched", len(after))
	}
	if after[0].labels["path"] != serving {
		t.Errorf("path = %q, want %q", after[0].labels["path"], serving)
	}
}

func TestServingCertNotDuplicatedWhenAlsoListed(t *testing.T) {
	dir := t.TempDir()
	serving := filepath.Join(dir, "serving.pem")
	writeCert(t, serving, time.Hour)

	cfg := certOnly(serving)
	cfg.Server.TLSCertFile = serving
	cfg.Server.TLSKeyFile = filepath.Join(dir, "serving.key")

	got := certMetrics(t, cfg)

	if n := len(got["watchcat_certificate_not_after_seconds"]); n != 1 {
		t.Errorf("got %d series, want 1 (no duplicate for the serving cert)", n)
	}
}

func TestCertificateCollectorIsQuietWithNoPaths(t *testing.T) {
	got := certMetrics(t, certOnly())

	for _, name := range []string{
		"watchcat_certificate_not_after_seconds",
		"watchcat_certificate_read_error",
	} {
		if n := len(got[name]); n != 0 {
			t.Errorf("%s produced %d series with no paths configured, want 0", name, n)
		}
	}
	// It still reports that it ran successfully.
	if n := len(got["watchcat_scrape_success"]); n == 0 {
		t.Error("expected a scrape_success series")
	}
}
