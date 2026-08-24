# Architecture

## 1. Shape

Three stages, strictly ordered. The ordering is the design.

```
  manifest/*.json          declarative checks — data, not code
         │
         ▼
  ferret/runner.py         executes probes, NO interpretation
         │
         ▼
  evidence.json            raw results + metadata, one record per check
         │
         ▼
  ferret/verdict.py        applies expectations, propagates taint
         │
         ▼
  ferret/render.py         severity-ordered glance
         │
         ▼
  status.md  +  stdout
```

**Why capture and interpretation are separate processes, not separate functions:** it makes the "no interpreting while collecting" rule structural rather than aspirational. The runner physically cannot skip a check because an earlier one failed — it doesn't know what failure means. That property is the whole reason the tool works, so it gets enforced by the architecture rather than by discipline.

Consequence: `evidence.json` is re-renderable. Change a pass condition, re-run `verdict.py` against yesterday's evidence, no re-probing.

---

## 2. Document set

### Committed, authored by humans (and Claude)

| File | Role | Audience | Churn |
|---|---|---|---|
| `README.md` | What and why, in 60 seconds | Anyone | Low |
| `docs/FRAMEWORK.md` | The evaluation theory — layers, ambient/declared, capture tiers, why first passes fail | Humans reasoning about coverage | Low |
| `docs/ARCHITECTURE.md` | This file. How it's built and why | Contributors | Medium |
| `docs/PLAN.md` | Milestones in dependency order | Whoever is building | High until done |
| `docs/PROMPT.md` | Standing instruction to Claude Code | Claude Code | Low |
| `docs/MANIFEST_SCHEMA.md` | Field-by-field spec for a check | Anyone adding checks | Low |
| `docs/DOCS_MODEL.md` | The three runtime documents, volatility classes, provenance, the baseline rule | Contributors | Low |
| `AGENTS.md` | Agent-facing repo conventions; `CLAUDE.md` is a thin pointer to it | Any coding agent | Low |

**`FRAMEWORK.md` is rationale; `manifest/` is contract.** The framework explains *why* clock skew matters. The manifest states *how* to probe it and *what* counts as passing. Prose does not drive execution — if a check isn't in the manifest, it doesn't run, no matter how well the framework argues for it.

### Committed, generated once then maintained

| Path | Role |
|---|---|
| `manifest/00-machine.json` … `manifest/10-process.json` | Checks, one file per layer |
| `ferret/*.py` | Runner, verdict engine, renderer, redactor |
| `ferret/bootstrap.sh` | Tier-1-only fallback when Python is unavailable |
| `scripts/sweep.sh` | Thin entry point |

### Generated at runtime

| Path | Role | Committed |
|---|---|---|
| `~/.ferret/system.md` | Durable machine facts, reused across projects | No |
| `<repo>/.ferret/requirements.md` | What this project needs, portable | **Yes** |
| `<repo>/.ferret/status.md` | The glance — this project, this machine, now | No |
| `<repo>/.ferret/evidence.json` | Raw probe results | No |
| `<repo>/.ferret/raw/` | Untruncated probe stdout, for debugging Ferret itself | No |

`.ferret/` is gitignored on creation, in the same commit that creates it — with an explicit un-ignore for `requirements.md`, which is the one artifact meant to travel. See `docs/DOCS_MODEL.md`.

---

## 3. Why the runner is Python, and the honest problem with that

The runner cannot depend on what it measures. That eliminates Node immediately — checking whether Node is correctly installed using Node is circular.

Python 3 (stdlib only, ≥3.8) is the least-bad choice: present on essentially every Linux and macOS, no install step, no third-party packages.

**But it is still a dependency, and pretending otherwise would be exactly the false-green failure this tool exists to prevent.** A Windows machine without Python, or a minimal Alpine container, has none. So:

- `ferret/bootstrap.sh` is POSIX sh and runs the **Tier-1 checks only** — clock, disk, inodes, arch, libc, PATH, CA, git state. These are the checks that invalidate everything else, and they're all cheap shell.
- If Python is absent, bootstrap emits a partial `status.md` whose verdict is **UNKNOWN**, never GO, with `python3 absent — 60 of 68 checks not run` stated at the top.

A partial answer that announces its partiality is the correct behavior. A partial answer that looks complete is the failure mode.

