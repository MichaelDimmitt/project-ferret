#!/bin/sh
# ferret/bootstrap.sh — the Tier-1 fallback, when the runner cannot run.
#
#   sh ferret/bootstrap.sh [--status PATH]
#
# Reached from scripts/sweep.sh when no Go toolchain was found: mise was
# declined, or could not install, or this machine simply has neither. That is a
# finding about the machine, not an error, and it must still produce an answer.
#
# THE VERDICT HERE IS ALWAYS UNKNOWN. Never GO. This script runs a handful of
# Tier-1 checks out of a manifest it cannot read -- it has no JSON parser, no
# expect evaluation, no taint graph -- so the most it can honestly say is "here
# is what I could see, and here is how much I could not." A partial answer that
# announces its partiality is correct behaviour; a partial answer that looks
# complete is the failure mode this tool exists to prevent.
#
# Contract: docs/design/ARCHITECTURE.md §3, docs/plan/PLAN.md M6.
#   - POSIX sh. It runs on the machine that has nothing.
#   - Installs nothing, mutates nothing. Phase 1 is read-only here too.
#   - Never reads a secret (AGENTS.md). Presence and shape only.
#   - Exit 2 (unknowns present), never 0. Exit 3 if it cannot even do this.

set -u

# Exit codes, per docs/design/ARCHITECTURE.md §11. Shared with main.go, so they
# must not drift: 0 GO / 1 NO-GO / 2 unknowns / 3 could not determine.
EXIT_NOGO=1
EXIT_UNKNOWN=2
EXIT_CANNOT=3

here=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
root=$(dirname -- "$here")

status_path="$root/.ferret/status.md"
while [ $# -gt 0 ]; do
  case "$1" in
    --status) shift; [ $# -gt 0 ] || { echo "bootstrap: --status needs a path" >&2; exit "$EXIT_CANNOT"; }; status_path="$1" ;;
    --status=*) status_path=${1#--status=} ;;
    *) : ;;  # Unknown flags are the Go runner's; ignore rather than fail.
  esac
  shift
done

# --- counting ---------------------------------------------------------------
#
# "N of M checks not run" needs M, and M lives in a JSON manifest this script
# cannot parse. Rather than invent a number, count `"id":` occurrences -- which
# is a grep, not a parse, and is therefore honest about being approximate.
#
# When the manifest is empty (as it is until M7), M is 0 and the header says so
# plainly instead of printing a fake denominator.
manifest_total() {
  mt_dir="$root/manifest"
  [ -d "$mt_dir" ] || { echo 0; return; }
  # Exclude _schema.json: its "id" keys are schema vocabulary, not checks.
  mt_n=$(cat "$mt_dir"/[0-9]*.json 2>/dev/null | grep -c '"id"[[:space:]]*:') || mt_n=0
  echo "${mt_n:-0}"
}

# --- findings ---------------------------------------------------------------
#
# Accumulated as lines: STATE|TITLE|DETAIL|REMEDY. Held in a file rather than a
# variable because a subshell in a pipeline cannot write back to its parent,
# and every probe below runs in one.
findings=$(mktemp) || { echo "bootstrap: no temp dir" >&2; exit "$EXIT_CANNOT"; }
trap 'rm -f "$findings"' EXIT HUP INT TERM

n_go=0
n_nogo=0
n_unknown=0

finding() {
  printf '%s|%s|%s|%s\n' "$1" "$2" "$3" "${4:-}" >>"$findings"
  case "$1" in
    GO)      n_go=$((n_go + 1)) ;;
    NO-GO)   n_nogo=$((n_nogo + 1)) ;;
    UNKNOWN) n_unknown=$((n_unknown + 1)) ;;
  esac
}

# --- Tier-1 probes ----------------------------------------------------------
#
# ARCHITECTURE.md §3 names these: clock, disk, inodes, arch, libc, PATH, CA,
# git state. They are the checks that invalidate everything else, and they are
# all cheap shell -- which is exactly why they are the ones worth having when
# the runner is gone.
#
# Deliberately NOT the full Tier-1 list. M7 decides what these checks actually
# assert, and writing that here first would mean two implementations of the
# same rule drifting apart. These are the ones whose answer is unambiguous.

