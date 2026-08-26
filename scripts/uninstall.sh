#!/usr/bin/env sh
# Stop and remove the watchcat-agent service and its binary.
set -eu

[ "$(id -u)" -eq 0 ] || { echo "run as root (use sudo)" >&2; exit 1; }

bin=$(command -v watchcat-agent || echo /usr/local/bin/watchcat-agent)
if [ -x "$bin" ]; then
  "$bin" stop >/dev/null 2>&1 || true
  "$bin" uninstall || true
  rm -f "$bin"
  echo "==> removed $bin"
fi

# Configuration is left in place on purpose; delete it yourself if unwanted:
echo "==> config kept at /etc/watchcat-agent (linux) or /usr/local/etc/watchcat-agent (macOS)"