**Manifest format: JSON, not YAML.** YAML would need PyYAML, which is not stdlib. JSON is verbose and has no comments — a real cost when authoring shell one-liners — but the alternative is a dependency in the one tool that must not have dependencies. Long shell commands use string arrays joined with `\n` to stay readable. If this becomes intolerable, the escape is a YAML→JSON build step where YAML is the source and JSON is committed alongside it, so the runtime path stays dependency-free.

---

## 4. The check record

Full field spec in `docs/MANIFEST_SCHEMA.md`. Shape:

```json
{
  "id": "runtime.node.version",
  "layer": 4,
  "title": "Node version matches pin",
  "applies_if": "test -f package.json",
  "probe": "node -v",
  "declared": "cat .nvmrc 2>/dev/null || jq -r '.engines.node // empty' package.json",
  "expect": { "type": "semver_satisfies", "actual": "$probe", "range": "$declared" },
  "severity": "blocker",
  "tainted_by": ["shell.path.node_shadowing", "toolchain.version_manager.conflict"],
  "remedy": "nvm use $declared   # or: mise install",
  "redact": false,
  "timeout_s": 10,
  "decisive": true
}
```

Field notes worth stating up front:

- **`probe` and `declared` are separate fields**, because nearly every check in this tool is a declared-vs-actual comparison. Making that structural means the comparison logic is written once in `verdict.py` rather than re-implemented per check.
- **`expect` is declarative** — `semver_satisfies`, `matches`, `one_of`, `non_empty`, `numeric_lt`, `equals`, `absent`. There is an `exit_code` escape hatch for the genuinely awkward cases, used sparingly. Declarative predicates keep the manifest data; shell everywhere would make it code, and code drifts.
- **`tainted_by` points upward**, listing prerequisite check ids. Edges are declared on the dependent, not the prerequisite, so adding a check never requires editing an unrelated one.
- **`severity`** ∈ `blocker` | `warning` | `info`. Drives ordering in the glance and the top-line verdict.
- **`remedy`** is required for anything that can be NO-GO. A status check that reports breakage without an action is half a tool.
- **`decisive`** separates checks that change the verdict from checks that are context. Context is captured but collapsed in the glance.

---

## 5. Result states

```
GO      probe ran, expect satisfied
NO-GO   probe ran, expect violated
UNKNOWN probe could not produce a trustworthy answer
N/A     applies_if false
```

`UNKNOWN` carries a mandatory `reason`, one of:

| Reason | Meaning | Implied action |
|---|---|---|
| `timeout` | Exceeded `timeout_s` | Retry, check network |
| `tool_absent` | Probe binary not on PATH | Install, or accept the gap |
| `permission` | Insufficient scope or rights | Escalate or ask someone |
| `unverifiable` | Cannot be read by design (secret values) | Cross-reference instead |
| `expired` | Data no longer retained | Re-run whatever produced it |
| `tainted` | A prerequisite is NO-GO | Fix the prerequisite first |
| `probe_error` | Probe itself failed unexpectedly | Bug in Ferret |

These map to the framework's T3a–d causes. The taxonomy stays as reason strings rather than becoming a schema — for a single-machine status check the four causes collapse into one output state plus an explanation, and formalizing further buys nothing.

**Invariant, asserted in tests:** no code path converts UNKNOWN into GO.

---

## 6. Taint

`verdict.py` builds a DAG from `tainted_by`, topologically sorts it, and walks it. Any check whose prerequisite resolved NO-GO becomes `UNKNOWN(tainted)` — regardless of what its own probe returned.

The probe **still ran and its raw output is still in `evidence.json`.** Taint is an interpretation-layer judgment about trustworthiness, not a reason to skip collection. This is what lets you re-run `verdict.py` after fixing the clock without re-probing anything.

The glance renders taint as a rollup on the *cause*, not as forty separate lines:

```
✗ Clock skew +11m42s          → taints 38 checks below
```

Cycles are a manifest authoring error. `verdict.py` detects them and fails loudly rather than resolving them arbitrarily.

---

## 7. Redaction

Redaction is **allowlist**, at capture time, in the runner — never in the renderer.

Blocklist redaction fails the moment someone invents a new secret-shaped env var, and it fails silently. So:

- For high-risk probes (`env`, `.npmrc`, `git remote -v`, any `*_TOKEN`), only explicitly allowlisted keys have values captured. Everything else records **presence and shape only**: `GH_TOKEN: <set, 40 chars, ghp_…>`.
- Values matching known credential patterns are replaced with a stable salted hash, so you can tell "same token as before" without exposing it.
- A test asserts that no known-secret fixture survives into `evidence.json` or `status.md`.
- `.ferret/` is gitignored from the commit that creates it.

