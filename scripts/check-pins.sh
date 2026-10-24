#!/bin/sh
# Every external `uses:` must be pinned to a full 40-character commit SHA. Fails with the offending lines.
# Local actions (./path) and docker:// references are skipped. A trailing comment is ignored.
set -eu
bad=$(grep -rhn '^[[:space:]]*-\{0,1\}[[:space:]]*uses:' .github/workflows | awk '
  {
    line = $0
    sub(/^[^:]*:[[:space:]]*-?[[:space:]]*uses:[[:space:]]*/, "", line)
    sub(/[[:space:]]*#.*$/, "", line)
    gsub(/["\047]/, "", line)
    if (line ~ /^\.\// || line ~ /^docker:\/\//) next
    if (line !~ /@[0-9a-f]{40}$/) print $0
  }' || true)
if [ -n "$bad" ]; then
  echo "unpinned actions:"
  echo "$bad"
  exit 1
fi
echo "all actions pinned"