probe_clock() {
  if ! command -v curl >/dev/null 2>&1; then
    finding UNKNOWN "Clock skew" "no curl; cannot reach a time reference" ""
    return
  fi
  remote=$(curl -sI --max-time 5 https://example.com 2>/dev/null \
           | grep -i '^date:' | cut -d' ' -f2- | tr -d '\r')
  if [ -z "$remote" ]; then
    finding UNKNOWN "Clock skew" "no network reference available" ""
    return
  fi
  # Neither GNU -d nor BSD -j -f is portable; try both, and report honestly
  # rather than guessing when neither parses.
  r_epoch=$(date -u -d "$remote" '+%s' 2>/dev/null \
            || date -u -j -f '%a, %d %b %Y %H:%M:%S %Z' "$remote" '+%s' 2>/dev/null) || r_epoch=""
  l_epoch=$(date -u '+%s' 2>/dev/null) || l_epoch=""
  if [ -z "$r_epoch" ] || [ -z "$l_epoch" ]; then
    finding UNKNOWN "Clock skew" "could not parse the remote date" ""
    return
  fi
  skew=$((l_epoch - r_epoch))
  [ "$skew" -lt 0 ] && skew=$((-skew))
  if [ "$skew" -le 300 ]; then
    finding GO "Clock skew" "${skew}s from network time" ""
  else
    finding NO-GO "Clock skew" "${skew}s from network time; TLS and tokens fail past ~300s" \
      "sudo sntp -sS time.apple.com   # or enable automatic time sync"
  fi
}

probe_disk() {
  # -P forces POSIX output: one line per filesystem, no wrapping on long
  # device names. Without it the column offsets move and the parse is wrong.
  avail=$(df -Pk . 2>/dev/null | awk 'NR==2 {print $4}') || avail=""
  if [ -z "$avail" ]; then
    finding UNKNOWN "Disk space" "df did not report a usable figure" ""
    return
  fi
  mb=$((avail / 1024))
  if [ "$mb" -lt 500 ]; then
    finding NO-GO "Disk space" "${mb}MB free; installs and builds will fail" \
      "free space on this volume, then re-run"
  else
    finding GO "Disk space" "${mb}MB free" ""
  fi
}

probe_inodes() {
  # Inode exhaustion looks exactly like a full disk to npm, with plenty of
  # bytes free. -Pi is POSIX on macOS; Linux uses --inodes and prints IUse%.
  ipct=$(df -Pi . 2>/dev/null | awk 'NR==2 {gsub(/%/,"",$8); print $8}') || ipct=""
  case "$ipct" in
    ''|*[!0-9]*) finding UNKNOWN "Inodes" "this platform's df does not report inode use" "" ;;
    *)
      if [ "$ipct" -ge 95 ]; then
        finding NO-GO "Inodes" "${ipct}% used; writes fail with space still free" \
          "delete unused files or node_modules trees on this volume"
      else
        finding GO "Inodes" "${ipct}% used" ""
      fi ;;
  esac
}

probe_arch() {
  a=$(uname -m 2>/dev/null) || a=""
  if [ -z "$a" ]; then
    finding UNKNOWN "Architecture" "uname -m failed" ""
  else
    finding GO "Architecture" "$a" ""
  fi
}

probe_libc() {
  # Which libc decides whether a prebuilt binary runs at all. musl and glibc
  # are the split that matters, and Alpine is the common musl case.
  case "$(uname -s 2>/dev/null)" in
    Darwin) finding GO "libc" "darwin (system libSystem)" "" ; return ;;
    Linux) : ;;
    *) finding UNKNOWN "libc" "not determined on this OS" "" ; return ;;
  esac
  if [ -f /etc/alpine-release ]; then
    finding GO "libc" "musl (alpine)" ""
  elif ldd --version 2>&1 | grep -qi musl; then
    finding GO "libc" "musl" ""
  elif ldd --version 2>&1 | grep -qi 'glibc\|gnu libc'; then
    finding GO "libc" "glibc" ""
  else
    finding UNKNOWN "libc" "ldd did not identify a flavour" ""
  fi
}

