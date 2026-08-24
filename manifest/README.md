# manifest/

Check definitions. Probes are POSIX sh strings; the runner never interprets them.

The field-by-field contract is `docs/design/MANIFEST_SCHEMA.md`. `_schema.json`
is the mechanical validation of it, and runs before any probe executes.

## Two rules learned by getting them wrong

**`tainted_by` means "this answer cannot be trusted", not "something related
also failed."** The test is whether the prerequisite's failure makes the
dependent's *measurement* wrong. Clock skew taints registry reachability,
because skew makes TLS fail and the registry looks unreachable when it is
fine. Duplicate PATH entries do *not* taint a count of distinct node installs,
because the count is already de-duplicated and stays correct either way.

Over-tainting is not a safe default. It converts real findings into UNKNOWN
and buries them under "untrustworthy", which is how the two most useful facts
about a machine — three node installs, and node missing from a clean shell —
ended up rendered as noise beneath a GO verdict. That was caught by reading
the glance, not by any test.

**Probes are POSIX sh, and `sh` is not bash.** `command -v -a` is a bashism;
macOS `/bin/sh` rejects it, the error goes to stderr, and a `| wc -l` then
counts zero. The check reported "no node installs" on a machine with three.
Walk `$PATH` explicitly instead. Anything clever deserves a run against
`/bin/sh` before it is committed.
