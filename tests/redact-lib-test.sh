#!/bin/sh
# tests/redact-lib-test.sh — scripts/lib/redact.sh works on its own.
#
#   ./tests/redact-lib-test.sh
#
# The library's whole claim is that a future script can source it alone. That
# claim breaks the moment someone adds a dependency on common.sh, and it breaks
# silently — the probe scripts source both, so they would keep working while
# every other caller stopped. This test sources ONLY redact.sh, in a subshell
# with no probe state, and asserts the substitutions still happen.
#
# POSIX sh, no dependencies. Exits 0 pass, 1 fail.

set -u

here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
root=$(dirname -- "$here")
lib="$root/scripts/lib/redact.sh"

fails=0
ok()   { echo "  ok   $1"; }
fail() { echo "  FAIL $1"; fails=$((fails + 1)); }

echo "redact.sh standalone test"

[ -r "$lib" ] || { echo "  FAIL missing $lib"; exit 1; }

# Sourcing must not require $OUT, $OS_DIR, or any probe function. Run it in a
# subshell under `set -u` so an unset variable reference is a hard error.
if ( set -u; . "$lib" ) 2>/dev/null; then
  ok "sources cleanly with no probe state"
else
  fail "sourcing requires something it should not"
fi

# It must not pull in common.sh — directly or by side effect.
if ( set -u; . "$lib"; command -v row >/dev/null 2>&1 ) 2>/dev/null; then
  fail "sourcing redact.sh defined row() — it is coupled to common.sh"
else
  ok "does not drag in the probe machinery"
fi

# --- behaviour: level 0 is the identity function ---------------------------
# Callers route every value through redact() unconditionally, so level 0 must
# return input unchanged or that pattern corrupts unredacted output.
got=$( set -u; FERRET_REDACT=0; export FERRET_REDACT; . "$lib"; redact "$HOME/x" )
if [ "$got" = "$HOME/x" ]; then
  ok "level 0 is identity"
else
  fail "level 0 altered the value: '$got'"
fi

# --- behaviour: level 1 substitutes ----------------------------------------
check_sub() {
  label=$1; input=$2; want=$3
  got=$( set -u
         FERRET_REDACT=1
         FERRET_REPO="/tmp/fake-repo"
         export FERRET_REDACT FERRET_REPO
         . "$lib"
         redact "$input" )
  if [ "$got" = "$want" ]; then
    ok "$label"
  else
    fail "$label: got '$got' want '$want'"
  fi
}

u=$(id -un 2>/dev/null)
check_sub "home -> ~"          "$HOME/projects/x"  "~/projects/x"
check_sub "username -> <user>" "run by $u"         "run by <user>"
check_sub "repo -> <repo>"     "/tmp/fake-repo/a"  "<repo>/a"

# Longest-first ordering: a repo inside HOME must become <repo>, not ~/...
got=$( set -u
       FERRET_REDACT=1
       FERRET_REPO="$HOME/code/proj"
       export FERRET_REDACT FERRET_REPO
       . "$lib"
       redact "$HOME/code/proj/file.sh" )
if [ "$got" = "<repo>/file.sh" ]; then
  ok "repo inside HOME wins over home substitution"
else
  fail "ordering wrong: got '$got' want '<repo>/file.sh'"
fi

# Multiple values in one string all get replaced.
got=$( set -u
       FERRET_REDACT=1
       export FERRET_REDACT
       . "$lib"
       redact "cp $HOME/a /tmp/b # $u" )
case "$got" in
  *"$HOME"*) fail "home survived in a mixed string" ;;
  *"$u"*)    fail "username survived in a mixed string" ;;
  *)         ok "multiple values in one string" ;;
esac

# --- level 2 is a superset for substitution --------------------------------
# The extra level-2 stripping is per-fact (row_posture), not substitution, but
# identity must still be gone.
got=$( set -u; FERRET_REDACT=2; export FERRET_REDACT; . "$lib"; redact "$HOME/x" )
if [ "$got" = "~/x" ]; then
  ok "level 2 still substitutes identity"
else
  fail "level 2 lost identity substitution: '$got'"
fi

echo
if [ "$fails" -eq 0 ]; then
  echo "PASS"
  exit 0
fi
echo "FAIL ($fails)"
exit 1
