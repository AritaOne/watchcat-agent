# watchcat-agent

A single-binary host metrics agent for **Linux, Windows and macOS**. Download it
onto a server, install it as a background service, and a remote Prometheus
server pulls metrics from it over HTTP.

- **One static binary**, no runtime, no dependencies, CGO-free (~11 MB).
- **Runs as a real service** — systemd on Linux, launchd on macOS, the Service
  Control Manager on Windows — installed and started by the binary itself.
- **Pull model**: the agent exposes `/metrics`; Prometheus scrapes it. Nothing is
  pushed out, so the agent never needs outbound access.
- **Secured for remote scraping**: optional bearer-token auth and TLS.
- **Lazy collection**: metrics are read only while a scrape is in flight, so an
  idle agent costs effectively nothing.

## Quick start

### Linux / macOS

```bash
curl -fsSL https://github.com/aritaone/watchcat-agent/raw/main/scripts/install.sh | sudo sh
```

With a shared secret and a custom port:

```bash
curl -fsSL https://github.com/aritaone/watchcat-agent/raw/main/scripts/install.sh | sudo LISTEN_ADDRESS=0.0.0.0:9800 BEARER_TOKEN=change-me sh
```

### Windows (elevated PowerShell)

```powershell
irm https://github.com/aritaone/watchcat-agent/raw/main/scripts/install.ps1 -OutFile install.ps1; .\install.ps1 -BearerToken "change-me"
```

The Windows script also opens the inbound firewall port for the Domain and
Private profiles.

### Install with the Go toolchain

On a machine that already has Go, skip the release download entirely:

```bash
go install github.com/aritaone/watchcat-agent/cmd/watchcat-agent@latest
```

That drops the binary in `$(go env GOPATH)/bin`. Install it as a service from
there:

```bash
sudo $(go env GOPATH)/bin/watchcat-agent install
```

Binaries built this way report `dev` for `watchcat_agent_build_info` — the
version is stamped by `make`, which `go install` does not run. Use a release
binary or `make build` when you want that label populated.

### Verify

```bash
curl -s http://127.0.0.1:9800/metrics | head
```

## Build from source

```bash
git clone https://github.com/aritaone/watchcat-agent.git
cd watchcat-agent

make build          # ./bin/watchcat-agent for this machine
make test           # unit tests with the race detector
make dist           # cross-compiled binaries + SHA256SUMS in ./dist
```

`make dist` builds assets for `linux/{amd64,arm64,arm}`, `darwin/{amd64,arm64}`
and `windows/{amd64,arm64}`, named
`watchcat-agent_<version>_<os>_<arch>` (`.exe` on Windows), alongside a
`SHA256SUMS` covering them. Every target is CGO-free, so cross-compiling needs
no toolchain beyond Go itself.

The release workflow additionally publishes each binary under a version-less
alias, `watchcat-agent_<os>_<arch>`, which is what the install scripts fetch
from `/releases/latest/download/`. Pass a `VERSION` to the scripts to pin the
versioned name instead.

## Commands

```
watchcat-agent run          Run in the foreground, or under a service manager
watchcat-agent install      Install as a system service and start it
watchcat-agent uninstall    Stop and remove the system service
watchcat-agent start|stop|restart|status
watchcat-agent config       Print the effective configuration
watchcat-agent version      Print build information
```

`install`, `uninstall`, `start`, `stop` and `restart` need root (Linux/macOS) or
Administrator (Windows).

Run it in the foreground to try it out, no install required:

```bash
./bin/watchcat-agent run --config configs/watchcat-agent.yaml --listen 127.0.0.1:9800
```

## Configuration

Precedence: **defaults → config file → `WATCHCAT_*` environment → flags.**
Every value is optional; the agent runs on defaults if no config file exists.

Default config paths:

| Platform | Path |
|---|---|
| Linux | `/etc/watchcat-agent/watchcat-agent.yaml` |
| macOS | `/usr/local/etc/watchcat-agent/watchcat-agent.yaml` |
| Windows | `%ProgramData%\watchcat-agent\watchcat-agent.yaml` |

