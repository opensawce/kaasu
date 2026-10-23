#!/bin/sh
# Table test for check-pr-title.sh. Exits non-zero on the first wrong answer.
set -u
here=$(cd "$(dirname "$0")" && pwd)
check() {
  want=$1; title=$2
  if "$here/check-pr-title.sh" "$title" >/dev/null 2>&1; then got=pass; else got=fail; fi
  if [ "$got" != "$want" ]; then echo "FAIL want=$want got=$got: $title"; exit 1; fi
}
check pass "feat(server): unique amount pool"
check pass "fix!: never confirm twice"
check pass "docs: add privacy draft"
check pass "ci(web): deploy to Cloudflare"
check fail "Update stuff"
check fail "feat: "
check fail "feat(Server): capital scope"
check fail "random: thing"
echo "all title cases pass"
