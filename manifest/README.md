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

**A check lives in one layer only.** PLAN.md lists registry reachability under
layer 5, but `network.registry.reachable` already implements it in
`02-network.json`, where it landed with Tier 1. A second copy would report one
outage as two findings — a false positive by duplication — and the layer-2
version is the better one, being additionally tainted by
`network.proxy.case_conflict`. When the plan and the manifest disagree about
where a check belongs, the manifest wins and the plan gets a note.

**Probes are POSIX sh, and `sh` is not bash.** `command -v -a` is a bashism;
macOS `/bin/sh` rejects it, the error goes to stderr, and a `| wc -l` then
counts zero. The check reported "no node installs" on a machine with three.
Walk `$PATH` explicitly instead. Anything clever deserves a run against
`/bin/sh` before it is committed.
