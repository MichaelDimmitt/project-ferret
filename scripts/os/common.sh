#!/bin/sh
# scripts/os/common.sh — the portable core of stage zero.
#
# Sourced, never run directly. Holds what is genuinely POSIX: row emission,
# PATH walking, dedupe, and the probes that behave the same everywhere.
# Anything that differs by OS belongs in scripts/os/<uname>.sh instead.
#
# Contract: docs/design/BOOTSTRAP_PIPELINE.md §2.
#   - POSIX sh only. No bashisms, no jq, no GNU-only flags.
#   - No Python. The interpreter is one of the things being measured.
#   - Installs nothing. Measures only.
#   - A failed probe is a finding, never a fatal error.
#   - Every fact carries the command that produced it.

# --- helpers ---------------------------------------------------------------

# Redaction lives in scripts/lib/redact.sh — standalone, so any future script
# can source it without dragging in the probe machinery. It provides rd_level()
# and redact(). cell() below applies it to every value, which is what makes the
# guarantee hold: a new probe cannot forget to redact.
_rd_lib="${FERRET_LIB_DIR:-$(dirname -- "${OS_DIR:-.}")/lib}/redact.sh"
if [ -r "$_rd_lib" ]; then
  . "$_rd_lib"
else
  echo "common.sh: missing $_rd_lib" >&2
  exit 1
fi


# row_posture NAME VALUE DEFAULT PATH COMMAND
#
# For rows that are security posture or hardware fingerprint. Emitted normally
# at levels 0 and 1; replaced with <redacted> at level 2.
#
# The row still appears, because a silently missing row is indistinguishable
# from a probe that failed — and this tool's whole premise is that absence and
# unknown are different results.
row_posture() {
  if [ "$(rd_level)" = "2" ]; then
    row "$1" "<redacted>" "$3" "-" "$5"
  else
    row "$1" "$2" "$3" "$4" "$5"
  fi
}

# A probe's output is untrusted text going into a table cell. `launchctl
# --version` prints a usage line containing a pipe, which silently splits the
# row into extra columns and corrupts every reader downstream. Escape the
# delimiter and flatten newlines; do it here so no caller has to remember.
cell() {
  redact "$1" | tr '\n\r\t' '   ' | sed -e 's/|/\\|/g' -e 's/  */ /g' \
    -e 's/^ *//' -e 's/ *$//'
}

# row NAME VALUE DEFAULT PATH COMMAND
row() {
  printf '| %s | %s | %s | %s | `%s` |\n' \
    "$(cell "$1")" "$(cell "$2")" "$(cell "$3")" "$(cell "$4")" "$(cell "$5")" >>"$OUT"
}

# Absence and unknown are different results. Never collapse them.
#   present   — probed, found
#   absent    — probed, not there
#   unknown   — could not probe (no permission, timed out, no way to ask)

first_line() { sed -n '1p' 2>/dev/null; }

# Version, or an honest statement that we couldn't get one.
#
# Two traps here, both of which produce a confident wrong answer:
#   - A tool that exits nonzero on --version (`port`) still prints to stdout.
#     Recording that error text as a version is a false positive, and a false
#     positive that gets written into a document outlives the run.
#   - A tool that prints usage instead of a version (`launchctl`, `xcodebuild`)
#     succeeds while telling us nothing.
# Both are "present, version unknown" — which is not the same as a version,
# and not the same as absent.
version_of() {
  vo_bin=$1
  # Capture the binary's own status, not the pipeline's — `cmd | sed` reports
  # sed's exit code, which is 0 even when the tool failed. No PIPESTATUS in
  # POSIX sh, so run it and slice afterward.
  if vo_raw=$("$vo_bin" --version 2>/dev/null); then
    vo_out=$(printf '%s' "$vo_raw" | first_line)
  else
    vo_out=""
  fi

  if [ -z "$vo_out" ]; then
    printf 'present (version unknown)'
    return
  fi

  # Usage/error text is not a version, however cleanly it exited.
  case "$vo_out" in
    Usage:*|usage:*|Error:*|error:*)
      printf 'present (version unknown)' ;;
    *)
      printf '%s' "$vo_out" ;;
  esac
}

