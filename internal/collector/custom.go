package collector

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

// maxCustomFileSize bounds how much of a single .conf file is read per scrape.
const maxCustomFileSize = 1 << 20

// customExt is the only extension the custom collector picks up.
const customExt = ".conf"

var customNameRE = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// customReserved are names the collector exports itself; a file reusing them
// would produce conflicting series.
var customReserved = map[string]bool{"read_error": true, "dir_error": true}

// customCollector exports values written by other programs on this host.
// Every *.conf file directly inside dir holds "name:value" lines, and each line
// becomes the gauge watchcat_custom_<name>{file="<file>.conf"}.
type customCollector struct {
	dir    string
	labels prometheus.Labels
	log    *slog.Logger

	readError *prometheus.Desc
	dirError  *prometheus.Desc
}

func newCustomCollector(labels prometheus.Labels, dir string, log *slog.Logger) *customCollector {
	return &customCollector{
		dir:    dir,
		labels: labels,
		log:    log,
		readError: desc("custom", "read_error",
			"1 if the custom metrics file could not be read.", []string{"file"}, labels),
		dirError: desc("custom", "dir_error",
			"1 if the custom metrics folder could not be read.", nil, labels),
	}
}

func (c *customCollector) Name() string { return "custom" }

func (c *customCollector) Update(_ context.Context, ch chan<- prometheus.Metric) error {
	// os.ReadDir returns entries sorted by name, so output order is stable.
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		// Reported as a metric rather than a collector failure, so a folder that
		// has not been created yet is alertable without flapping scrape_success.
		c.log.Warn("custom metrics folder unreadable", "dir", c.dir, "err", err)
		ch <- prometheus.MustNewConstMetric(c.dirError, prometheus.GaugeValue, 1)
		return nil
	}
	ch <- prometheus.MustNewConstMetric(c.dirError, prometheus.GaugeValue, 0)

	for _, e := range entries {
		file := e.Name()
		if !e.Type().IsRegular() || strings.HasPrefix(file, ".") || filepath.Ext(file) != customExt {
			continue
		}
		samples, err := c.readFile(filepath.Join(c.dir, file))
		if err != nil {
			c.log.Warn("custom metrics file unreadable", "file", file, "err", err)
			ch <- prometheus.MustNewConstMetric(c.readError, prometheus.GaugeValue, 1, file)
			continue
		}
		ch <- prometheus.MustNewConstMetric(c.readError, prometheus.GaugeValue, 0, file)

		for _, s := range samples {
			d := desc("custom", s.name, "Custom value read from "+customExt+" files in "+c.dir+".",
				[]string{"file"}, c.labels)
			ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, s.value, file)
		}
	}
	return nil
}

type customSample struct {
	name  string
	value float64
}

func (c *customCollector) readFile(path string) ([]customSample, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxCustomFileSize {
		return nil, fmt.Errorf("%d bytes exceeds the %d byte limit", info.Size(), maxCustomFileSize)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	samples, problems := parseCustom(string(data))
	for _, p := range problems {
		c.log.Warn("custom metrics line skipped", "file", filepath.Base(path), "err", p)
	}
	return samples, nil
}

// parseCustom turns "name:value" lines into samples. Blank lines and lines
// starting with # are ignored. A bad line is reported and skipped so it never
// hides the valid lines around it; a repeated name keeps its first value.
func parseCustom(data string) ([]customSample, []error) {
	var (
		samples  []customSample
		problems []error
		seen     = map[string]bool{}
	)
	for i, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lineNo := i + 1
		name, raw, ok := strings.Cut(line, ":")
		if !ok {
			problems = append(problems, fmt.Errorf("line %d: missing ':'", lineNo))
			continue
		}
		name, raw = strings.TrimSpace(name), strings.TrimSpace(raw)
		if !customNameRE.MatchString(name) {
			problems = append(problems, fmt.Errorf("line %d: invalid metric name %q", lineNo, name))
			continue
		}
		if customReserved[name] {
			problems = append(problems, fmt.Errorf("line %d: name %q is reserved", lineNo, name))
			continue
		}
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			problems = append(problems, fmt.Errorf("line %d: invalid value %q for %s", lineNo, raw, name))
			continue
		}
		if seen[name] {
			problems = append(problems, fmt.Errorf("line %d: duplicate name %s, keeping the first value", lineNo, name))
			continue
		}
		seen[name] = true
		samples = append(samples, customSample{name: name, value: value})
	}
	return samples, problems
}
