#!/bin/sh
# Runs every Go fuzz target for the given time (default 20s each). Exits non-zero if any finds a crash.
set -eu
t=${1:-20s}
found=0
pkgs=$(go list ./...)
for pkg in $pkgs; do
  dir=$(go list -f '{{.Dir}}' "$pkg")
  for f in $(grep -hoE '^func (Fuzz[A-Za-z0-9_]+)' "$dir"/*_test.go 2>/dev/null | awk '{print $2}'); do
    found=1
    echo "fuzz $pkg $f for $t"
    go test "$pkg" -run '^$' -fuzz "^${f}\$" -fuzztime "$t"
  done
done
[ "$found" = 1 ] || echo "no fuzz targets yet"
