# The Docs Model

Ferret produces documents, and those documents are inputs to the next run. This file specifies what they are, where they live, what may be committed, and when a claim in them stops being trustworthy.

---

## 1. Why documents at all

FRAMEWORK.md's thesis:

> A project is well-configured to exactly the degree that it has migrated dependencies from the ambient column into the declared one.

Ferret is a **declaration engine**. Every run converts unwritten machine state into a versioned artifact. `system.md` is a `.nvmrc` for the whole box — ambient state that someone finally wrote down.

The consequence that matters: **prior docs are inputs, not just outputs.** Run two reads what run one wrote and re-verifies only what could have drifted. That makes the second run cheap, and it turns Ferret into the same declared-vs-actual diff as every other check — with the doc as *declared* and the machine as *actual*.

---

## 2. The three documents

The split is by **reuse lifecycle**, not by subject matter.

| Doc | Location | Committed | Scope | Lifetime |
|---|---|---|---|---|
| `system.md` | `~/.ferret/system.md` | **Never** | This machine, all projects | Months |
| `requirements.md` | `<repo>/.ferret/requirements.md` | **Yes** | This project, all machines | Repo lifetime |
| `status.md` | `<repo>/.ferret/status.md` | **No** | This project on this machine | This session |

### `system.md` — durable, machine-scoped

What doesn't change from project to project: OS, arch, libc, package manager, shell, installed toolchains and their real paths, CA bundle location, proxy configuration, version managers present and which is active, disk layout, git version and global config.

Written once, verified thereafter. This is the amortization — the tenth project on this machine should re-verify a handful of volatile facts, not rediscover the box.

Never committed. It describes a machine, not a project, and it will contain paths, usernames, and org-internal hostnames.

### `requirements.md` — portable, project-scoped, **committed**

What this project needs, independent of where it runs: Node 20, pnpm 9, Postgres 15, an internal registry, `libvips`, a `GH_TOKEN` with `repo` scope.

**This is the highest-value artifact Ferret produces.** It's the dependency manifest the repo never wrote, discovered empirically rather than guessed. It is true on every machine, so it belongs in git and travels to the next person.

Each requirement carries how it was learned:

```markdown
- **Node 20.x** — declared in `.nvmrc`
- **pnpm 9.x** — declared in `packageManager`
- **Postgres ≥14** — inferred: `docker-compose.yml` service, `DATABASE_URL` in `.env.example`
- **libvips** — inferred: `sharp` in dependencies, native build failed without it
- **`GH_TOKEN` with `repo`** — inferred: workflow reads it; scope determined by 403 on first attempt
```

`declared` means Ferret read it somewhere. `inferred` means Ferret worked it out — useful, and lower confidence. Marking the difference is what keeps the file honest.

### `status.md` — the glance, disposable

Right now, on this machine, for this project: what's GO, NO-GO, UNKNOWN, and what to do. Gitignored. Regenerated every run.

---

## 3. Volatility classes

A system doc that says GO from three months ago is a false green with a timestamp on it. Every claim carries a class, and re-verification is scheduled by class rather than wholesale.

| Class | Re-verify | Examples |
|---|---|---|
| `permanent` | Once | arch, libc flavor, OS family, case sensitivity |
| `stable` | On version change or 30d | installed tool versions, package manager, CA bundle path |
| `session` | Every run | PATH resolution, shell state, active version manager, disk free |
| `volatile` | Every run, minutes-valid | clock skew, rate limit standing, port availability |
| `expiring` | Per stored expiry | token scopes, SSO session, cert validity |

Record shape:

```json
{
  "id": "machine.libc",
  "value": "glibc 2.39",
  "volatility": "permanent",
  "observed_at": "2026-08-23T14:02:11Z",
  "provenance": "observed"
}
```

**A stale claim is UNKNOWN(expired), never GO.** The same invariant as everywhere else: not knowing must never render as knowing.

`ferret --refresh` re-verifies everything regardless of class, for when you don't trust the cache.

---

## 4. Provenance

Once Ferret can install things, "mise is present" means two different things depending on who put it there.

| Tag | Meaning |
|---|---|
| `observed` | Found as-is. A fact about the machine. |
| `ferret-installed` | Ferret put it there, with timestamp and method. |
| `user-remediated` | Ferret reported it; a human fixed it; a later run confirmed. |
| `inferred` | Deduced rather than directly probed. Lower confidence. |

Two reasons this is mandatory, not nice-to-have:

- **Clean uninstall.** Without provenance you cannot undo what Ferret did without risking removing something the user needed.
- **Honest findings.** A conflict Ferret created is not a finding about the machine. Untagged, Ferret reports its own footprint as a problem — and that's the exact false-positive failure that gets a tool ignored.

`ferret --footprint` lists everything tagged `ferret-installed`. `ferret --revert` undoes it.

---

## 5. The baseline rule

**Nothing mutates before a clean baseline is written.**

```
1. observe          read-only sweep, no exceptions
2. write baseline   evidence.json, provenance=observed, timestamped
3. remediate        opt-in, logged, tagged ferret-installed
4. re-observe       fresh sweep after changes
5. document         update system.md / requirements.md
6. next time        read docs, verify by volatility class, converge faster
```

Mutation is not the problem. **Unrecorded mutation is.** Step 2 is what preserves the ability to answer *"was this broken before I got here?"* — and that question is the entire value of a diagnostic tool.

Skipping step 2 for speed is never acceptable. Ferret exits 3 rather than remediate without a baseline.

---

## 6. Failed remediation degrades to reporting

The machines where remediation fails are the machines that need the report most: no sudo, no egress, TLS interception, immutable filesystem, broken package manager. Each of those is itself a high-priority finding.

So a failed install is **never fatal**. It becomes a record:

```
⚠ Could not install mise — no sudo, and no user-writable dir on PATH
  → 12 checks will run in reduced mode
  → Remediation available: see status.md § manual steps
```

Ferret continues with what it has and emits the diagnosis it already collected. A partial answer that announces its partiality is correct behavior; a tool that gives up because it couldn't modify the machine has failed at its actual job.

---

## 7. Cross-project reuse

The tenth project on a known machine:

1. Read `~/.ferret/system.md`
2. Re-verify `session` and `volatile` claims only — seconds, not minutes
3. Read `<repo>/.ferret/requirements.md` if the project has been seen before
4. Diff project requirements against verified system capability
5. Report only the delta

**The delta is the whole output.** "This machine has everything except Postgres" is the useful sentence, and it's reachable in seconds because the machine was documented once.

When `requirements.md` doesn't exist, Ferret discovers from scratch and writes one. The first run on a project is expensive; every run after is not.

---

## 8. What never goes in a committed file

`requirements.md` is committed, so it is subject to the same allowlist redaction as everything else — plus a stricter rule: **no machine-specific values at all.**

- Requirement: `GH_TOKEN with repo scope` ✓
- Never: the token, its shape, or its hash
- Requirement: `Node 20.x` ✓
- Never: `/Users/alice/.nvm/versions/node/v20.11.0/bin/node`

Resolved paths, ports, hostnames, and usernames belong in `status.md` or `system.md`, both gitignored. A committed file that leaks a laptop's layout is the mundane, likely failure — someone commits `.ferret/` by reflex.
