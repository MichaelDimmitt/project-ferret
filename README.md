# Ferret Sniffer

**Hand an AI a repo on a machine you don't trust, and have it fix the environment — with its plan on the table first.**

You are in a git repo on a machine you don't fully trust. Maybe it's a fresh clone, maybe it's a container, maybe it's a laptop that worked yesterday. You want the AI to fix it. But before it touches anything you want to see the approach, and you want to be certain it is working from what's actually on the box rather than from what it assumes.

That's the whole product.

```
$ ferret

STACK — derived from this repo
  ✓ node 20.11          .nvmrc pins 20              present
  ✓ git 2.43            required                    present
  ✗ pnpm                pnpm-lock.yaml              MISSING
  ✗ postgres 16         docker-compose.yml          MISSING
  ? python 3.11         scripts/*.py                UNKNOWN — no egress to check

BLOCKERS
  ✗ Clock skew +11m42s                → taints 38 checks below
  ✗ Node 18.19 — .nvmrc pins 20       → nvm use 20

PROPOSED — 2 to install, review before anything runs

  pnpm 8.15    corepack enable          no network, reversible
               ↓ if unavailable         npm i -g pnpm        needs registry
               ↓ if install forbidden   npm (lockfile drift — flagged, not silent)

  postgres 16  docker compose up        image cached locally
               ↓ if unavailable         brew install         needs network + write
               ↓ if install forbidden   NOT AVAILABLE — 6 checks stay UNKNOWN

  $ ferret --approve        accept as-is
  $ ferret --edit           drop lines, pin versions, force a fallback

Nothing is installed until this list is approved.
```

---

## The shape of it

1. **Derive the stack.** Read the repo — lockfiles, `.nvmrc`, compose files, CI config, scripts — and work out what it actually needs. Not a generic checklist; this repo's requirements.
2. **Observe the machine.** Probe what's present, read-only, and write a baseline before anything changes.
3. **Propose a list.** Everything the stack needs that the machine lacks, with the preferred tool and its fallback chain, in one editable list.
4. **Wait.** You approve or edit. Nothing installs before that.
5. **Fix, then re-observe.** Execute the approved list, verify it took, document what changed.

Steps 1–2 are why step 3 can be trusted. An agent that hasn't checked will confidently install the wrong thing.

## Preferred tool, and what happens when it's absent

A stack requirement is not "postgres." It's *postgres, obtained this way, or these other ways, and here's what breaks if none work.* Every requirement declares:

- **The preferred tool** — what it would reach for given a free hand.
- **An ordered fallback chain** — what it tries next, and what each alternative costs. `corepack` needs no network; `npm i -g` needs a reachable registry. That difference decides the order on a machine behind a proxy.
- **What it does when installing is not allowed.** No sudo, no egress, immutable filesystem, a policy that says no. This is the case the tool exists for. Sometimes there's a degraded path — use `npm` against a `pnpm` lockfile and flag the drift loudly. Sometimes there isn't, and the honest answer is *not available, and here are the six checks that stay UNKNOWN because of it.*

The fallback chain is declared up front, in the proposal, before you approve. Discovering the second choice at execution time — after approval, when you're no longer looking — is exactly the surprise this design is built to prevent.

## Why it collects everything before deciding

Environment failures **alias**. One symptom maps to six causes across six layers. A TLS failure could be clock skew, a CA bundle, a proxy, DNS, an expired token, or a registry outage. Diagnose while collecting and you lock onto the wrong cause immediately — and an AI that locks onto the wrong cause proceeds to fix the wrong thing, confidently.

Ferret collects first without interpreting, then decides. That ordering is what makes the proposal worth reading.

## The four states

| State | Meaning |
|---|---|
| **GO** | Probed, passes its condition |
| **NO-GO** | Probed, fails |
| **UNKNOWN** | Could not probe — timeout, missing permission, tool absent, or unverifiable by design |
| **N/A** | Doesn't apply here (no `gh` checks on a GitLab repo) |

**UNKNOWN must never render as green.** Every silent omission is a false green, and a false green converts *"I don't know"* into *"I verified."* For an agent about to change your machine, that is the difference between an informed fix and a guess wearing a checkmark.

## Taint

If the clock is wrong, forty green checks below it are meaningless. Ferret models dependency between checks, so a foundational failure marks everything downstream as **tainted** rather than letting it report clean.

Without this you get the worst possible input to a decision: one red, mostly green, and a conclusion of "mostly fine."

## What it will not do

