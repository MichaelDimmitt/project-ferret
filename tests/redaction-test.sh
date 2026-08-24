#!/bin/sh
# tests/redaction-test.sh — assert the redacted output leaks nothing.
#
#   ./tests/redaction-test.sh
#
# Redaction that is only spot-checked is redaction that regresses. This runs
# both modes and asserts the machine-identifying values are absent from the
# redacted file — and present in the full one, so a test that passes because
# redaction silently emptied the table is caught too.
#
# POSIX sh, no dependencies. Exits 0 pass, 1 fail.

set -u

here=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
root=$(dirname -- "$here")
tmp="${TMPDIR:-/tmp}/ferret-redaction-test.$$"
mkdir -p "$tmp" || exit 1
# shellcheck disable=SC2064
trap "rm -rf '$tmp'" EXIT INT TERM

script="$root/scripts/default-script.sh.example"
full="$tmp/full.md"
red="$tmp/redacted.md"
par="$tmp/paranoid.md"

fails=0
ok()   { echo "  ok   $1"; }
fail() { echo "  FAIL $1"; fails=$((fails + 1)); }

echo "redaction test"

FERRET_REPO="$root"
export FERRET_REPO

FERRET_REDACT=0 sh "$script" "$full" >/dev/null 2>&1 || { echo "full run failed"; exit 1; }
FERRET_REDACT=1 sh "$script" "$red"  >/dev/null 2>&1 || { echo "identity run failed"; exit 1; }
FERRET_REDACT=2 sh "$script" "$par"  >/dev/null 2>&1 || { echo "paranoid run failed"; exit 1; }

# --- the table must survive redaction --------------------------------------
# A redacted file that leaks nothing because it contains nothing is not a
# pass. Both files must have the same number of rows.
n_full=$(grep -c '^| ' "$full")
n_red=$(grep -c '^| ' "$red")
n_par=$(grep -c '^| ' "$par")
if [ "$n_full" -eq "$n_red" ] && [ "$n_full" -gt 10 ]; then
  ok "row count preserved at identity level ($n_red rows)"
else
  fail "row count: full=$n_full identity=$n_red"
fi

# Paranoid merges clock-local + clock-remote into one clock-skew row, so it is
# allowed to be exactly one shorter — but no shorter than that.
if [ "$n_par" -eq "$n_full" ] || [ "$n_par" -eq "$((n_full - 1))" ]; then
  ok "row count preserved at paranoid level ($n_par rows)"
else
  fail "row count: full=$n_full paranoid=$n_par (expected $n_full or $((n_full - 1)))"
fi

# Cells must not be empty — the failure mode when the sed delimiter breaks.
for f in "$red" "$par"; do
  if grep -q '^|  *|  *|  *|  *| ` *` |$' "$f"; then
    fail "$(basename "$f") contains empty rows"
  else
    ok "no empty rows in $(basename "$f")"
  fi
done

# --- the actual guarantee ---------------------------------------------------
check_absent() {
  file=$1; label=$2; value=$3
  [ -n "$value" ] || { ok "$label (not set, skipped)"; return; }
  n=$(grep -cF "$value" "$file")
  if [ "$n" -eq 0 ]; then
    ok "$label absent"
  else
    fail "$label LEAKED $n times in $(basename "$file")"
    grep -nF "$value" "$file" | head -2 | sed 's/^/       /'
  fi
}

# Identity must be gone at BOTH redacted levels — paranoid is a superset.
for f in "$red" "$par"; do
  echo "  -- $(basename "$f")"
  check_absent "$f" "username"  "$(id -un 2>/dev/null)"
  check_absent "$f" "home path" "${HOME:-}"
  check_absent "$f" "tmpdir"    "$(printf '%s' "${TMPDIR:-}" | sed 's:/*$::')"
  check_absent "$f" "repo path" "$root"
done

# --- and the control: full mode must still contain them ---------------------
# Without this, redaction could be a no-op that passes by accident.
u=$(id -un 2>/dev/null)
if [ -n "$u" ] && grep -qF "$u" "$full"; then
  ok "full mode retains username (control)"
else
  fail "full mode is missing the username — redaction may be always-on"
fi

# --- capability rows must survive -------------------------------------------
# Redaction removes identity, not usefulness. If these are gone, the shareable
# file cannot be acted on and the feature is pointless.
echo "  -- identity level keeps capability"
for want in 'os-support' 'internet' 'sudo' 'arch'; do
  if grep -q "^| $want " "$red"; then
    ok "kept: $want"
  else
    fail "identity level removed $want"
  fi
done

# --- level 2: posture removed, capability kept ------------------------------
echo "  -- paranoid level strips posture"

# The row must still EXIST (absence and unknown are different results) but
# carry no value.
posture_redacted() {
  if ! grep -q "^| $1 " "$par"; then
    fail "$1 row vanished at paranoid level (should be <redacted>, not absent)"
  elif grep -q "^| $1 | <redacted> " "$par"; then
    ok "stripped: $1"
  else
    fail "$1 NOT stripped at paranoid level: $(grep "^| $1 " "$par" | head -1)"
  fi
}

posture_redacted sudo
posture_redacted kernel
posture_redacted locale
[ "$(uname -s)" = "Darwin" ] && { posture_redacted cpu; posture_redacted sip; posture_redacted macos-build; }

# Absolute clocks gone, but the skew finding kept — it taints other checks.
if grep -q '^| clock-skew ' "$par"; then
  ok "clock-skew kept (it is a real finding)"
else
  fail "clock-skew missing at paranoid level"
fi
if grep -q '^| clock-local ' "$par"; then
  fail "absolute clock still present at paranoid level"
else
  ok "absolute clocks removed"
fi

# The whole point of level 2 is that it stays useful. Tool versions must live.
echo "  -- paranoid level keeps tool versions"
for want in 'git' 'python3' 'internet' 'arch' 'os'; do
  if grep -q "^| $want " "$par"; then
    ok "kept: $want"
  else
    fail "paranoid level removed $want"
  fi
done

# A version string must actually survive, not just the row label.
if grep -qE '^\| git \| git version [0-9]' "$par"; then
  ok "git version string intact"
else
  fail "paranoid level destroyed version strings — file is useless"
fi

echo
if [ "$fails" -eq 0 ]; then
  echo "PASS"
  exit 0
fi
echo "FAIL ($fails)"
exit 1
