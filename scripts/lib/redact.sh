#!/bin/sh
# scripts/lib/redact.sh — machine-identifying values out of a string.
#
# Sourced, never run. Standalone by design: it needs no $OUT, no table, and
# nothing else from scripts/os/. Any script that emits text a human might
# share can source this and filter values through it.
#
#   . "$root/scripts/lib/redact.sh"
#   safe=$(redact "$value")
#
# Contract: docs/design/BOOTSTRAP_PIPELINE.md §3.
#   - POSIX sh only. No bashisms.
#   - Pure: string in, string out. No files, no globals but its own.
#   - Level comes from FERRET_REDACT; the caller sets it, this never guesses.
#
# APPLY IT AT CAPTURE TIME, not to finished output.
#
# This is deliberately a *value* filter, not a stream filter. Running a
# finished document through a substitution pass is a denylist — it removes the
# patterns someone remembered to list, and silently passes an API key in a
# stack trace. Filtering each value as it is recorded is the allowlist
# discipline ARCHITECTURE.md requires of the runner (M5), and it is the reason
# scripts/os/common.sh calls this from inside cell(): every value reaches the
# table through one function, so a new probe cannot forget.
#
# LEVELS
#
#   0  off (default)  — the local file is yours; unredacted paths are what
#                       make a finding actionable.
#   1  identity       — who you are. Username, home, TMPDIR, repo path.
#                       Machine details stay, so a reader can still tell you
#                       your Docker is stale. For an issue tracker, a
#                       colleague, or an AI.
#   2  paranoid       — a superset of 1 for *substitution* purposes. The extra
#                       stripping at level 2 is machine fingerprint and
#                       security posture, which is not a string substitution:
#                       it is per-fact, so the caller decides. See
#                       row_posture() in scripts/os/common.sh for how the
#                       probe scripts do it.
#
# WHAT THE CALLER MUST PROVIDE
#
#   FERRET_REDACT   level, 0/1/2. Absent means 0.
#   FERRET_REPO     repo root, optional. Not derivable here — cwd may be
#                   anywhere — so a caller that has it should pass it.
#
# HOME, TMPDIR, and the username are read from the environment directly.

# A byte that cannot appear in a path or a version string, used as the sed
# delimiter so slashes in paths need no escaping. Computed once, not per call.
RD_D=$(printf '\001')

# The active level. A function, not a variable, so a caller can change
# FERRET_REDACT mid-run and have it take effect.
rd_level() { printf '%s' "${FERRET_REDACT:-0}"; }

# redact VALUE -> filtered VALUE on stdout
#
# At level 0 this is the identity function, so callers can route every value
# through it unconditionally rather than branching at each site.
redact() {
  [ "$(rd_level)" != "0" ] || { printf '%s' "$1"; return; }

  rd_user=$(id -un 2>/dev/null) || rd_user=""
  rd_home="${HOME:-}"
  rd_tmp="${TMPDIR:-}"
  rd_repo="${FERRET_REPO:-}"

  # Trailing slashes would leave "<tmpdir>/" vs "<tmpdir>" inconsistent.
  rd_tmp=${rd_tmp%/}
  rd_home=${rd_home%/}
  rd_repo=${rd_repo%/}

  # Longest-first: the repo usually sits under HOME, and TMPDIR can too.
  # Substituting the shorter one first would leave a half-redacted path
  # like "~/new_c/project-ferret" where "<repo>" was meant.
  #
  # Regex metacharacters in HOME are possible in principle but not in any real
  # home directory; the worst case is a missed substitution, which
  # tests/redaction-test.sh catches.
  rd_out=$1
  [ -n "$rd_repo" ] && rd_out=$(printf '%s' "$rd_out" | sed "s${RD_D}${rd_repo}${RD_D}<repo>${RD_D}g")
  [ -n "$rd_tmp" ]  && rd_out=$(printf '%s' "$rd_out" | sed "s${RD_D}${rd_tmp}${RD_D}<tmpdir>${RD_D}g")
  [ -n "$rd_home" ] && rd_out=$(printf '%s' "$rd_out" | sed "s${RD_D}${rd_home}${RD_D}~${RD_D}g")
  [ -n "$rd_user" ] && rd_out=$(printf '%s' "$rd_out" | sed "s${RD_D}${rd_user}${RD_D}<user>${RD_D}g")

  printf '%s' "$rd_out"
}