See [configs/watchcat-agent.yaml](configs/watchcat-agent.yaml) for a fully
commented example. The essentials:

```yaml
server:
  listen_address: "0.0.0.0:9800"   # 0.0.0.0 so a remote Prometheus can reach it
  metrics_path: "/metrics"
  bearer_token: ""                 # require Authorization: Bearer <token>
  tls_cert_file: ""                # set both to serve HTTPS
  tls_key_file: ""
  scrape_timeout: 15s              # hard bound on one collection pass

collectors:
  cpu: true
  memory: true
  filesystem: true
  diskio: true
  network: true
  load: true
  host: true
  process: true
  go: true
  certificate: true
  per_cpu: false                   # one series per core; costly on big hosts
  certificate_paths:               # files or globs; empty still watches tls_cert_file
    - /etc/nginx/certs/*.pem

labels:                            # stamped onto every metric, keep low-cardinality
  env: production
  role: api

log:
  level: info                      # debug | info | warn | error
  file: ""                         # empty logs to stderr
```

Environment overrides: `WATCHCAT_CONFIG`, `WATCHCAT_LISTEN_ADDRESS`,
`WATCHCAT_BEARER_TOKEN`, `WATCHCAT_TLS_CERT_FILE`, `WATCHCAT_TLS_KEY_FILE`,
`WATCHCAT_LOG_LEVEL`, `WATCHCAT_LOG_FILE`, `WATCHCAT_SCRAPE_TIMEOUT`,
`WATCHCAT_PER_CPU`, `WATCHCAT_CERTIFICATE_PATHS`, `WATCHCAT_LABELS`
(`env=prod,role=api`).

Keep the token out of the config file where you can — pass it through the
environment instead, so a world-readable config never carries the secret.

## Prometheus setup

Add a job to your central Prometheus server — full example in
[configs/prometheus-scrape-example.yml](configs/prometheus-scrape-example.yml):

```yaml
scrape_configs:
  - job_name: watchcat
    scrape_interval: 30s
    scrape_timeout: 20s          # must exceed the agent's scrape_timeout
    static_configs:
      - targets:
          - linux-box-1.internal:9800
          - windows-box-1.internal:9800
          - mac-build-1.internal:9800
```

With auth and TLS enabled on the agent:

```yaml
    scheme: https
    tls_config:
      ca_file: /etc/prometheus/watchcat-ca.pem
    authorization:
      type: Bearer
      credentials_file: /etc/prometheus/watchcat.token
```

## API description

The three HTTP endpoints are described in OpenAPI 3.1:

- [api/openapi.yaml](api/openapi.yaml) — source of truth
- [api/openapi.json](api/openapi.json) — same document, for tooling that wants JSON

| Method | Path | Auth | Purpose |
|---|---|---|---|
| `GET` | `/metrics` | bearer, when configured | Prometheus scrape endpoint |
| `GET` | `/healthz` | none | liveness probe |
| `GET` | `/` | none | human-facing landing page |

`/metrics` moves if you set `server.metrics_path`; the other two are fixed.

```bash
npx @redocly/cli lint api/openapi.yaml --config api/redocly.yaml   # validate
npx @redocly/cli preview-docs api/openapi.yaml                     # browse
```

## Metrics

All series are prefixed `watchcat_`. Counters are cumulative — use `rate()`.