- **Install anything off its own initiative.** The proposed list is approved or edited by you first. Every installed thing is tagged `ferret-installed` with method and timestamp, so Ferret's own footprint never gets reported back as a finding about your machine. `--footprint` lists it; `--revert` undoes it.
- **Mutate before a baseline exists.** Observation is strictly read-only. The baseline is written before anything changes, so you can always answer *"was this broken before I got here?"*
- **Print secrets.** Environment variables, `.npmrc` auth tokens, and remotes with embedded credentials all get captured. Redaction is allowlist-based, not blocklist-based, and output paths are gitignored by default.
- **Guess.** If it can't determine something, it says UNKNOWN and why.
- **Give up when it can't fix things.** No sudo, no egress, immutable filesystem — each is itself a finding. The machines where remediation fails are the machines that need the report most.

## The loop

```
derive stack → observe → write baseline → propose → approve → remediate → re-observe → document → (next time) verify
```

Mutation isn't the problem — **unapproved and unrecorded** mutation is.

## Output

Three documents, split by **reuse lifecycle** — how long the knowledge stays true.

| Doc | Where | Committed | Scope |
|---|---|---|---|
| `system.md` | `~/.ferret/` | No | This machine, every project. Written once, verified thereafter. |
| `requirements.md` | `<repo>/.ferret/` | **Yes** | What this project needs, on any machine. The dependency manifest the repo never wrote. |
| `status.md` | `<repo>/.ferret/` | No | The glance: this project, this machine, right now. |

Plus `evidence.json` — raw probe results, not for reading, for when a line in `status.md` surprises you.

**Prior documents are inputs, not just outputs.** The tenth project on a known machine re-verifies a handful of volatile facts instead of rediscovering the box, then proposes only the delta: *"this machine has everything except Postgres."* Claims carry a volatility class, and a stale claim reads as UNKNOWN, never GO.

See `docs/design/DOCS_MODEL.md`.

## The verdict

The real question is not "are forty checks green." It is: **can this repo go clone → install → build → test → run, from a fresh non-login shell?**

Everything before that is preflight — cheap checks that *explain* a failure and save you watching a build fail for a reason you could have known in thirty seconds. The golden path is the verdict. Preflight is the diagnosis. The proposal is what you approve in between.

## Running it

```sh
./bootstrap/run.sh      # 1. measure this machine — works now
./scripts/sweep.sh      # 2. check this repo      — stub until M1
```

Stage zero first, always; everything after reads what it measured.
Quick list: `HOW-simple.md`. Full detail: `HOW.md`.

## Status

Early. See `docs/plan/PLAN.md` for milestones and `docs/design/ARCHITECTURE.md` for the design.

> **Known gap:** the preferred-tool / fallback-chain model described above is not yet in the manifest schema. `docs/design/ARCHITECTURE.md` and `manifest/*.json` need fields for preferred tool, ordered alternatives, and install-permission gating.

## Layout

Runnable code and prose are kept apart, and the prose is split by what it's for.

```
bootstrap/     stage zero — measure this machine before anything else runs
ferret/        the Go runner, verdict engine, renderer
manifest/      the checks, as data
scripts/       shell entry points and committed reference scripts
tests/         fixtures and tests
docs/design/   how the system works — architecture, theory, doc model
docs/plan/     how it gets built — milestones and the standing prompt
```

## Documents

| File | Role |
|---|---|
| `HOW-simple.md` | What to run, as a bulleted list |
| `HOW.md` | What to run, when, and in what order |
| `docs/design/FRAMEWORK.md` | The evaluation theory — layers, ambient vs declared, why first passes fail |
| `docs/design/ARCHITECTURE.md` | How Ferret is built and why |
| `docs/design/LANGUAGE_CHOICE.md` | Why the runner is Go, what mise changes, Rust as the alternative |
| `docs/design/BOOTSTRAP_PIPELINE.md` | The five documents: measure the machine with a script, then plan against it |
| `docs/design/DOCS_MODEL.md` | The three documents, volatility, provenance, the baseline rule |
| `docs/plan/PLAN.md` | Milestones, in order |
| `docs/plan/PROMPT.md` | The instruction handed to Claude Code to execute the plan |
| `docs/plan/CONTINUE.md` | Re-pasteable prompt to resume the build mid-project |
| `manifest/*.json` | The checks themselves — the actual contract |

## Name

A ferret goes into the burrow and finds out what's actually down there. The framework calls the target "ambient state" — everything influencing your commands that nobody wrote down. Same idea, shorter word.