probe_path() {
  # Duplicate PATH entries are not fatal, but they are a real finding: they
  # slow every exec and they are usually a symptom of a shell rc sourced twice.
  n=$(printf '%s' "${PATH:-}" | tr ':' '\n' | grep -c .) || n=0
  u=$(printf '%s' "${PATH:-}" | tr ':' '\n' | grep . | sort -u | wc -l | tr -d ' ') || u=0
  if [ "$n" -eq 0 ]; then
    finding UNKNOWN "PATH" "empty or unreadable" ""
  elif [ "$n" -ne "$u" ]; then
    finding NO-GO "PATH" "$n entries, $u unique — $((n - u)) duplicated" \
      "de-duplicate PATH in your shell rc"
  else
    finding GO "PATH" "$n entries, no duplicates" ""
  fi
}

probe_ca() {
  # A CA bundle pointed at a file that does not exist breaks every HTTPS call
  # with an error that names TLS rather than the variable. Presence only --
  # never open it.
  for var in SSL_CERT_FILE NODE_EXTRA_CA_CERTS REQUESTS_CA_BUNDLE CURL_CA_BUNDLE; do
    eval "val=\${$var:-}"
    [ -n "$val" ] || continue
    if [ -r "$val" ]; then
      finding GO "CA bundle ($var)" "set and readable" ""
    else
      finding NO-GO "CA bundle ($var)" "points at a path that is not readable" \
        "unset $var, or point it at a readable bundle"
    fi
  done
}

probe_git() {
  if ! command -v git >/dev/null 2>&1; then
    finding UNKNOWN "Git" "git is not installed; repo state unknown" ""
    return
  fi
  if ! git -C "$root" rev-parse --git-dir >/dev/null 2>&1; then
    finding UNKNOWN "Git" "not a git repository" ""
    return
  fi

  # Mid-operation is the state that makes every other answer provisional: the
  # working tree is not what any branch says it is.
  gd=$(git -C "$root" rev-parse --git-dir 2>/dev/null)
  if [ -d "$gd/rebase-merge" ] || [ -d "$gd/rebase-apply" ]; then
    finding NO-GO "Git state" "mid-rebase; the working tree is not any branch" \
      "git rebase --continue   # or: git rebase --abort"
  elif [ -f "$gd/MERGE_HEAD" ]; then
    finding NO-GO "Git state" "mid-merge; the working tree is not any branch" \
      "git merge --continue   # or: git merge --abort"
  else
    branch=$(git -C "$root" rev-parse --abbrev-ref HEAD 2>/dev/null) || branch="unknown"
    finding GO "Git state" "on $branch" ""
  fi

  if [ -n "$(git -C "$root" status --porcelain 2>/dev/null)" ]; then
    finding NO-GO "Working tree" "uncommitted changes present" \
      "commit or stash before trusting a build result"
  else
    finding GO "Working tree" "clean" ""
  fi
}

# --- run --------------------------------------------------------------------

probe_clock
probe_disk
probe_inodes
probe_arch
probe_libc
probe_path
probe_ca
probe_git

n_ran=$((n_go + n_nogo + n_unknown))
m_total=$(manifest_total)
not_run=$((m_total - n_ran))
[ "$not_run" -lt 0 ] && not_run=0

# The partiality line. It leads every output, per ARCHITECTURE.md §3: the
# reason this report is incomplete is the most important thing on screen.
if [ "$m_total" -eq 0 ]; then
  partial="runner unavailable — $n_ran Tier-1 checks run; the full manifest was never read"
else
  partial="runner unavailable — $not_run of $m_total checks not run"
fi

