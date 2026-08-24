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
# Shell, not Go: stage zero measures whether a toolchain exists, so it
# cannot be written in one. The probes themselves live in scripts/os/ — a
# portable core plus one script per OS — and the seeded script dispatches on
# `uname -s`. An OS with no script still gets the common core and records
# os-support: unknown.
#
# Contract: docs/design/BOOTSTRAP_PIPELINE.md §2.

set -u

here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
root=$(dirname -- "$here")

example="$root/scripts/default-script.sh.example"
script="$here/default-script.sh"
out="$here/default-results.md"

# --redact writes a separate, shareable file. It never overwrites the full
# results: the unredacted copy is the one that makes findings actionable, and
# silently replacing it would be a bad trade the user didn't ask for.
redact=0
for arg in "$@"; do
  case "$arg" in
    --redact|--redact=identity)
      redact=1
      out="$here/default-results.redacted.md"
      ;;
    --redact=paranoid)
      redact=2
      out="$here/default-results.redacted.md"
      ;;
    -h|--help)
      echo "usage: ./bootstrap/run.sh [--redact[=identity|paranoid]]"
      echo
      echo "  (no args)  measure this machine"
      echo "             -> bootstrap/default-results.md"
      echo
      echo "  --redact   remove identity: username, home paths, TMPDIR, repo"
      echo "             path, PATH contents, uid. Machine details stay, so a"
      echo "             reader can still tell you your Docker is stale."
      echo "             For an issue tracker, a colleague, or an AI."
      echo "             -> bootstrap/default-results.redacted.md"
      echo
      echo "  --redact=paranoid"
      echo "             also removes fingerprint and posture: exact OS build,"
      echo "             CPU, SIP/sudo state, kernel revision, locale, clocks."
      echo "             Tool versions stay. For somewhere public and indexed."
      echo "             -> bootstrap/default-results.redacted.md"
      exit 0
      ;;
    --redact=*)
      echo "bootstrap: unknown redaction level: ${arg#--redact=}" >&2
      echo "bootstrap: expected 'identity' or 'paranoid'" >&2
      exit 2
      ;;
    *)
      echo "bootstrap: unknown argument: $arg" >&2
      echo "bootstrap: try --help" >&2
      exit 2
      ;;
  esac
done

if [ ! -f "$example" ]; then
  echo "bootstrap: missing $example" >&2
  exit 1
fi

if [ ! -r "$root/scripts/os/common.sh" ]; then
  echo "bootstrap: missing $root/scripts/os/common.sh" >&2
  exit 1
fi

if [ ! -f "$script" ]; then
  echo "bootstrap: seeding default-script.sh from scripts/default-script.sh.example"
  cp "$example" "$script" || exit 1
  chmod +x "$script" 2>/dev/null || :
fi

# Prefer the machine's sh; the script is POSIX and must not need more.
FERRET_REDACT="$redact"
# The repo path leaks your directory conventions and the project name, and it
# is not derivable inside the probe script (cwd may be anywhere). Pass it.
FERRET_REPO="$root"
export FERRET_REDACT FERRET_REPO
sh "$script" "$out" || {
  echo "bootstrap: probe script failed" >&2
  exit 1
}

echo "bootstrap: wrote $out"