The threat model is mundane and likely: someone pastes `status.md` into a ticket, or commits `.ferret/` by reflex.

---

## 8. Read-only guarantee

The sweep must not change what it measures. Enforced three ways:

1. **Manifest review** — every `probe` is inspected for mutation at authoring time.
2. **Denylist** — the runner refuses to execute a probe matching `install|fetch|pull|clone|write|set|checkout|prune|gc|>|>>`, unless the check carries an explicit `mutating: false` override with a justification comment. The override exists because `git config --get` is read-only despite containing "config," and a denylist without an escape hatch produces workarounds worse than the rule.
3. **Test** — the suite runs the full sweep against a fixture repo and asserts the working tree, index, and config hashes are unchanged.

Fresh-shell isolation: probes that read shell state run under `env -i` with a minimal PATH for at least one pass, so Ferret measures the environment CI will see rather than the user's `.zshrc`.

---

## 9. Layer 7 (forge) is different, and the manifest records why

The forge is the only layer whose evidence completeness depends on **who you are**. Two people running the identical sweep get different results, and neither is wrong.

So forge check records carry an extra stamp: `identity`, `scopes`, `host`, `captured_at`. And the glance says so explicitly rather than implying completeness:

```
FORGE  (as alice@github.com, scopes: repo,read:org — 4 checks above your ceiling)
```

Anything an admin could see but you cannot is `UNKNOWN(permission)`, never absent. The distinction between "no branch protection" and "cannot read branch protection" is the difference between a finding and a blind spot.

---

## 10. Golden path

Preflight explains failure. The golden path *is* the verdict.

`ferret --golden` runs clone → install → build → test → run in a temp directory from a fresh non-login shell. It is opt-in and separate because it is the only part of Ferret that is slow and the only part that writes anything.

If any blocker is outstanding, it refuses to run and says `NOT ATTEMPTED (blocked)`. Watching a build fail for a reason you already know is the exact waste this tool exists to prevent.

---

## 11. Exit codes

```
0   GO — no blockers, no unknowns among decisive checks
1   NO-GO — one or more blockers
2   GO-WITH-UNKNOWNS — no blockers, but decisive checks unresolved
3   Ferret itself failed (bad manifest, cycle, internal error)
```

**2 is deliberately not 0.** In CI, "we couldn't verify" must not pass silently — that is the false green, moved into automation where nobody will look at it. A team that finds 2 annoying can opt into `--treat-unknown-as-pass`, which forces the decision to be explicit and reviewable in a config file rather than accidental.

---

## 12. Remediation — phases, not prohibition

Ferret converges a machine toward operational, and documents as it goes. That requires mutation. The rule is not *never mutate* — it is **never mutate before a clean baseline is written.**

```
observe → write baseline → remediate → re-observe → document → (next time) verify
```

Enforced as follows:

- **Phase 1 (observe) is read-only, no exceptions.** The mutation denylist and the byte-identical fixture test from §8 apply here in full. This phase must work with nothing but `sh` and coreutils.
- **The baseline is written before any mutation.** If it can't be written, Ferret exits 3 rather than proceed. This preserves the ability to answer *"was this broken before I got here?"* — which is the entire value of a diagnostic tool.
- **Phase 3 (remediate) is opt-in** (`--remediate`), logged, and every change tagged `ferret-installed` with method and timestamp.
- **Failed remediation is never fatal.** No sudo, no egress, TLS interception, immutable FS, broken package manager — each is itself a high-priority finding. Ferret records the failure, notes which checks now run in reduced mode, and emits the diagnosis it already collected. The machines where remediation fails are the machines that need the report most.
- **Provenance is mandatory.** Untagged, Ferret reports its own footprint as a finding — the false positive that gets a tool ignored. See `docs/DOCS_MODEL.md` §4.

`--footprint` lists what Ferret installed; `--revert` undoes it.

**Still out of scope:**

- **Cross-machine diff.** `evidence.json` is structured so this stays possible, but the task is understanding one machine.
- **Watching / daemon mode.** It's a convergence loop you invoke, not monitoring.
- **A generalized capture-tier taxonomy** beyond Layer 7. It earned its place there in prose and does not need to be a schema.