# The headline, matching render.go's precedence: blockers outrank unknowns,
# because a found blocker is a decided answer and reporting it as "unknown"
# buries the one thing the reader must act on.
#
# But the GO branch does not exist here. render.go can say GO when nothing
# failed; this script cannot, because "nothing failed" is only true of the
# handful of checks it ran. Its clean state is UNKNOWN, and the exit code
# agrees -- ARCHITECTURE.md §11: 2 is never collapsed into 0.
#
# The first version of this file printed "VERDICT UNKNOWN" while exiting 1,
# so a human and a CI job read the same run differently. Found by running it,
# not by testing it.
if [ "$n_nogo" -gt 0 ]; then
  headline="NO-GO"
  headline_detail="$n_nogo blocker(s) found, and $partial"
else
  headline="UNKNOWN"
  headline_detail="$partial"
fi

# --- output -----------------------------------------------------------------

emit() {
  # $1: "term" or "md". Same content, same order, both destinations -- so a
  # pasted status.md and a screenshot of the terminal never disagree.
  fmt=$1
  if [ "$fmt" = md ]; then
    printf '# Ferret status\n\n'
    printf '**%s** — %s\n\n' "$headline" "$headline_detail"
    printf 'Generated %s by the Tier-1 fallback.\n\n' \
      "$(date -u '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || echo unknown)"
  else
    printf 'VERDICT  %s\n' "$headline"
    printf '         %s\n\n' "$headline_detail"
  fi

  for state in NO-GO UNKNOWN GO; do
    # A count first, so an empty section is never mistaken for a clean one.
    c=$(grep -c "^$state|" "$findings") || c=0
    [ "$c" -gt 0 ] || continue

    case "$state" in
      NO-GO)   title="Blockers" ;;
      UNKNOWN) title="Unknown" ;;
      GO)      title="Passed" ;;
    esac

    if [ "$fmt" = md ]; then
      printf '## %s (%s)\n\n' "$title" "$c"
      [ "$state" = UNKNOWN ] && printf '*Could not verify. This is not the same as verified-good.*\n\n'
    else
      printf '%s (%s)\n' "$title" "$c"
    fi

    # UNKNOWN is never the same glyph as GO -- it survives a pipe, a CI log,
    # and a colourblind reader. render.go holds the same rule.
    while IFS='|' read -r st ti de rem; do
      [ "$st" = "$state" ] || continue
      case "$st" in
        GO)      glyph="ok  " ;;
        NO-GO)   glyph="FAIL" ;;
        UNKNOWN) glyph="????" ;;
      esac
      if [ "$fmt" = md ]; then
        printf -- '- `%s` **%s**' "$glyph" "$ti"
        [ -n "$de" ] && printf -- ' — %s' "$de"
        printf '\n'
        [ -n "$rem" ] && printf -- '  - remedy: `%s`\n' "$rem"
      else
        printf '  %s %-22s %s\n' "$glyph" "$ti" "$de"
        [ -n "$rem" ] && printf '       remedy: %s\n' "$rem"
      fi
    done <"$findings"
    printf '\n'
  done

  if [ "$fmt" = md ]; then
    printf -- '---\n\nThis is the Tier-1 fallback, not the full sweep. It cannot\n'
    printf 'evaluate the manifest, propagate taint, or derive a stack. Install a Go\n'
    printf 'toolchain (`mise install`; see mise.toml) and re-run for the full report.\n'
  else
    printf 'This is the Tier-1 fallback. Install a Go toolchain for the full sweep:\n'
    printf '  mise install     # see mise.toml; never installed automatically\n'
  fi
}

emit term

if [ -n "$status_path" ]; then
  if mkdir -p "$(dirname -- "$status_path")" 2>/dev/null; then
    emit md >"$status_path" 2>/dev/null \
      || echo "bootstrap: could not write $status_path" >&2
  else
    echo "bootstrap: could not create $(dirname -- "$status_path")" >&2
  fi
fi

# UNKNOWN, always. A blocker found here still exits 1 -- it is a real NO-GO --
# but the absence of blockers is NEVER exit 0, because most of the manifest was
# never run and this script cannot claim what it did not check.
if [ "$n_nogo" -gt 0 ]; then
  exit "$EXIT_NOGO"
fi
exit "$EXIT_UNKNOWN"
