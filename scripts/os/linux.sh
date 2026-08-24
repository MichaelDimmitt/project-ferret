#!/bin/sh
# scripts/os/linux.sh — Linux probes.
#
# Sourced by the dispatcher after scripts/os/common.sh. Everything here is
# something macOS either lacks or answers differently.
#
# Contract: docs/design/BOOTSTRAP_PIPELINE.md §2.
#
# "Linux" is not one platform. Alpine is musl with busybox coreutils; Debian
# is glibc with GNU. Probes here must survive both, which mostly means not
# assuming a tool exists because it does on Ubuntu.

SHELL_DIRS="/bin /usr/bin /usr/local/bin /sbin /usr/sbin"
WRITE_DIRS="$HOME . /usr/local /opt ${TMPDIR:-/tmp}"

DNS_PROBE="getent hosts example.com"

LINUX_TOOLS="apt apt-get dnf yum pacman apk zypper systemctl ld gcc g++ pkg-config"

probe_os() {
  probe_common

  for t in $LINUX_TOOLS; do
    probe_tool "$t"
  done

  # Distro identity. /etc/os-release is the standard and is present on Alpine,
  # Debian, RHEL, and Arch alike — one of the few things they all agree on.
  if [ -r /etc/os-release ]; then
    distro=$(. /etc/os-release 2>/dev/null && printf '%s' "${PRETTY_NAME:-${NAME:-unknown}}")
    [ -n "$distro" ] || distro=unknown
    # Which distro decides the package manager, so the family must survive
    # every level. The exact point release ("22.04.3") is what pins a patch
    # level, so at level 2 keep only the name and major version.
    if [ "$(rd_level)" = "2" ] && [ "$distro" != "unknown" ]; then
      distro_id=$(. /etc/os-release 2>/dev/null && printf '%s' "${ID:-linux}")
      distro_maj=$(. /etc/os-release 2>/dev/null && printf '%s' "${VERSION_ID:-}" | cut -d. -f1)
      if [ -n "$distro_maj" ]; then
        distro="$distro_id $distro_maj.x"
      else
        distro="$distro_id"
      fi
    fi
    row "distro" "$distro" "-" "/etc/os-release" ". /etc/os-release; echo \$PRETTY_NAME"
  else
    row "distro" "unknown" "-" "-" "cat /etc/os-release"
  fi

  # libc flavor decides whether a prebuilt binary will run at all. A musl box
  # given a glibc-linked node fails at exec with an error that names neither.
  if command -v ldd >/dev/null 2>&1; then
    if ldd --version 2>&1 | grep -qi musl; then
      libc=musl
    elif ldd --version 2>&1 | grep -qi 'gnu\|glibc'; then
      libc=glibc
    else
      libc=unknown
    fi
    row "libc" "$libc" "-" "-" "ldd --version"
  elif [ -e /lib/ld-musl-* ] 2>/dev/null; then
    row "libc" "musl" "-" "-" "ls /lib/ld-musl-*"
  else
    row "libc" "unknown" "-" "-" "ldd --version"
  fi

  # Container detection — changes what remediation is even sensible.
  if [ -f /.dockerenv ]; then
    c=docker
  elif [ -r /proc/1/cgroup ] && grep -qE 'docker|containerd|kubepods' /proc/1/cgroup 2>/dev/null; then
    c=container
  elif [ -r /run/.containerenv ]; then
    c=podman
  else
    c=no
  fi
  row "container" "$c" "-" "-" "test -f /.dockerenv"

  # WSL changes path semantics, line endings, and what "the filesystem" means.
  if grep -qi microsoft /proc/version 2>/dev/null; then w=yes; else w=no; fi
  row "wsl" "$w" "-" "-" "grep -i microsoft /proc/version"

  # init system decides how a service gets started, if one needs to be.
  if [ -d /run/systemd/system ]; then
    init=systemd
  elif [ -x /sbin/openrc ] || [ -d /run/openrc ]; then
    init=openrc
  else
    init=unknown
  fi
  # Init system is posture: it names the service-management attack surface.
  row_posture "init" "$init" "-" "-" "test -d /run/systemd/system"

  # Read-only root defeats every install path regardless of sudo.
  if [ -r /proc/mounts ]; then
    if grep -E '^\S+ / \S+ ro[,ing ]' /proc/mounts >/dev/null 2>&1; then
      rootfs=read-only
    else
      rootfs=writable
    fi
    row "rootfs" "$rootfs" "-" "/" "grep ' / ' /proc/mounts"
  fi

  # Linux is case-sensitive nearly always — but overlayfs on a case-insensitive
  # host, and network mounts, are the exceptions that produce baffling bugs.
  probe_dir="${TMPDIR:-/tmp}"
  cs_probe="$probe_dir/.ferret-case-$$"
  if : >"${cs_probe}a" 2>/dev/null; then
    if [ -e "${cs_probe}A" ]; then fs_case=insensitive; else fs_case=sensitive; fi
    rm -f "${cs_probe}a" 2>/dev/null
  else
    fs_case=unknown
  fi
  row "fs-case" "$fs_case" "-" "$probe_dir" "touch f; test -e F"
}
