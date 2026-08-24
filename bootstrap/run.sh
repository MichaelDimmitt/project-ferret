#!/bin/sh
# bootstrap/run.sh — stage-zero entry point.
#
# Measures this machine and writes bootstrap/default-results.md, by invoking
# the probe script in scripts/. Run this; don't call scripts/ directly.
#
#   ./bootstrap/run.sh
#
# On first run, seeds bootstrap/default-script.sh from the committed example
# in scripts/. That copy is gitignored and yours to adapt — subsequent runs
# use it as-is and never overwrite your edits.
#
# Contract: docs/design/BOOTSTRAP_PIPELINE.md §2.

set -u

here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
root=$(dirname -- "$here")

example="$root/scripts/default-script.sh.example"
script="$here/default-script.sh"
out="$here/default-results.md"

if [ ! -f "$example" ]; then
  echo "bootstrap: missing $example" >&2
  exit 1
fi

if [ ! -f "$script" ]; then
  echo "bootstrap: seeding default-script.sh from scripts/default-script.sh.example"
  cp "$example" "$script" || exit 1
  chmod +x "$script" 2>/dev/null || :
fi

# Prefer the machine's sh; the script is POSIX and must not need more.
sh "$script" "$out" || {
  echo "bootstrap: probe script failed" >&2
  exit 1
}

echo "bootstrap: wrote $out"