# `readlink -f` is GNU/macOS-14+; older macOS lacks it. Resolve conservatively
# and fall back to the literal path rather than reporting a wrong target.
resolve() {
  readlink -f "$1" 2>/dev/null || printf '%s' "$1"
}

common_header() {
  : >"$OUT"
  {
    echo "# default-results.md"
    echo
    echo "Generated: $(date -u '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || echo unknown)"
    echo "Generator: default-script.sh (${OS_SCRIPT:-common only})"
    echo
    if [ "$(rd_level)" = "2" ]; then
      echo "Mode: **redacted (paranoid)** — identity removed, plus machine"
      echo "fingerprint and security posture: exact OS build, CPU, SIP/sudo"
      echo "state, kernel revision, locale, and absolute clocks. Clock *skew*"
      echo "is kept, because it is a real finding. Tool versions are kept,"
      echo "because they are the reason to share the file at all."
      echo
      echo "Safe for a public, indexed destination."
    elif [ "$(rd_level)" = "1" ]; then
      echo "Mode: **redacted (identity)** — username, home paths, TMPDIR, repo"
      echo "path, PATH contents, and uid removed. Machine details are intact,"
      echo "so a reader can still act on it."
      echo
      echo "> Fine for an issue tracker, a colleague, or an AI. It still names"
      echo "> your exact OS build, CPU, and whether SIP and sudo are on — for"
      echo "> somewhere public, use \`--redact=paranoid\`."
    else
      echo "Mode: full (unredacted)"
      echo
      echo "> **Do not paste this publicly.** It contains your username, home"
      echo "> directory paths, exact OS build, hardware, and your full PATH —"
      echo "> which inventories the tooling installed on this machine. Together"
      echo "> that identifies you and tells a reader which exploits would land."
      echo ">"
      echo "> To get a shareable copy: \`./bootstrap/run.sh --redact\`"
    fi
    echo
    echo "Regenerate rather than edit. See docs/design/BOOTSTRAP_PIPELINE.md §3."
    echo
    echo "| name | value | default | path | command to reproduce |"
    echo "|---|---|---|---|---|"
  } >>"$OUT"
}

common_footer() {
  printf '\nProbes: %s\n' "$(grep -c '^| ' "$OUT")" >>"$OUT"
}

# --- shells ----------------------------------------------------------------
# Report EVERY shell found, not the first. Two bash installs on one machine is
# the case that matters and the case `bash --version` alone gets wrong.
# SHELL_DIRS is set by the OS script; the search prefixes are not portable.

probe_shells() {
  default_shell="${SHELL:-unknown}"
  row "default-shell" "$default_shell" "-" "-" 'printf %s "$SHELL"'

  {
    [ -r /etc/shells ] && grep -v '^#' /etc/shells 2>/dev/null
    for d in ${SHELL_DIRS:-/bin /usr/bin /usr/local/bin}; do
      for s in sh bash dash zsh ksh mksh ash busybox; do
        printf '%s/%s\n' "$d" "$s"
      done
    done
  } | {
    seen=""
    while IFS= read -r sh_path; do
      [ -x "$sh_path" ] || continue

      # Dedupe on resolved target so symlinks don't double-count.
      real=$(resolve "$sh_path")
      case " $seen " in *" $real "*) continue ;; esac
      seen="$seen $real"

      name=$(basename "$sh_path")
      ver=$(version_of "$sh_path")

      if [ "$sh_path" = "$default_shell" ]; then is_default=yes; else is_default=no; fi
      row "$name" "$ver" "$is_default" "$sh_path" "$sh_path --version"
    done
  }
}

# --- tools -----------------------------------------------------------------
# Same rule: every install on PATH, not just the winner.

