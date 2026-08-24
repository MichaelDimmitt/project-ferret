#!/bin/sh
# tests/bootstrap-fallback-test.sh — the Tier-1 fallback never reports GO.
#
#   ./tests/bootstrap-fallback-test.sh
#
# The fallback runs when mise was declined or no Go toolchain exists. Its whole
# claim is that it still produces an answer, and that the answer is honest
# about how partial it is. The invariant under test:
#
#   A clean machine, with every Tier-1 check passing, still exits 2 and still
#   says UNKNOWN. Never 0, never GO.
#
# That is the one property that cannot be allowed to regress. Most of the
# manifest was never read, so "nothing failed" is a statement about eight
# checks, not about the machine -- and a green here is the false green this
# tool exists to prevent, emitted from the code path that runs on the most
# broken machines.
#
# POSIX sh, no dependencies. Exits 0 pass, 1 fail.

set -u

here=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
root=$(dirname -- "$here")
script="$root/ferret/bootstrap.sh"

fails=0
ok()   { echo "  ok   $1"; }
fail() { echo "  FAIL $1"; fails=$((fails + 1)); }

echo "bootstrap fallback test"

[ -r "$script" ] || { echo "  FAIL missing $script"; exit 1; }

command -v git >/dev/null 2>&1 || { echo "  SKIP git unavailable"; exit 0; }

# A clean fixture repo, with the script inside it and every output written
# OUTSIDE it. Writing status.md into the repo under test makes the working-tree
# check fire on the test's own artefacts -- which happened while developing
# this, and looked exactly like a false positive in the script.
tmp=$(mktemp -d) || { echo "  FAIL no temp dir"; exit 1; }
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

mkdir -p "$tmp/repo/sub" "$tmp/out"
cp "$script" "$tmp/repo/sub/bootstrap.sh"

(
  cd "$tmp/repo" || exit 1
  git init -q .
  git -c user.email=t@t.invalid -c user.name=T add -A
  git -c user.email=t@t.invalid -c user.name=T commit -q -m init
) >/dev/null 2>&1 || { echo "  SKIP could not build a fixture repo"; exit 0; }

# --- the invariant ----------------------------------------------------------

# PATH is normalised to a clean, duplicate-free value on purpose.
#
# Inherited, this test takes whichever branch the developer's own PATH happens
# to produce: on a machine with duplicated entries it exits 1, and the exit-2
# assertion below -- the one invariant that must never regress -- never runs at
# all. A test whose coverage depends on the tester's dotfiles is not a test.
#
# HOME is kept: git needs it, and it is not what makes this deterministic.
env PATH=/usr/bin:/bin HOME="${HOME:-$tmp}" \
  sh "$tmp/repo/sub/bootstrap.sh" --status "$tmp/out/status.md" >"$tmp/out/term.txt" 2>&1
code=$?

# Exit code, checked from $? directly. Piping to grep would report grep's
# status -- the mistake that produced two false "verified" claims in this
# project, per docs/plan/CONTINUE.md.
if [ "$code" -eq 2 ]; then
  ok "clean machine exits 2 (unknowns present)"
elif [ "$code" -eq 0 ]; then
  fail "exited 0 — the fallback claimed GO on an unread manifest"
else
  # A real blocker on the test machine is legitimate; anything else is not.
  if [ "$code" -eq 1 ] && grep -q '^Blockers' "$tmp/out/term.txt"; then
    ok "exited 1 with blockers named (this machine has a real finding)"
  else
    fail "unexpected exit $code"
  fi
fi

if [ "$code" -ne 0 ]; then
  ok "never exits 0 from the fallback path"
else
  fail "exit 0 is not reachable honestly here"
fi

# --- the headline -----------------------------------------------------------

if grep -q '^VERDICT  *GO$' "$tmp/out/term.txt"; then
  fail "headline said GO"
else
  ok "headline is never a bare GO"
fi

# The headline and the exit code must agree. The first version printed
# "VERDICT UNKNOWN" while exiting 1, so a human and a CI job read the same run
# differently.
if [ "$code" -eq 1 ]; then
  if grep -q '^VERDICT  *NO-GO' "$tmp/out/term.txt"; then
    ok "exit 1 is headlined NO-GO"
  else
    fail "exit 1 without a NO-GO headline — the reader and CI disagree"
  fi
fi
if [ "$code" -eq 2 ]; then
  if grep -q '^VERDICT  *UNKNOWN' "$tmp/out/term.txt"; then
    ok "exit 2 is headlined UNKNOWN"
  else
    fail "exit 2 without an UNKNOWN headline"
  fi
fi

# --- partiality is stated ---------------------------------------------------
#
# ARCHITECTURE.md §3: the reason this report is incomplete is the most
# important thing on screen. A partial answer that looks complete is the
# failure mode.

if grep -q 'runner unavailable' "$tmp/out/term.txt"; then
  ok "states 'runner unavailable' on the terminal"
else
  fail "did not say why the report is partial"
fi

if [ -r "$tmp/out/status.md" ]; then
  ok "wrote status.md"
  if grep -q 'runner unavailable' "$tmp/out/status.md"; then
    ok "status.md states its partiality too"
  else
    fail "status.md looks like a complete report"
  fi
  # status.md is the artefact most likely to be pasted into a ticket, so it
  # must not imply a verdict the run did not earn.
  if grep -q '^\*\*GO\*\*' "$tmp/out/status.md"; then
    fail "status.md headlined GO"
  else
    ok "status.md never headlines GO"
  fi
else
  fail "no status.md written"
fi

# --- read-only --------------------------------------------------------------
#
# Phase 1 is read-only, and that binds the fallback exactly as it binds the Go
# runner. This is the same guarantee ferret/readonly_test.go asserts, on the
# code path that runs when the Go runner cannot.

dirty=$(cd "$tmp/repo" && git status --porcelain 2>/dev/null)
if [ -z "$dirty" ]; then
  ok "left the fixture repo byte-identical"
else
  fail "the fallback modified the repo it measured: $dirty"
fi

# --- never reads a secret ---------------------------------------------------
#
# AGENTS.md binds this script too. The CA probe reads variables that point AT
# files; it must never open them.

if grep -Eq '\b(cat|head|tail|less|more)\b[^|;&]*\$(val|SSL_CERT_FILE|NODE_EXTRA_CA_CERTS)' "$script"; then
  fail "the script opens a file named by a CA/credential variable"
else
  ok "never opens the files those variables point at"
fi

echo
if [ "$fails" -eq 0 ]; then
  echo "PASS"
  exit 0
fi
echo "FAIL ($fails)"
exit 1
