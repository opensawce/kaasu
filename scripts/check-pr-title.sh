#!/bin/sh
# Checks a pull request title follows Conventional Commits. Usage: check-pr-title.sh "<title>"
set -eu
title="${1:-}"
re='^(feat|fix|docs|test|chore|ci|refactor|perf|build|style|revert)(\([a-z0-9-]+\))?!?: .+'
if printf '%s' "$title" | grep -Eq "$re"; then
  echo "ok: $title"
else
  echo "not a conventional title: $title" >&2
  exit 1
fi
