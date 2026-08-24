#!/bin/sh
# tests/run.sh — run every test.
#
#   ./tests/run.sh
#
# Each test is a standalone POSIX sh script exiting 0 pass / non-zero fail.
# This runs all of them and reports the roll-up. No framework, no dependencies.

set -u

here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

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

if [ "$failed" -eq 0 ]; then
  echo "all $total test files passed"
  exit 0
fi
echo "$failed of $total test files FAILED"
exit 1
