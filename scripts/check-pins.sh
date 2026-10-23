#!/bin/sh
# Every `uses:` must be pinned to a full 40-character commit SHA. Fails with the offending lines.
set -eu
bad=$(grep -rhn '^[[:space:]]*-\{0,1\}[[:space:]]*uses:' .github/workflows | grep -v '@[0-9a-f]\{40\}' || true)
if [ -n "$bad" ]; then
  echo "unpinned actions:"
  echo "$bad"
  exit 1
fi
echo "all actions pinned"
