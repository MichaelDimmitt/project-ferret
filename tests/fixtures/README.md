# tests/fixtures/

Committed fixtures. No machine-specific values — no paths, ports, hostnames,
or usernames. Anything here is read by tests on every machine.

| Path | For | Milestone |
|---|---|---|
| `manifest-valid/` | A manifest exercising every `expect` type and every optional field | M1 |
| `manifest-invalid/` | One file per §8 validation error; each must be rejected | M1 |
| `manifest-dup-a/`, `manifest-dup-b/` | Same id in two files — the cross-file duplicate case | M1 |

Still to come: `evidence.json` for the verdict engine (M2), taint DAGs (M3),
and the read-only fixture repo (M5).

## What the manifest fixtures are for

`manifest-valid/04-runtime.json` is the positive case: it must load clean, and
between its twelve checks it covers all nine `expect` types, both `redact`
levels, the `mutating` override, `applies_if`, `declared`, array-form probes,
`decisive: false`, and every `volatility` class. If a field is in the schema and
not in this file, it is untested.

The probes in it are realistic but never run against a real machine in M1 — the
loader is what is under test. They are written as they would really be written
so that the fixture doubles as a worked reference for anyone adding checks.

`manifest-invalid/` is the negative case, and it is the more important half. A
validator that accepts everything passes every positive test. See that
directory's README for the file-to-rule mapping.

## The verdict fixture

`verdict/` is M2's committed evidence: a manifest and an `evidence.json` that
resolve to a stable, deliberately mixed report — one NO-GO, three GO, three
UNKNOWN, one N/A.

Two things in it are load-bearing rather than illustrative:

- **`fx.node.version` and `fx.registry` both have passing probe output.**
  `v20.11.0` satisfies the declared `20`, and the registry exited 0. They
  resolve UNKNOWN anyway, because the clock is skewed 702s and taint says a
  green check below a broken clock is meaningless. A fixture where the tainted
  checks also failed on their own would prove nothing.
- **The chain is three deep** — clock → node → registry — so transitive taint
  is exercised, and both dependents must name the root cause rather than the
  intermediate.

Regenerate it by hand, not by running a sweep: it must stay free of
machine-specific values, and its whole point is being stable across machines.
