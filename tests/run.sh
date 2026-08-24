#!/bin/sh
# tests/run.sh — run every test.
#
#   ./tests/run.sh
#
# Each test is a standalone POSIX sh script exiting 0 pass / non-zero fail.
# This runs all of them and reports the roll-up. No framework, no dependencies.

set -u

here=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)

failed=0
total=0

for t in "$here"/*-test.sh; do
  [ -r "$t" ] || continue
  total=$((total + 1))
  name=$(basename "$t")
  if sh "$t"; then
    :
  else
    failed=$((failed + 1))
    echo "  ^^ $name FAILED"
  fi
  echo
done

if [ "$total" -eq 0 ]; then
  echo "no tests found in $here"
  exit 1
fi

# Lint, when available. Not having shellcheck is not a failure — it is not a
# dependency — but a finding it reports is.
root=$(dirname -- "$here")
if command -v shellcheck >/dev/null 2>&1; then
  if shellcheck -s sh \
      "$root"/scripts/default-script.sh.example \
      "$root"/scripts/lib/*.sh \
      "$root"/scripts/os/*.sh \
      "$root"/bootstrap/run.sh \
      "$root"/scripts/sweep.sh \
      "$root"/tests/*.sh; then
    echo "shellcheck: clean"
  else
    failed=$((failed + 1))
    total=$((total + 1))
    echo "  ^^ shellcheck FAILED"
  fi
  echo
else
  echo "shellcheck: not installed — skipped (brew install shellcheck)"
  echo
fi

if [ "$failed" -eq 0 ]; then
  echo "all $total test files passed"
  exit 0
fi
echo "$failed of $total test files FAILED"
exit 1