probe_tool() {
  tool=$1
  found=0
  tool_seen=""
  # `command -v` finds the active one; walk PATH for the rest.
  IFS=:
  for d in $PATH; do
    [ -n "$d" ] || d=.
    p="$d/$tool"
    [ -x "$p" ] || continue

    # A duplicated PATH entry is not a second install. Dedupe on the resolved
    # target so /opt/homebrew/bin/git listed twice reports once.
    treal=$(resolve "$p")
    case " $tool_seen " in *" $treal "*) continue ;; esac
    tool_seen="$tool_seen $treal"

    found=1
    ver=$(version_of "$p")
    active=$(command -v "$tool" 2>/dev/null)
    if [ "$p" = "$active" ]; then is_default=yes; else is_default=no; fi
    row "$tool" "$ver" "$is_default" "$p" "$p --version"
  done
  unset IFS
  [ "$found" -eq 1 ] || row "$tool" "absent" "-" "-" "command -v $tool"
}

# Tools every OS cares about. OS scripts add their own (brew, apt, dnf...).
COMMON_TOOLS="git python3 node npm pnpm yarn bun deno go rustc cargo
              docker podman make cc curl wget mise asdf nvm"

probe_common_tools() {
  for t in $COMMON_TOOLS; do
    probe_tool "$t"
  done
}

# --- capabilities ----------------------------------------------------------
# Not "is X installed" — the things an AI must know before it can plan.
# These sit in the same table as installs because to a planner they are
# exactly as load-bearing as a version number.

probe_network() {
  # DNS and internet and registry are THREE questions. A machine can fail any
  # one alone, and the distinction changes which fallback is viable.
  # `getent` is glibc; macOS has no such thing. OS scripts set DNS_PROBE.
  dns_cmd="${DNS_PROBE:-}"
  if [ -n "$dns_cmd" ] && sh -c "$dns_cmd" >/dev/null 2>&1; then
    row "dns" "resolving" "-" "-" "$dns_cmd"
  elif [ -n "$dns_cmd" ]; then
    row "dns" "failing" "-" "-" "$dns_cmd"
  else
    row "dns" "unknown" "-" "-" "no dns probe for this os"
  fi

  if command -v curl >/dev/null 2>&1; then
    if curl -sI --max-time 5 https://example.com >/dev/null 2>&1; then
      net=reachable
    else
      net=unreachable
    fi
    row "internet" "$net" "-" "-" "curl -sI --max-time 5 https://example.com"

    if curl -sI --max-time 5 https://registry.npmjs.org >/dev/null 2>&1; then
      reg=reachable
    else
      reg=unreachable
    fi
    row "npm-registry" "$reg" "-" "-" "curl -sI --max-time 5 https://registry.npmjs.org"
  else
    row "internet" "unknown" "-" "-" "curl -sI https://example.com"
    row "npm-registry" "unknown" "-" "-" "curl -sI https://registry.npmjs.org"
  fi
}

probe_writable() {
  # Writability of the places a fix would actually touch. WRITE_DIRS is
  # OS-specific — /usr/local matters on macOS, /opt on some Linux layouts.
  for d in ${WRITE_DIRS:-"$HOME" . "${TMPDIR:-/tmp}"}; do
    if [ -w "$d" ]; then w=writable; else w=read-only; fi
    row "writable:$d" "$w" "-" "$d" "test -w $d"
  done
}

probe_sudo() {
  # sudo: present, passwordless, and usable are three different states.
  if command -v sudo >/dev/null 2>&1; then
    if sudo -n true 2>/dev/null; then s=passwordless; else s="present (needs password)"; fi
  else
    s=absent
  fi
  # Whether sudo works is posture: it tells a reader what a foothold could
  # escalate to. Useful to a colleague, useful to an attacker.
  row_posture "sudo" "$s" "-" "-" "sudo -n true"
}

