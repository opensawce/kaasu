#!/bin/sh
# Builds the linux/amd64 binary twice in clean directories and fails if the hashes differ.
set -eu
version=${VERSION:-dev}
root=$(cd "$(dirname "$0")/.." && pwd)
hashes=""
for i in 1 2; do
  out=$(mktemp -d)
  (cd "$root" && GOCACHE="$out/gocache" CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false \
    -ldflags "-s -w -X main.version=$version" -o "$out/kaasu" ./cmd/kaasu)
  hashes="$hashes $(sha256sum "$out/kaasu" | cut -d' ' -f1)"
  rm -rf "$out"
done
set -- $hashes
if [ "$1" != "$2" ]; then
  echo "NOT reproducible: $1 != $2"
  exit 1
fi
echo "reproducible: $1"
