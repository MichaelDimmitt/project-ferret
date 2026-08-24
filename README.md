# Ferret Sniffer

**A giant status check for the machine you're standing on.**

You are in a git repo on a machine you don't fully trust. Maybe it's a fresh clone, maybe it's a container, maybe it's a laptop that worked yesterday. You want one screen that tells you whether you can proceed, and if not, what's broken and what to do about it.

That's the whole product.

```
$ ferret

VERDICT: NO-GO — 2 blockers, 3 unknown, 47 ok

BLOCKERS
  ✗ Clock skew +11m42s              → taints 38 checks below
  ✗ Node 18.19 — .nvmrc pins 20     → nvm use 20

UNKNOWN
  ? Registry npm.corp.internal      → timeout 5s (VPN?)
  ? Branch protection on main       → token lacks repo:admin
  ? Secret DEPLOY_KEY               → unverifiable by design

WARNINGS
  ⚠ 2 lockfiles present (npm + pnpm)
  ⚠ postinstall scripts disabled via ignore-scripts

Golden path: NOT ATTEMPTED (blocked)
```

---

## Why this exists

Environment failures are miserable specifically because they **alias**. One symptom maps to six possible causes across six different layers. A TLS failure could be clock skew, a CA bundle, a proxy, DNS, an expired token, or a registry outage. If you diagnose while collecting evidence, you lock onto the wrong cause immediately.

Ferret collects everything first, without interpreting, then renders a verdict. That ordering is the entire method.

## The four states

The design lives or dies on this distinction:

| State | Meaning |
|---|---|
| **GO** | Probed, passes its condition |
| **NO-GO** | Probed, fails |
| **UNKNOWN** | Could not probe — timeout, missing permission, tool absent, or unverifiable by design |
| **N/A** | Doesn't apply here (no `gh` checks on a GitLab repo) |

**UNKNOWN must never render as green.** A status dashboard is precisely the format that tempts you to collapse "fine" and "didn't check" into the same color. Every silent omission is a false green, and a false green is worse than no tool at all — it converts *"I don't know"* into *"I verified."*

## Taint

If the clock is wrong, forty green checks below it are meaningless. Ferret models dependency between checks, so a foundational failure marks everything downstream as **tainted** rather than letting it report clean.

Without this you get the worst possible dashboard: one red, mostly green, and a human concluding "mostly fine."

## What it will not do

- **Mutate before a baseline exists.** Phase 1 is strictly read-only — no installs, no `git fetch`, no writing config. The baseline is written before anything changes, so you can always answer *"was this broken before I got here?"*
- **Print secrets.** Environment variables, `.npmrc` auth tokens, and remotes with embedded credentials all get captured. Redaction is allowlist-based, not blocklist-based, and output paths are gitignored by default.
- **Guess.** If it can't determine something, it says UNKNOWN and why.
- **Give up when it can't fix things.** No sudo, no egress, immutable filesystem — each is itself a finding. Ferret reports what it has rather than failing.

## The loop

Ferret isn't only a report. It converges a machine toward operational and documents as it goes, so the next project starts from what's already known.

```
observe → write baseline → remediate → re-observe → document → (next time) verify
```

Mutation isn't the problem — **unrecorded** mutation is. Everything Ferret installs is tagged `ferret-installed` with method and timestamp, so its own footprint never gets reported as a finding about your machine. `--footprint` lists it; `--revert` undoes it.

## Output

Three documents, split by **reuse lifecycle** — how long the knowledge stays true.

| Doc | Where | Committed | Scope |
|---|---|---|---|
| `system.md` | `~/.ferret/` | No | This machine, every project. Written once, verified thereafter. |
| `requirements.md` | `<repo>/.ferret/` | **Yes** | What this project needs, on any machine. The dependency manifest the repo never wrote. |
| `status.md` | `<repo>/.ferret/` | No | The glance: this project, this machine, right now. |

Plus `evidence.json` — raw probe results, not for reading, for when a line in `status.md` surprises you.

**Prior documents are inputs, not just outputs.** The tenth project on a known machine re-verifies a handful of volatile facts instead of rediscovering the box, then reports only the delta: *"this machine has everything except Postgres."* Claims carry a volatility class, and a stale claim reads as UNKNOWN, never GO.

See `docs/DOCS_MODEL.md`.

## The verdict

The real question is not "are forty checks green." It is: **can this repo go clone → install → build → test → run, from a fresh non-login shell?**

Everything Ferret checks before that is preflight — cheap checks that *explain* a failure and save you the minutes of watching a build fail for a reason you could have known in thirty seconds. The golden path is the verdict. Preflight is the diagnosis.

## Status

Early. See `docs/PLAN.md` for milestones and `docs/ARCHITECTURE.md` for the design.

## Documents

| File | Role |
|---|---|
| `docs/FRAMEWORK.md` | The evaluation theory — layers, ambient vs declared, why first passes fail |
| `docs/ARCHITECTURE.md` | How Ferret is built and why |
| `docs/DOCS_MODEL.md` | The three documents, volatility, provenance, the baseline rule |
| `docs/PLAN.md` | Milestones, in order |
| `docs/PROMPT.md` | The instruction handed to Claude Code to execute the plan |
| `manifest/*.json` | The checks themselves — the actual contract |

## Name

A ferret goes into the burrow and finds out what's actually down there. The framework calls the target "ambient state" — everything influencing your commands that nobody wrote down. Same idea, shorter word.