probe_clock() {
  # Clock skew taints everything below it (README: Taint), so the *comparison*
  # must survive every redaction level — it is a real finding. The absolute
  # timestamps are what leak (they place you in a timezone and narrow when the
  # machine was up), so at level 2 we keep the verdict and drop the clocks.
  clock_now=$(date -u '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || echo unknown)

  remote=""
  if command -v curl >/dev/null 2>&1; then
    remote=$(curl -sI --max-time 5 https://example.com 2>/dev/null \
             | grep -i '^date:' | cut -d' ' -f2- | tr -d '\r')
  fi

  if [ "$(rd_level)" = "2" ]; then
    # Skew in seconds, when both ends are known and `date` can parse the
    # remote form. Neither GNU -d nor BSD -j -f is portable, so try both and
    # fall back to "present, not compared" rather than guessing.
    skew=""
    if [ -n "$remote" ]; then
      r_epoch=$(date -u -d "$remote" '+%s' 2>/dev/null \
                || date -u -j -f '%a, %d %b %Y %H:%M:%S %Z' "$remote" '+%s' 2>/dev/null) || r_epoch=""
      l_epoch=$(date -u '+%s' 2>/dev/null) || l_epoch=""
      if [ -n "$r_epoch" ] && [ -n "$l_epoch" ]; then
        skew=$((l_epoch - r_epoch))
        [ "$skew" -lt 0 ] && skew=$((-skew))
      fi
    fi

    if [ -n "$skew" ]; then
      if [ "$skew" -le 5 ]; then
        row "clock-skew" "under 5s" "-" "-" "date -u; curl -sI https://example.com"
      else
        row "clock-skew" "${skew}s" "-" "-" "date -u; curl -sI https://example.com"
      fi
    elif [ -n "$remote" ]; then
      row "clock-skew" "unknown (could not parse remote date)" "-" "-" \
          "date -u; curl -sI https://example.com"
    else
      row "clock-skew" "unknown (no remote reference)" "-" "-" \
          "date -u; curl -sI https://example.com"
    fi
    return
  fi

  row "clock-local" "$clock_now" "-" "-" "date -u"
  if [ -n "$remote" ]; then
    row "clock-remote" "$remote" "-" "-" \
        "curl -sI https://example.com | grep -i ^date:"
  else
    row "clock-remote" "unknown" "-" "-" "curl -sI https://example.com"
  fi
}

probe_environment() {
  row "os"     "$(uname -s 2>/dev/null || echo unknown)"     "-" "-" "uname -s"
  # Kernel revision pins the exact patch level; arch does not, and arch decides
  # whether a prebuilt binary runs at all. Different value, different risk.
  row_posture "kernel" "$(uname -r 2>/dev/null || echo unknown)" "-" "-" "uname -r"
  row "arch"   "$(uname -m 2>/dev/null || echo unknown)"     "-" "-" "uname -m"
  # Locale is weak on its own but sharpens a fingerprint in combination.
  row_posture "locale" "${LANG:-unset}"                      "-" "-" 'printf %s "$LANG"'

  # PATH is the single most identifying row in the file — it inventories every
  # language manager, editor plugin, and vendor SDK you have installed, most of
  # which no other probe mentions. Redacted, it collapses to the shape that
  # actually matters for diagnosis: how many entries, and how many are dupes.
  # Shadowing is reported per-tool by probe_tool anyway, which is where a PATH
  # problem is actionable.
  if [ "$(rd_level)" != "0" ]; then
    path_n=$(printf '%s' "$PATH" | tr ':' '\n' | grep -c .)
    path_u=$(printf '%s' "$PATH" | tr ':' '\n' | grep . | sort -u | wc -l | tr -d ' ')
    row "path" "$path_n entries ($path_u unique)" "-" "-" 'printf %s "$PATH"'
  else
    row "path" "$(printf '%s' "$PATH" | tr ':' ' ')" "-" "-" 'printf %s "$PATH"'
  fi

  # Identity is the point of redaction. What a reader still needs is whether
  # we are root, because that changes what remediation may attempt — so report
  # the privilege, not the person.
  uid_n=$(id -u 2>/dev/null || echo unknown)
  if [ "$(rd_level)" != "0" ]; then
    row "user" "<redacted>" "-" "-" "id -un"
    case "$uid_n" in
      0) row "uid" "0 (root)" "-" "-" "id -u" ;;
      *) row "uid" "non-root" "-" "-" "id -u" ;;
    esac
  else
    row "user" "$(id -un 2>/dev/null || echo unknown)" "-" "-" "id -un"
    row "uid"  "$uid_n"                                "-" "-" "id -u"
  fi
}

# The portable run, in order. OS scripts call this, then add their own probes.
probe_common() {
  probe_shells
  probe_common_tools
  probe_network
  probe_writable
  probe_sudo
  probe_clock
  probe_environment
}