| Collector | Metrics |
|---|---|
| `cpu` | `cpu_seconds_total{cpu,mode}`, `cpu_cores`, `cpu_physical_cores` |
| `memory` | `memory_{total,available,used,free,cached,buffers}_bytes`, `memory_swap_*` |
| `filesystem` | `filesystem_{size,free,avail,used}_bytes`, `filesystem_files{,_free}`, `filesystem_readonly`, `filesystem_device_error` — labelled `device`, `mountpoint`, `fstype` |
| `diskio` | `disk_{reads,writes}_completed_total`, `disk_read_bytes_total`, `disk_written_bytes_total`, `disk_*_time_seconds_total`, `disk_io_now` |
| `network` | `network_{receive,transmit}_{bytes,packets,errs,drop}_total` per `device` |
| `load` | `load1`, `load5`, `load15` |
| `host` | `host_info{hostname,os,platform,…}`, `host_uptime_seconds`, `host_boot_time_seconds`, `host_time_seconds` |
| `process` | `processes`, plus the agent's own `agent_cpu_seconds_total`, `agent_resident_memory_bytes`, `agent_open_fds`, `agent_threads` |
| `certificate` | `certificate_not_after_seconds`, `certificate_not_before_seconds`, `certificate_info{subject,issuer,serial,dns_names}`, `certificate_read_error` — labelled `path`, `index` |
| meta | `scrape_duration_seconds{collector}`, `scrape_success{collector}`, `agent_build_info` |

`watchcat_scrape_success` is the metric to alert on: it goes to 0 for a single
collector without failing the rest of the scrape.

### Useful queries

#### CPU

```promql
# CPU utilisation, per host
100 - (avg by (instance) (rate(watchcat_cpu_seconds_total{mode="idle"}[5m])) * 100)
```

#### Memory

```promql
# Breakdown for a stacked graph — these three always sum to exactly total
watchcat_memory_total_bytes - watchcat_memory_available_bytes    # used, unreclaimable
watchcat_memory_available_bytes - watchcat_memory_free_bytes     # cache, buffers, purgeable
watchcat_memory_free_bytes                                       # genuinely untouched

# Memory in use, as a percentage
100 * (1 - watchcat_memory_available_bytes / watchcat_memory_total_bytes)

# Fleet-wide memory in use — sum the bytes, never average the percentages
100 * (1 - sum(watchcat_memory_available_bytes) / sum(watchcat_memory_total_bytes))

# Swap in use, guarded against hosts with no swap configured
100 * watchcat_memory_swap_used_bytes / watchcat_memory_swap_total_bytes
  and watchcat_memory_swap_total_bytes > 0

# Actively swapping out — the signal that correlates with pain
rate(watchcat_memory_swap_out_bytes_total[5m]) > 0
```

Derive the breakdown from `available`, not from `cached`: gopsutil leaves
`watchcat_memory_cached_bytes` and `_buffers_bytes` at 0 on macOS and Windows, so a
`used + cached + free` stack silently drops every reclaimable page on those hosts.
Never alert on `free` either — a healthy Linux box keeps it near zero on purpose,
because the kernel spends what it can on page cache.

#### Disk

```promql
# Disks over 85% full
100 * (1 - watchcat_filesystem_avail_bytes / watchcat_filesystem_size_bytes) > 85

# Average read latency across all disks, seconds per operation
sum by (instance) (rate(watchcat_disk_read_time_seconds_total[5m]))
/
sum by (instance) (rate(watchcat_disk_reads_completed_total[5m]))

# Average write latency across all disks
sum by (instance) (rate(watchcat_disk_write_time_seconds_total[5m]))
/
sum by (instance) (rate(watchcat_disk_writes_completed_total[5m]))

# Slow reads, but only where there is enough traffic for the average to mean anything
(
  sum by (instance) (rate(watchcat_disk_read_time_seconds_total[5m]))
  /
  sum by (instance) (rate(watchcat_disk_reads_completed_total[5m]))
) > 0.05
and
sum by (instance) (rate(watchcat_disk_reads_completed_total[5m])) > 10

# Fraction of wall-clock each device spent busy
rate(watchcat_disk_io_time_seconds_total[5m])
```

Sum numerator and denominator separately rather than averaging per-device ratios —
otherwise an idle disk doing two slow operations outweighs a busy one doing ten
thousand. These counters include queueing, so the result is time per request, not
device service time.

#### Certificates

```promql
# Certificates expiring within 30 days, soonest first
sort(watchcat_certificate_not_after_seconds - time() < 30 * 86400)

# Days left on each certificate, labelled with what it is for
(watchcat_certificate_not_after_seconds - time()) / 86400
  * on(path, index) group_left(subject, dns_names) watchcat_certificate_info

# A certificate has already expired
watchcat_certificate_not_after_seconds < time()

# A watched certificate file went missing or stopped parsing
watchcat_certificate_read_error == 1
```

