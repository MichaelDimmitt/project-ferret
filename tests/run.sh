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

# Go tests, when a toolchain is available. Same rule as shellcheck: not having
# one is not a failure — mise is proposed, never silently installed — but a
# test it runs that fails is. Resolution mirrors scripts/sweep.sh.
go_bin=""
if command -v mise >/dev/null 2>&1; then
  go_bin=$(mise which go 2>/dev/null) || go_bin=""
fi
if [ -z "$go_bin" ]; then
  go_bin=$(command -v go 2>/dev/null) || go_bin=""
fi

if [ -n "$go_bin" ] && [ -x "$go_bin" ]; then
  if (cd "$root" && "$go_bin" vet ./... && "$go_bin" test ./...); then
    echo "go: vet and test clean"
  else
    failed=$((failed + 1))
    total=$((total + 1))
    echo "  ^^ go vet/test FAILED"
  fi

  # gofmt -l lists files needing formatting; empty output means clean.
  #
  # NOT `go fmt -n`, which prints the command it would run whether or not any
  # file needs it, and so always looks dirty. That bug shipped for one commit.
  gofmt_bin=$(dirname -- "$go_bin")/gofmt
  if [ -x "$gofmt_bin" ]; then
    unformatted=$(cd "$root" && "$gofmt_bin" -l . 2>/dev/null)
    if [ -n "$unformatted" ]; then
      echo "  ^^ gofmt FAILED — run: gofmt -w ."
      echo "$unformatted" | sed 's/^/       /'
      failed=$((failed + 1))
      total=$((total + 1))
    else
      echo "gofmt: clean"
    fi
  fi
  echo
else
  echo "go: no toolchain — skipped (mise install; see mise.toml)"
  echo
fi

if [ "$failed" -eq 0 ]; then
  echo "all $total test files passed"
  exit 0
fi
echo "$failed of $total test files FAILED"
exit 1
