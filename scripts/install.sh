#!/usr/bin/env sh
# Install watchcat-agent as a background service on Linux or macOS.
#
#   curl -fsSL https://raw.githubusercontent.com/aritaone/watchcat-agent/main/scripts/install.sh | sudo sh
#
# Options (environment variables):
#   VERSION          release tag to install (default: latest)
#   LISTEN_ADDRESS   bind address written into the config (default: 0.0.0.0:9800)
#   BEARER_TOKEN     shared secret required from Prometheus (default: none)
#   BINARY           path to a locally built binary, skips downloading
set -eu

REPO="aritaone/watchcat-agent"
VERSION="${VERSION:-latest}"
LISTEN_ADDRESS="${LISTEN_ADDRESS:-0.0.0.0:9800}"
BEARER_TOKEN="${BEARER_TOKEN:-}"
BINARY="${BINARY:-}"

log() { printf '==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "run as root (use sudo): installing a system service needs privileges"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux)  install_dir=/usr/local/bin; config_dir=/etc/watchcat-agent ;;
  darwin) install_dir=/usr/local/bin; config_dir=/usr/local/etc/watchcat-agent ;;
  *) die "unsupported operating system: $os" ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64)  arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  armv7l|armv6l) arch=arm ;;
  *) die "unsupported architecture: $arch" ;;
esac

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

if [ -n "$BINARY" ]; then
  log "using local binary $BINARY"
  cp "$BINARY" "$tmp/watchcat-agent"
else
  if [ "$VERSION" = "latest" ]; then
    url="https://github.com/$REPO/releases/latest/download/watchcat-agent_${os}_${arch}"
  else
    url="https://github.com/$REPO/releases/download/${VERSION}/watchcat-agent_${VERSION}_${os}_${arch}"
  fi
  log "downloading $url"
  curl -fsSL -o "$tmp/watchcat-agent" "$url" || die "download failed: $url"
fi

chmod 0755 "$tmp/watchcat-agent"
install -d "$install_dir"
install -m 0755 "$tmp/watchcat-agent" "$install_dir/watchcat-agent"
log "installed $install_dir/watchcat-agent"

install -d -m 0755 "$config_dir"
config="$config_dir/watchcat-agent.yaml"
if [ -f "$config" ]; then
  log "keeping existing config $config"
else
  cat > "$config" <<YAML
server:
  listen_address: "$LISTEN_ADDRESS"
  metrics_path: "/metrics"
  bearer_token: "$BEARER_TOKEN"
  scrape_timeout: 15s

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

labels: {}

log:
  level: info
YAML
  # The token is a secret; keep the file out of reach of other users.
  chmod 0600 "$config"
  log "wrote $config"
fi

# Replace any previous installation so the new binary and args take effect.
"$install_dir/watchcat-agent" uninstall >/dev/null 2>&1 || true
"$install_dir/watchcat-agent" install --config "$config"

log "done. verify with:"
log "  curl -s http://127.0.0.1:${LISTEN_ADDRESS##*:}/metrics | head"
