#!/bin/sh
# Installs Kaasu on Linux with systemd. Idempotent. --dry-run prints what it would do and changes nothing.
set -eu

VERSION="${KAASU_VERSION:-latest}"
REPO="opensawce/kaasu"
DRY=0
[ "${1:-}" = "--dry-run" ] && DRY=1

run() {
  if [ "$DRY" = 1 ]; then echo "would: $*"; else "$@"; fi
}

need() { command -v "$1" >/dev/null 2>&1 || { echo "missing tool: $1" >&2; exit 1; }; }

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac
[ "$os" = linux ] || [ "$DRY" = 1 ] || { echo "this installer is for Linux; see the docs for macOS and Windows" >&2; exit 1; }

if [ "$DRY" = 0 ] && [ "$(id -u)" -ne 0 ]; then
  echo "run as root (sudo sh install.sh)" >&2
  exit 1
fi

asset="kaasu-$os-$arch"
if [ "$VERSION" = latest ]; then base="https://github.com/$REPO/releases/latest/download"; else base="https://github.com/$REPO/releases/download/$VERSION"; fi
echo "plan: download $asset and SHA256SUMS from $base"
run mkdir -p /var/lib/kaasu /etc/kaasu
run chmod 0750 /var/lib/kaasu
run useradd --system --no-create-home --shell /usr/sbin/nologin kaasu 2>/dev/null || true

if [ "$DRY" = 0 ]; then
  need curl; need sha256sum
  tmp=$(mktemp -d)
  curl -fsSL -o "$tmp/$asset" "$base/$asset"
  curl -fsSL -o "$tmp/SHA256SUMS" "$base/SHA256SUMS"
  (cd "$tmp" && grep " $asset\$" SHA256SUMS | sha256sum -c -)
  install -m 0755 "$tmp/$asset" /usr/local/bin/kaasu
  rm -rf "$tmp"
  if [ ! -f /etc/kaasu/master.key ]; then
    # Written under a private umask into a temporary file, then moved into place, so the key is never readable by others.
    ( umask 077; keytmp=$(mktemp /etc/kaasu/.master.key.XXXXXX) && /usr/local/bin/kaasu keygen > "$keytmp" && mv "$keytmp" /etc/kaasu/master.key )
  fi
  chmod 0400 /etc/kaasu/master.key
  chown root:root /etc/kaasu/master.key
  echo "verify the release signature with cosign (see SECURITY.md) if you have it installed"
else
  echo "would: install /usr/local/bin/kaasu after checking its SHA-256"
  echo "would: create /etc/kaasu/master.key (0400) with kaasu keygen if missing"
fi

here=$(cd "$(dirname "$0")" && pwd)
run install -m 0644 "$here/deploy/systemd/kaasu.service" /etc/systemd/system/kaasu.service
run systemctl daemon-reload
run systemctl enable kaasu
run systemctl restart kaasu # restart, so an already-running instance picks up the new binary and unit
echo "done. Next: kaasu admin init --name <your name> (run as the kaasu user, pointing at /var/lib/kaasu/kaasu.db)"
