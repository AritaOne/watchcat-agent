// Package config loads agent configuration from defaults, a YAML file and the environment.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the full agent configuration.
type Config struct {
	Server     Server            `yaml:"server"`
	Collectors Collectors        `yaml:"collectors"`
	Labels     map[string]string `yaml:"labels"`
	Log        Log               `yaml:"log"`
}

// Server holds the HTTP listener settings for the Prometheus scrape endpoint.
type Server struct {
	// ListenAddress is the host:port the metrics endpoint binds to.
	// Bind to 0.0.0.0 to accept scrapes from a remote Prometheus.
	ListenAddress string `yaml:"listen_address"`
	// MetricsPath is the URL path Prometheus scrapes.
	MetricsPath string `yaml:"metrics_path"`
	// BearerToken, when set, requires "Authorization: Bearer <token>" on scrapes.
	BearerToken string `yaml:"bearer_token"`
	// TLSCertFile and TLSKeyFile enable HTTPS when both are set.
	TLSCertFile string `yaml:"tls_cert_file"`
	TLSKeyFile  string `yaml:"tls_key_file"`
	// ReadTimeout / WriteTimeout / IdleTimeout bound slow clients.
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
	IdleTimeout  time.Duration `yaml:"idle_timeout"`
	// ScrapeTimeout bounds how long a single collection may take.
	ScrapeTimeout time.Duration `yaml:"scrape_timeout"`
}

// Collectors toggles individual metric sources and their filters.
type Collectors struct {
	CPU        bool `yaml:"cpu"`
	Memory     bool `yaml:"memory"`
	Filesystem bool `yaml:"filesystem"`
	DiskIO     bool `yaml:"diskio"`
	Network    bool `yaml:"network"`
	Load       bool `yaml:"load"`
	Host       bool `yaml:"host"`
	Process    bool `yaml:"process"`
	Go         bool `yaml:"go"`

	// Certificate reports the validity window of X.509 certificates on this
	// host. With no paths configured it watches only the agent's own serving
	// certificate, so leaving it on costs nothing.
	Certificate bool `yaml:"certificate"`
	// CertificatePaths are files or globs to watch, e.g. /etc/nginx/certs/*.pem.
	CertificatePaths []string `yaml:"certificate_paths"`

	// PerCPU reports per-core CPU time in addition to the aggregate.
	PerCPU bool `yaml:"per_cpu"`

	// FilesystemIgnoreMountPoints / FilesystemIgnoreFSTypes drop pseudo filesystems.
	FilesystemIgnoreMountPoints string `yaml:"filesystem_ignore_mount_points"`
	FilesystemIgnoreFSTypes     string `yaml:"filesystem_ignore_fs_types"`
	// NetworkIgnoreDevices drops loopback and virtual interfaces.
	NetworkIgnoreDevices string `yaml:"network_ignore_devices"`
	// DiskIOIgnoreDevices drops ram/loop devices.
	DiskIOIgnoreDevices string `yaml:"diskio_ignore_devices"`
}

// Log configures agent logging.
type Log struct {
	Level string `yaml:"level"` // debug, info, warn, error
	File  string `yaml:"file"`  // empty means stderr
}

// Compiled holds the pre-compiled ignore patterns.
type Compiled struct {
	IgnoreMountPoints *regexp.Regexp
	IgnoreFSTypes     *regexp.Regexp
	IgnoreNetDevices  *regexp.Regexp
	IgnoreDiskDevices *regexp.Regexp
}

// Default returns a configuration that is safe to run unattended.
func Default() *Config {
	return &Config{
		Server: Server{
			ListenAddress: "0.0.0.0:9800",
			MetricsPath:   "/metrics",
			ReadTimeout:   10 * time.Second,
			WriteTimeout:  30 * time.Second,
			IdleTimeout:   60 * time.Second,
			ScrapeTimeout: 15 * time.Second,
		},
		Collectors: Collectors{
			CPU: true, Memory: true, Filesystem: true, DiskIO: true,
			Network: true, Load: true, Host: true, Process: true, Go: true,
			Certificate:                 true,
			PerCPU:                      false,
			FilesystemIgnoreMountPoints: "^/(dev|proc|sys|run|var/lib/docker/.+|var/lib/kubelet/.+)($|/)",
			FilesystemIgnoreFSTypes:     "^(autofs|binfmt_misc|bpf|cgroup2?|configfs|debugfs|devpts|devtmpfs|fusectl|hugetlbfs|iso9660|mqueue|nsfs|overlay|proc|procfs|pstore|rpc_pipefs|securityfs|selinuxfs|squashfs|sysfs|tracefs)$",
			NetworkIgnoreDevices:        "^(lo[0-9]*|docker[0-9]|veth.*|br-.*|virbr.*|cni[0-9]|flannel.*|utun[0-9]+|awdl[0-9]+|llw[0-9]+)$",
			DiskIOIgnoreDevices:         "^(ram|loop|fd|(h|s|v|xv)d[a-z]+[0-9]+|nvme[0-9]+n[0-9]+p[0-9]+|dm-|md|dasd)",
		},
		Labels: map[string]string{},
		Log:    Log{Level: "info"},
	}
}

