#!/bin/sh
# scripts/sweep.sh — the runner entry point.
#
#   ./scripts/sweep.sh [args...]
#
# This is stage one, not stage zero. Stage zero (./bootstrap/run.sh) is shell
# and measures the machine, including whether a toolchain exists at all. This
# script may only assume what stage zero confirmed.
#
# So it does not run the binary blind. A missing runner is a finding — it
# routes to ferret/bootstrap.sh (M6) and yields verdict UNKNOWN, never a crash
# and never GO.
#
# Resolution order, per docs/design/LANGUAGE_CHOICE.md:
#   1. An already-built binary        — nothing to install, fastest path
#   2. `go build` via mise's toolchain — mise is already present and approved
#   3. `go build` via a system Go      — no mise, but a toolchain exists
#   4. Tier-1 shell fallback           — mise declined or unavailable
#
# mise is never installed here. It is proposed for approval like any other
# tool; installing it silently to satisfy Ferret's own runtime would mutate
# the machine before a baseline exists, which the invariants forbid.
#
# Contract: docs/design/ARCHITECTURE.md §3, docs/plan/PLAN.md M0/M6.

set -u

here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
root=$(dirname -- "$here")

bin="$root/ferret/ferret"

# GO and GO_SOURCE record which toolchain won and how it was found. "Which Go
# built this" is exactly the ambient state Ferret exists to surface, and
# Ferret is not exempt from its own rule.
GO=""
GO_SOURCE=""

find_go() {
  # mise first: if it is present it is already approved, and its toolchain is
  # the pinned one from mise.toml. Failure is not an error — it just means
  # mise has no Go, so we fall through.
  if command -v mise >/dev/null 2>&1; then
    fg_bin=$(mise which go 2>/dev/null) || fg_bin=""
    if [ -n "$fg_bin" ] && [ -x "$fg_bin" ]; then
      GO="$fg_bin"
      GO_SOURCE="mise"
      return 0
    fi
  fi

  fg_bin=$(command -v go 2>/dev/null) || fg_bin=""
  if [ -n "$fg_bin" ] && [ -x "$fg_bin" ]; then
    GO="$fg_bin"
    GO_SOURCE="system"
    return 0
  fi

  return 1
}

# 1. Already built.
if [ -x "$bin" ]; then
  exec "$bin" "$@"
fi

# 2/3. Build it, if a toolchain is available.
if find_go; then
  cd "$root" || exit 1
  if "$GO" build -o "$bin" ./ferret 2>&1; then
    exec "$bin" "$@"
  fi
  echo "ferret: build failed (go: $GO [$GO_SOURCE])" >&2
  echo "ferret: this is a bug in Ferret, not a finding about your machine" >&2
  exit 3
fi

# 4. No toolchain. A finding, not an error.
echo "ferret: no Go toolchain found — falling back to bootstrap checks" >&2
echo "ferret: 'mise install' provides one (see mise.toml); it is never" >&2
echo "ferret: installed automatically" >&2

fallback="$root/ferret/bootstrap.sh"
if [ -x "$fallback" ] || [ -r "$fallback" ]; then
  sh "$fallback" "$@"
  exit $?
fi

echo "ferret: bootstrap fallback not implemented yet (M6)" >&2
echo "ferret: run ./bootstrap/run.sh to measure this machine without a runner" >&2
exit 3