#### Host and agent health

```promql
# Host rebooted in the last 10 minutes
watchcat_host_uptime_seconds < 600

# A collector is failing
watchcat_scrape_success == 0

# Slowest collectors across the fleet
topk(10, watchcat_scrape_duration_seconds)

# Collection wall-time per host — collectors run concurrently, so max, never sum
max by (instance) (watchcat_scrape_duration_seconds)

# Overhead outside the agent: network transit plus serialisation
scrape_duration_seconds{job="watchcat"}
- on(instance) group_left()
max by (instance) (watchcat_scrape_duration_seconds)

# Collection eating more than half the agent's 15s scrape_timeout
max_over_time((max by (instance) (watchcat_scrape_duration_seconds))[10m:1m]) > 7.5
```

`scrape_duration_seconds` without the prefix is Prometheus' own end-to-end
measurement; `watchcat_scrape_duration_seconds` is the agent's per-collector
breakdown. Subtracting one from the other separates a slow network from a slow
collector. When the agent is unreachable neither exists, so `up{job="watchcat"} == 0`
is the alert that actually fires.

## Security

The endpoint reports hostnames, mount points and network interfaces — treat it
as internal.

1. **Firewall the port** so only the Prometheus server can reach it. This is the
   single most effective control.
2. **Set `bearer_token`** so a stray connection cannot read your metrics.
3. **Enable TLS** when the scrape crosses an untrusted network. Prometheus
   verifies the certificate against `ca_file`.
4. **Bind to `127.0.0.1`** if Prometheus reaches the host through a tunnel or
   sidecar.

The agent needs no special privileges to read these counters; the bundled
systemd unit runs it under `DynamicUser` with an empty capability set. Root is
needed only to *install* the service.

## Troubleshooting

**Prometheus reports the target as down.** Check the port is reachable from the
Prometheus host (`curl http://server:9800/metrics`), then check the firewall.
`listen_address` must be `0.0.0.0:…`, not `127.0.0.1:…`.

**401 Unauthorized.** The agent has `bearer_token` set and Prometheus does not
send a matching one, or the `credentials_file` has a trailing newline.

**`install` fails.** It needs root/Administrator. Check the service afterwards
with `watchcat-agent status`, `journalctl -u watchcat-agent` (Linux),
`log show --predicate 'process == "watchcat-agent"'` (macOS) or Event Viewer
(Windows).

**`watchcat_scrape_success{collector="filesystem"} == 0`** usually means an
unreachable network mount. Add it to `filesystem_ignore_mount_points`.

**`watchcat_certificate_read_error == 1`.** The agent cannot read the file. The
usual cause is permissions: certificates often live in root-only directories
such as `/etc/ssl/private`, and the bundled systemd unit runs the agent under
`DynamicUser`. Grant just that path, for example with `SupplementaryGroups=ssl-cert`
or `ReadOnlyPaths=/etc/nginx/certs` in the unit. The same metric goes to 1 when
a path you named explicitly no longer exists, which is worth alerting on in its
own right.

**Too many series.** Turn off `per_cpu`, widen the `*_ignore_*` patterns, and
keep `labels` low-cardinality.

## Project layout

```
api/                   OpenAPI 3.1 description of the HTTP endpoints
cmd/watchcat-agent/    CLI entry point and subcommands
internal/agent/        lifecycle wiring and service-manager integration
internal/collector/    one file per metric source, plus the scrape dispatcher
internal/config/       defaults, YAML, environment, validation
internal/server/       HTTP endpoint, auth, TLS
configs/               example config, systemd unit, Prometheus job
scripts/               install/uninstall for Linux, macOS and Windows
```

Adding a collector: implement `collector.Subcollector` (a `Name()` and an
`Update(ctx, ch)`), then register it in `collector.New` behind a config flag.

## License

MIT — see [LICENSE](LICENSE). Copyright (c) 2026 Aritaone.
