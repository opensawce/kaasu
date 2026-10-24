#!/bin/sh
# Fails if a money-path package that exists has less than MIN% statement coverage (default 85).
set -eu
min=${MIN:-85}
fail=0
for p in orders pool states ledger match; do
  dir="internal/$p"
  [ -d "$dir" ] || continue
  out=$(go test -cover "./$dir" 2>&1 | tail -1)
  pct=$(echo "$out" | sed -n 's/.*coverage: \([0-9.]*\)%.*/\1/p')
  [ -n "$pct" ] || { echo "$p: no coverage figure ($out)"; fail=1; continue; }
  ok=$(awk -v a="$pct" -v b="$min" 'BEGIN{print (a>=b)?"ok":"LOW"}')
  echo "$p: $pct% ($ok, need $min%)"
  [ "$ok" = ok ] || fail=1
done
exit $fail
