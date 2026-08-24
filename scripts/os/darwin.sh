#!/bin/sh
# scripts/os/darwin.sh — macOS probes.
#
# Sourced by the dispatcher after scripts/os/common.sh. Everything here is
# something Linux either lacks or answers differently.
#
# Contract: docs/design/BOOTSTRAP_PIPELINE.md §2.

# Homebrew owns /opt/homebrew on arm64 and /usr/local on x86_64. MacPorts uses
# /opt/local. All three can coexist, which is exactly why we search all three.
SHELL_DIRS="/bin /usr/bin /usr/local/bin /opt/homebrew/bin /opt/local/bin /usr/local/sbin"
WRITE_DIRS="$HOME . /usr/local /opt/homebrew ${TMPDIR:-/tmp}"

# No getent on macOS. dscacheutil is the system resolver; host is the fallback.
if command -v dscacheutil >/dev/null 2>&1; then
  DNS_PROBE="dscacheutil -q host -a name example.com"
else
  DNS_PROBE="host example.com"
fi

DARWIN_TOOLS="brew port xcode-select xcodebuild swift clang codesign launchctl"

probe_os() {
  probe_common

  for t in $DARWIN_TOOLS; do
    probe_tool "$t"
  done

  # Product version, not just the Darwin kernel number. `uname -r` says 25.5.0;
  # nobody writes a requirement against that.
  if command -v sw_vers >/dev/null 2>&1; then
    mac_ver=$(sw_vers -productVersion 2>/dev/null || echo unknown)
    # At level 2, coarsen to the major line: "26.x" still answers "is this too
    # old for the toolchain" without naming the exact patch an exploit targets.
    if [ "$(rd_level)" = "2" ] && [ "$mac_ver" != "unknown" ]; then
      mac_ver="$(printf '%s' "$mac_ver" | cut -d. -f1).x"
    fi
    row "macos-version" "$mac_ver" "-" "-" "sw_vers -productVersion"
    row_posture "macos-build" "$(sw_vers -buildVersion 2>/dev/null || echo unknown)" \
        "-" "-" "sw_vers -buildVersion"
  else
    row "macos-version" "unknown" "-" "-" "sw_vers -productVersion"
  fi

  # Rosetta: an arm64 machine running an x86_64 shell reports x86_64 from
  # `uname -m` and every version probe below it inherits the lie.
  if command -v sysctl >/dev/null 2>&1; then
    translated=$(sysctl -n sysctl.proc_translated 2>/dev/null)
    case "$translated" in
      1) r="yes (running under rosetta)" ;;
      0) r=no ;;
      *) r=unknown ;;
    esac
    # Rosetta stays at every level: it changes what `uname -m` means, so
    # hiding it would make the arch row misleading rather than merely absent.
    row "rosetta" "$r" "-" "-" "sysctl -n sysctl.proc_translated"
    row_posture "cpu" "$(sysctl -n machdep.cpu.brand_string 2>/dev/null || echo unknown)" \
        "-" "-" "sysctl -n machdep.cpu.brand_string"
  fi

  # Command Line Tools. Their absence is the single most common reason a
  # native build fails on a Mac that otherwise looks fully equipped.
  if command -v xcode-select >/dev/null 2>&1; then
    clt=$(xcode-select -p 2>/dev/null) || clt=""
    if [ -z "$clt" ]; then
      clt=absent
    elif [ "$(rd_level)" = "2" ]; then
      # Presence is what makes a native build work or fail. The path also says
      # full Xcode vs. bare CLT and where it lives, which is fingerprint.
      case "$clt" in
        *Xcode.app*) clt="present (full Xcode)" ;;
        *)           clt="present (command line tools)" ;;
      esac
    fi
    row "xcode-clt" "$clt" "-" "-" "xcode-select -p"
  else
    row "xcode-clt" "unknown" "-" "-" "xcode-select -p"
  fi

  # SIP restricts what remediation can touch, whatever sudo says.
  if command -v csrutil >/dev/null 2>&1; then
    sip=$(csrutil status 2>/dev/null | sed -n '1p') || sip=unknown
    [ -n "$sip" ] || sip=unknown
    # Whether SIP is off is exactly what an attacker wants to know first.
    row_posture "sip" "$sip" "-" "-" "csrutil status"
  fi

  # Case-insensitive by default, and that difference breaks imports that work
  # everywhere else. Probe it rather than assume the default holds.
  probe_dir="${TMPDIR:-/tmp}"
  cs_probe="$probe_dir/.ferret-case-$$"
  if : >"${cs_probe}a" 2>/dev/null; then
    if [ -e "${cs_probe}A" ]; then fs_case=insensitive; else fs_case=sensitive; fi
    rm -f "${cs_probe}a" 2>/dev/null
  else
    fs_case=unknown
  fi
  row "fs-case" "$fs_case" "-" "$probe_dir" "touch f; test -e F"

  # macOS containers are Linux VMs; /.dockerenv won't exist on the host.
  if [ -f /.dockerenv ]; then c=docker; else c=no; fi
  row "container" "$c" "-" "-" "test -f /.dockerenv"
}
