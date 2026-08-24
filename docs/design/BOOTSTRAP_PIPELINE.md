# The Bootstrap Pipeline

Five documents that get an AI from *nothing known about this machine* to *an approved plan it may execute*. They run before the Python runner, before the manifest, before anything that could itself be missing.

The ordering is the design, and the constraint that shapes all of it: **step 2 is a shell script, not an agent.** An AI cannot be trusted to report what is on a machine it hasn't measured, and it cannot measure a machine whose tools it hasn't confirmed exist. So the measuring is mechanical, and the AI reads the result.

```
1. initial-defaults-prompt.md   → authors the script          (AI, once)
2. default-script.sh            → measures the machine        (sh, every run, no AI)
3. default-results.md           → the measurement             (generated)
4. ai-preferred-stack.md        → what the AI wants, + fallbacks
5. ai-do-not-use-list.md        → what the user has forbidden
                                        ↓
                          proposed list → you approve or edit → execute
```

Documents 4 and 5 are read *against* document 3. Preference meets reality meets policy; what survives all three is the proposal.

---

## 1. `initial-defaults-prompt.md`

**What it is:** the standing instruction for writing and revising `default-script.sh`. Read by an AI; never executed.

**Why it exists separately:** the script has to run on a machine where almost nothing is guaranteed. That is a narrow and unusual authoring problem, and the rules for it are worth stating once rather than re-deriving each time someone extends the script.

**What it must cover:**

- **Discover the default shell, then the alternatives** — `$SHELL`, the invoking shell, `/etc/shells`, and what is actually executable on disk. Report every one found; do not stop at the first. Two `bash` installs on one machine (`/bin/bash` 3.2, `/opt/homebrew/bin/bash` 5.2) is the case that matters, and it is the case a naive `bash --version` gets wrong.
- **Install nothing.** Not a package, not a shim, not a config file. The script measures; it does not change.
- **Assume nothing beyond POSIX sh and coreutils.** No bashisms, no `jq`, no GNU-only flags. If a richer tool is present, use it *and record that you did* — but there must be a path that works without it.
- **Never fail the run because a probe failed.** A missing tool is a finding, not an error. Absence and unknown are different results and must stay distinguishable.
- **Every fact carries the command that produced it.** This is what makes the output auditable rather than merely believable.

## 2. `default-script.sh`

**What it is:** a POSIX sh script that runs every probe appropriate to the detected OS and writes `default-results.md`.

**No AI runs this.** That is the point of it. It is a script so it can run on a box with no network, no agent, and no Python — and so its output is reproducible by a human who does not trust the agent.

**Where it lives:**

| Path | Committed | Role |
|---|---|---|
| `scripts/default-script.sh.example` | **Yes** | The reference implementation. What reviewers read and adaptation starts from. |
| `bootstrap/run.sh` | **Yes** | Stage-zero entry point. Seeds the script from the example on first run, then executes it. |
| `bootstrap/default-script.sh` | No | The adapted copy for *this* machine. Yours to edit; never overwritten. |
| `bootstrap/default-results.md` | No | The measurement. Describes one machine, never travels. |

Run `./bootstrap/run.sh`, not the script in `scripts/` directly — the entry point owns the seeding and the output path. An agent asked what is installed reads `bootstrap/default-results.md`, or generates it by running that entry point; it does not answer from assumption and does not probe ad hoc. See `AGENTS.md`.

**What it reports:**

- **What is installed** — shells, runtimes, package managers, VCS, container tooling, build tools. With versions, with paths, one line per install found.
- **What capabilities exist** — the things an AI needs to know before planning, which are not "is X installed":
  - Is there internet? Is there DNS? Is a package registry reachable? These are three different questions and a machine can fail any one of them alone.
  - Can we write to the places that matter — `$HOME`, the repo, `/usr/local`, `TMPDIR`?
  - Is there sudo, and does it work without a password prompt?
  - Is the filesystem immutable? Are we in a container? Is the clock right?
- **What the environment looks like** — PATH, locale, encoding, architecture, libc.

This maps onto the existing `ferret/bootstrap.sh` (ARCHITECTURE.md §3): Tier-1-only, POSIX sh, runs when Python is absent. The bootstrap pipeline is that script's contract, written down.

## 3. `default-results.md`

**What it is:** the machine's measured state. Generated, never hand-edited, regenerated rather than patched.

**Required columns:**

| name | value | command to reproduce |
|---|---|---|

`command to reproduce` is not decoration. It is what lets a human check a line that looks wrong, and what lets the next run diff honestly. A fact without provenance is a claim.

**Optional columns** as they earn their place — `default` (yes/no), `path`, `kind`.

**One line per install, never merged.** Two bashes are two rows:

| name | value | default | path | command to reproduce |
|---|---|---|---|---|
| bash | 3.2.57 | yes | `/bin/bash` | `/bin/bash --version` |
| bash | 5.2.21 | no | `/opt/homebrew/bin/bash` | `/opt/homebrew/bin/bash --version` |
| internet | reachable | — | — | `curl -sI https://example.com` |
| dns | resolving | — | — | `getent hosts example.com` |
| sudo | passwordless | — | — | `sudo -n true` |

Capabilities sit in the same table as installs. "Connected to the internet?" is a row like any other, because to an AI planning a fix it is exactly as load-bearing as a version number.

## 4. `ai-preferred-stack.md`

**What it is:** what the AI reaches for given a free hand, what it tries when it can't have that, and where it stops.

**Preference is contextual, not fixed.** If `mise` is already on the machine, the correct plan is different — version management routes through it and half the fallback chain becomes irrelevant. The stack doc is read against `default-results.md`, and what is already installed changes what should be proposed. An AI that proposes `nvm` on a `mise` machine has not read the room.

**Every entry declares an ordered chain:**

1. **Preferred** — the best tool for the job.
2. **Alternatives, in order** — each with its cost. `corepack enable` needs no network; `npm i -g` needs a reachable registry. On a proxied box that difference decides the order, which is why the cost is recorded and not just the command.
3. **Degrade toward POSIX.** The chain always moves toward fewer dependencies and more portability. The last technical option should be something that works with `sh` and coreutils, even if it is worse. A tool that only works when everything is already working is not a fallback.
4. **Ask, as last resort.** When the chain is exhausted, the AI does not improvise and does not silently skip. It presents the recommendations it has and asks you to pick. Running out of options is a legitimate outcome; pretending otherwise is not.

**Approval gate.** The AI derives its preferred stack, checks it against what is present, and everything missing goes into **one editable list**. You edit and approve that list — drop lines, pin versions, force a fallback — and nothing installs before you do.

## 5. `ai-do-not-use-list.md`

**What it is:** user policy. Standing constraints on what the AI may propose at all — "no mise," "no python," "never install globally," "no homebrew."

**Why it is separate from the stack doc:** the stack is the AI's reasoning and changes as tooling changes. This is *your* preference and persists across projects and machines. Merging them means every stack revision risks quietly dropping a constraint you set months ago.

**How it is applied:** as a filter before the proposal is built, not as a veto after. A forbidden tool never appears in the list you review. If a ban empties a fallback chain, that surfaces as an honest dead end — *"no remaining option for postgres under current policy; six checks stay UNKNOWN"* — and routes to the ask-the-user case in §4. It does not get quietly worked around.

A ban is a finding, not an error. The machine that forbids everything still gets a report.

---

## What this does not settle

The manifest schema has no fields yet for preferred tool, ordered alternatives, or install-permission gating. This document specifies the behavior; `docs/design/MANIFEST_SCHEMA.md` and `manifest/*.json` still need the data model to carry it.