// Load reads defaults, overlays the YAML file at path (if non-empty and present),
// then overlays WATCHCAT_* environment variables.
func Load(path string) (*Config, error) {
	cfg := Default()

	if path != "" {
		data, err := os.ReadFile(path)
		switch {
		case err == nil:
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return nil, fmt.Errorf("parse config %s: %w", path, err)
			}
		case os.IsNotExist(err):
			// Missing config file is not fatal: defaults + env are enough.
		default:
			return nil, fmt.Errorf("read config %s: %w", path, err)
		}
	}

	applyEnv(cfg)

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("WATCHCAT_LISTEN_ADDRESS"); v != "" {
		cfg.Server.ListenAddress = v
	}
	if v := os.Getenv("WATCHCAT_METRICS_PATH"); v != "" {
		cfg.Server.MetricsPath = v
	}
	if v := os.Getenv("WATCHCAT_BEARER_TOKEN"); v != "" {
		cfg.Server.BearerToken = v
	}
	if v := os.Getenv("WATCHCAT_TLS_CERT_FILE"); v != "" {
		cfg.Server.TLSCertFile = v
	}
	if v := os.Getenv("WATCHCAT_TLS_KEY_FILE"); v != "" {
		cfg.Server.TLSKeyFile = v
	}
	if v := os.Getenv("WATCHCAT_LOG_LEVEL"); v != "" {
		cfg.Log.Level = v
	}
	if v := os.Getenv("WATCHCAT_LOG_FILE"); v != "" {
		cfg.Log.File = v
	}
	if v := os.Getenv("WATCHCAT_SCRAPE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Server.ScrapeTimeout = d
		}
	}
	if v := os.Getenv("WATCHCAT_PER_CPU"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Collectors.PerCPU = b
		}
	}
	// WATCHCAT_CERTIFICATE_PATHS="/etc/ssl/a.pem,/etc/nginx/certs/*.pem"
	if v := os.Getenv("WATCHCAT_CERTIFICATE_PATHS"); v != "" {
		cfg.Collectors.CertificatePaths = splitAndTrim(v)
	}
	// WATCHCAT_LABELS="env=prod,role=api" adds constant labels to every metric.
	if v := os.Getenv("WATCHCAT_LABELS"); v != "" {
		if cfg.Labels == nil {
			cfg.Labels = map[string]string{}
		}
		for _, pair := range strings.Split(v, ",") {
			k, val, ok := strings.Cut(strings.TrimSpace(pair), "=")
			if ok && k != "" {
				cfg.Labels[k] = val
			}
		}
	}
}

// Validate checks the configuration for fatal mistakes.
func (c *Config) Validate() error {
	if c.Server.ListenAddress == "" {
		return fmt.Errorf("server.listen_address must not be empty")
	}
	if !strings.HasPrefix(c.Server.MetricsPath, "/") {
		return fmt.Errorf("server.metrics_path must start with /: got %q", c.Server.MetricsPath)
	}
	if (c.Server.TLSCertFile == "") != (c.Server.TLSKeyFile == "") {
		return fmt.Errorf("server.tls_cert_file and server.tls_key_file must be set together")
	}
	if c.Server.ScrapeTimeout <= 0 {
		return fmt.Errorf("server.scrape_timeout must be positive")
	}
	for _, pattern := range c.Collectors.CertificatePaths {
		if _, err := filepath.Match(pattern, ""); err != nil {
			return fmt.Errorf("collectors.certificate_paths: bad pattern %q: %w", pattern, err)
		}
	}
	if _, err := c.Compile(); err != nil {
		return err
	}
	return nil
}

// splitAndTrim parses a comma separated environment value into a clean list.
func splitAndTrim(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// Compile pre-compiles every ignore pattern.
func (c *Config) Compile() (*Compiled, error) {
	out := &Compiled{}
	var err error
	compile := func(name, expr string) *regexp.Regexp {
		if err != nil || expr == "" {
			return nil
		}
		re, cerr := regexp.Compile(expr)
		if cerr != nil {
			err = fmt.Errorf("collectors.%s: %w", name, cerr)
			return nil
		}
		return re
	}
	out.IgnoreMountPoints = compile("filesystem_ignore_mount_points", c.Collectors.FilesystemIgnoreMountPoints)
	out.IgnoreFSTypes = compile("filesystem_ignore_fs_types", c.Collectors.FilesystemIgnoreFSTypes)
	out.IgnoreNetDevices = compile("network_ignore_devices", c.Collectors.NetworkIgnoreDevices)
	out.IgnoreDiskDevices = compile("diskio_ignore_devices", c.Collectors.DiskIOIgnoreDevices)
	return out, err
}

// TLSEnabled reports whether the endpoint should be served over HTTPS.
func (c *Config) TLSEnabled() bool { return c.Server.TLSCertFile != "" && c.Server.TLSKeyFile != "" }
