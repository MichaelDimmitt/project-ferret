# Plan

Milestones in dependency order. Each is independently verifiable — you can run something and see whether it worked. Do not start a milestone before its predecessor's exit criteria are met.

**Guiding constraint:** M0–M4 exist to prove the schema before it gets expensive to change. Do not populate all eleven layers early. Eighty checks written against a schema that turns out wrong is eighty checks to rewrite.

---

## M0 — Skeleton

Repo structure, gitignore, agent conventions.

- [x] `ferret/`, `manifest/`, `tests/fixtures/`
- [x] `bootstrap/`, `scripts/`, `docs/design/`, `docs/plan/` — runnable code apart from prose, design docs apart from build plans
- [x] `.gitignore` containing `.ferret/` — **in this commit, before any code can create it**
- [x] `AGENTS.md` with repo conventions; `CLAUDE.md` as a one-line pointer to it
- [x] `docs/design/FRAMEWORK.md` copied in (source of coverage truth, not executed)
- [x] `scripts/os/common.sh` + `scripts/os/{darwin,linux}.sh`; `default-script.sh.example` dispatches on `uname -s`
- [x] `scripts/sweep.sh` stub that resolves and runs the Go binary — locates a
      runner rather than assuming one; exits **3**, not 0, because an
      unimplemented sweep has verified nothing and 0 means GO
- [x] `mise.toml` pinning the Go version, so the build is reproducible

Note: `bootstrap/run.sh` (stage zero, POSIX sh) and `scripts/sweep.sh` (runner
entry) are two different entry points and both are expected.

Stage zero is shell and stays shell. It runs before mise exists, so it cannot
be written in a runtime it is measuring.
The probes are per-OS because the probes genuinely differ (`/proc` is Linux,
`sw_vers` and SIP are macOS, `getent` is glibc); a single script covering every
OS is wrong everywhere instead of right somewhere. An unrecognized `uname -s`
records `os-support: unknown (<uname>)`, runs the common core alone, and never
guesses at the nearest match.

**Exit:** `./bootstrap/run.sh` writes a well-formed `default-results.md` on
macOS and Linux, runs unchanged under `dash`, and degrades to the common core
on an unknown OS. `./scripts/sweep.sh` runs and prints "not implemented"
without traceback.

---

## M1 — Schema and runner core

The contract. Get this right and everything after is mechanical.

**Language: Go, stdlib only** — settled, see `docs/design/LANGUAGE_CHOICE.md`.
mise supplies the toolchain after the user approves it; `mise.toml` pins the
version. Rust is documented there as the live alternative, with the crate set
it would need, in case the four-state invariant proves hard to hold by
discipline alone.

`evidence.json` is the contract, not the language. A rewrite that emits the
same file leaves M2 onward untouched — which is why this milestone spends its
effort on the schema.

- [x] `docs/design/MANIFEST_SCHEMA.md` — every field, every `expect` type, worked examples
- [x] JSON Schema at `manifest/_schema.json`; validation runs before any probe executes
- [x] `ferret/runner.go` — loads manifest, evaluates `applies_if`, runs `probe` and `declared`, enforces `timeout_s`, writes `evidence.json`
      — loading and validation split into `ferret/manifest.go`; §8's twelve
      rules are substantial enough to be their own file, and the split keeps
      `runner.go` to execution
- [x] Four states with mandatory `reason` on UNKNOWN — the runner emits the
      four reasons it can determine alone (`timeout`, `tool_absent`,
      `permission`, `probe_error`). `tainted` is M3's, `expired` is M10's, and
      `unverifiable` is a property of the check rather than the run
- [x] **Runner performs no interpretation.** It records raw stdout, stderr, exit code, duration. It does not evaluate `expect`.
      — asserted by `TestEvidenceContainsNoVerdict`, which fails if a
      verdict-shaped key ever appears in a record
- [x] Mutation denylist with `mutating: false` override

**Exit:** three hand-written checks produce a valid `evidence.json`. A deliberately mutating probe is refused. A deliberately hanging probe times out and records UNKNOWN(timeout).

**Met.** Demonstrated end-to-end through `./scripts/sweep.sh`: the mutating
probe is refused in 0ms with the matched token named, and the hanging probe
returns at its `timeout_s` rather than its `sleep`. 32 Go tests, `go vet` and
`gofmt` clean, wired into `./tests/run.sh` alongside the shell suite.

Note for M2: the runner exits 0 or 3 only. It never exits 1 or 2, because
those are verdicts and it has not made one. The exit codes in ARCHITECTURE §11
belong to the process that decides.

---

## M2 — Verdict engine

- [x] `ferret/verdict.go` — applies `expect` to evidence, emits GO / NO-GO / UNKNOWN / N/A
- [x] Predicates: `equals`, `matches`, `one_of`, `non_empty`, `absent`, `numeric_lt`, `numeric_gt`, `semver_satisfies`, `exit_code`
      — semver in `ferret/semver.go`, scoped to the table in MANIFEST_SCHEMA.md
      §4. Anything outside it is UNKNOWN with the unparsed range in the reason,
      never an approximation
- [x] Reads `evidence.json` as its **only** input — never re-probes
      — `ferret -verdict` re-decides from a stored file
- [x] Test asserting **no code path turns UNKNOWN into GO**
      — `TestUnknownNeverBecomesGo` drives all six routes to UNKNOWN through
      one manifest and asserts none resolves GO, none lacks a reason, and the
      report does not exit 0

**Exit:** `verdict.go` runs standalone against a committed fixture `evidence.json` and produces stable output. Editing an `expect` and re-running changes the verdict without touching the machine.

**Met.** Against `tests/fixtures/verdict/`: clock skew 702s is NO-GO and taints
two checks whose own probes passed. Widening the tolerance to 900s in the
manifest alone flips it to GO, releases the taint, and the evidence file is
byte-identical afterwards — nothing was re-probed. 115 assertions.

Taint arrives late, deliberately: M3 owns the DAG, but a NO-GO prerequisite
that let its dependents render green would have been a false green shipped for
a whole milestone. What M3 still owes: cycle detection at verdict time
(currently only at load), and the ordering guarantees.

---

## M3 — Taint

Partly landed with M2 — a NO-GO prerequisite whose dependents rendered green
would have been a false green shipped for a whole milestone, so the propagation
could not wait. What remains is the DAG's own machinery.

- [x] DAG from `tainted_by` — *(landed in M2)* fixed-point propagation rather
      than a topological sort. Equivalent for an acyclic graph of this size and
      simpler to read; revisit if the manifest grows past a few hundred checks
- [x] NO-GO prerequisite ⇒ dependents become UNKNOWN(tainted), **probe output preserved in evidence**
      — *(landed in M2)* asserted by `TestTaintPreservesEvidence`
- [x] Rollup count on the cause for rendering — *(landed in M2)* the count
      lands on the ROOT cause, and a tainted check drops its own remedy,
      because the fix belongs to the prerequisite
- [ ] Cycle detection fails loudly with the offending ids — never resolves arbitrarily
      — load-time detection exists (§8 rule 4, `TestCycleNamesBothEnds`);
      still to do is the verdict-time guard and the exit-3 path
- [x] Transitive taint through a chain deeper than two — *(landed in M2)* the
      committed fixture chains clock → node → registry, and both dependents
      name the root cause rather than the intermediate

**Exit:** fixture with deliberate clock skew marks its dependents tainted and reports `→ taints N checks below`. Fixture with a cycle exits 3 naming both ends.

---

## M4 — The glance

The product. Everything before this is plumbing.

- [ ] `ferret/render.go` → `status.md` + stdout
- [ ] Ordered by **severity, not by layer**
- [ ] Sections: VERDICT / BLOCKERS / UNKNOWN / WARNINGS / golden-path line
- [ ] Every NO-GO carries its `remedy`
- [ ] Non-decisive checks collapsed to a count
- [ ] UNKNOWN visually distinct from GO — never the same glyph, never the same color
- [ ] Exit codes 0/1/2/3 per ARCHITECTURE §11
- [ ] Degrades in a pipe and in a terminal without color

**Exit:** output matches the README example shape on a fixture. **Then run it on your actual machine and read it.** If it doesn't tell you something true and useful in ten seconds, fix the format now — not after eighty more checks exist.

---

## M5 — Redaction and read-only hardening

Do this before the manifest grows, because both get harder to retrofit with volume.

- [ ] Allowlist redaction at capture time in the runner
- [x] *(landed early, at stage zero)* Redaction in `cell()` for
      `default-results.md`, at two levels — `--redact` (identity) and
      `--redact=paranoid` (adds fingerprint and posture); asserted by
      `tests/redaction-test.sh`. Stage zero produces machine-identifying data
      before the runner exists, so the guarantee had to start there. Two
      levels because leaking *who you are* and leaking *what the machine is*
      are different threats with different audiences.
- [ ] Presence-and-shape recording: `GH_TOKEN: <set, 40 chars, ghp_…>`
- [ ] Stable salted hashing for credential-shaped values
- [ ] Secret fixtures (fake `GH_TOKEN`, `.npmrc` `_authToken`, remote with embedded creds) asserted absent from both outputs
- [ ] Read-only test: full sweep against fixture repo, assert working tree + index + config hashes unchanged
- [ ] `env -i` isolation for shell-state probes

**Exit:** secret fixtures do not survive. Read-only test passes. `git status` clean after a sweep.

---

## M6 — Bootstrap fallback

- [ ] `ferret/bootstrap.sh`, POSIX sh, Tier-1 checks only
- [ ] Emits partial `status.md`, verdict **UNKNOWN**, never GO
- [ ] Header states plainly: `runner unavailable — N of M checks not run`
- [ ] `scripts/sweep.sh` detects and delegates

**Exit:** works in a container with no Go toolchain and mise declined. Verdict is UNKNOWN, and the reason is the first thing on screen.

---

## M7 — Vertical slice: the checks that matter

Now the schema is proven. Populate the highest-value layers.

**Tier 1 — fast, foundational, taint sources:**
- [ ] Clock skew, disk free, inodes, arch, libc flavor
- [ ] PATH shadowing (`which -a` on each key tool)
- [ ] Proxy vars incl. lowercase twins, CA bundle, `NODE_EXTRA_CA_CERTS`
- [ ] Git repo state: HEAD, dirty, mid-rebase/merge, `safe.directory`, shallow/sparse, submodules, LFS

**Layer 4 — runtime vs pin:**
- [ ] `.nvmrc` / `.node-version` / `engines` / `packageManager` / `.tool-versions` vs actual
- [ ] Version manager conflicts (two installed simultaneously)
- [ ] Native build chain presence

**Layer 5 — package manager:**
- [ ] Lockfile count (>1 is a warning), lockfile vs PM version
- [ ] `.npmrc` registry, scopes, `ignore-scripts`
- [ ] Registry reachability with timeout
- [ ] `node_modules` present vs lockfile agreement

**Exit:** run against three real repos of different shapes. Every reported NO-GO is real. **Every false positive is a bug** — a status check that cries wolf gets ignored, which is total failure, not partial.

---

## M8 — Remaining layers

Order by value, not by number.

- [ ] Layer 8 — project config, entry point discovery, `.env.example` vs required
- [ ] Layer 1 — case sensitivity, normalization, Windows path/reserved-name scan
- [ ] Layer 9 — AI/agent config; conflicts between coexisting instruction files
- [ ] Layer 3 — shell state, hook conflicts, login vs non-login
- [ ] Layers 0, 2, 10 — remaining machine, network, process items

**Exit:** coverage cross-checked against `FRAMEWORK.md`. Anything deliberately not implemented is listed in `docs/COVERAGE.md` with a reason — silent gaps are the failure this tool exists to prevent, and that applies to Ferret itself.

---

## M9 — Forge

Last, because it's the only layer needing network, auth, and identity stamping.

- [ ] Detect forge; `applies_if` gates GitHub-only checks
- [ ] `gh auth status`, token scopes, host config
- [ ] Branch protection, required checks on default branch
- [ ] CI ever passed on default branch
- [ ] Secret **names** vs names referenced in workflows — the cross-reference for the unverifiable case
- [ ] Every record stamped identity / scopes / host / captured_at
- [ ] Permission failures ⇒ UNKNOWN(permission), **never absent**
- [ ] Header line declaring identity and ceiling

**Exit:** run authenticated and unauthenticated. Unauthenticated produces UNKNOWNs with reasons, not silent gaps, and not a green verdict.

---

## M10 — Document emission

The convergence loop's memory. Requires precise checks (M7+), because a doc built on false positives propagates them forward.

- [ ] `~/.ferret/system.md` — durable machine facts, volatility class per claim
- [ ] `<repo>/.ferret/requirements.md` — portable, **committed**, each entry marked `declared` or `inferred`
- [ ] `.gitignore` rule: ignore `.ferret/`, un-ignore `requirements.md`
- [ ] Provenance tags on every fact: `observed` / `ferret-installed` / `user-remediated` / `inferred`
- [ ] Stale claims render UNKNOWN(expired), never GO
- [ ] `--refresh` forces full re-verification regardless of class
- [ ] Test: no machine-specific value (path, port, hostname, username) reaches `requirements.md`

**Exit:** run on a project twice. Second run reads the docs, re-verifies only `session` and `volatile` claims, and is materially faster. `requirements.md` is committable without leaking the machine.

---

## M10.5 — Stack derivation and the approval gate

Before M11, because remediation without an approved plan is the thing this
tool exists not to do. Specified in `docs/design/BOOTSTRAP_PIPELINE.md`.

- [ ] `ai-preferred-stack.md` — preferred tool per requirement, ordered fallbacks with the cost of each, degrading toward POSIX
- [ ] `ai-do-not-use-list.md` — standing user policy, applied as a filter *before* the proposal is built
- [ ] Manifest schema fields for preferred tool, ordered alternatives, install-permission gating — the model has no schema yet
- [ ] Derive the stack from the repo (lockfiles, `.nvmrc`, compose, CI) and diff against `bootstrap/default-results.md`
- [ ] Preference is contextual: `mise` already present routes version management through it and shortens the chain
- [ ] Render the proposal — one editable list, fallbacks visible before approval
- [ ] `--approve` and `--edit`; no install path bypasses them
- [ ] Exhausted chain asks the user to pick from what remains; never improvises, never silently skips
- [ ] Test: a ban that empties a chain produces an honest dead end with the affected checks named, not a workaround

**Exit:** on a machine missing two tools, Ferret proposes both with fallbacks shown, installs nothing until approved, and a `--edit` that removes a line results in that tool not being installed. A do-not-use entry keeps its tool off the list entirely.

---

## M11 — Remediation

Last of the core work, deliberately. Remediation before the checks are precise is remediation aimed at false positives.

- [ ] Executes only what M10.5's gate approved
- [ ] `--remediate`, opt-in, never default
- [ ] **Refuses to run without a written baseline** — exits 3 rather than proceed
- [ ] Detect OS package manager; select install method by `applies_if`
- [ ] Every change tagged `ferret-installed` with method and timestamp
- [ ] Failed install is non-fatal: record the failure, mark affected checks reduced-mode, continue
- [ ] Re-observe after remediation; update docs
- [ ] `--footprint` lists Ferret's changes; `--revert` undoes them
- [ ] Test: on a machine with no sudo and no egress, Ferret still emits a complete diagnosis

**Exit:** container missing a toolchain converges to operational and documents what it did. The same container with installs blocked produces a diagnosis rather than an error.

---

## M12 — Cross-project reuse

- [ ] Read prior `system.md`; skip `permanent` and unexpired `stable` claims
- [ ] Diff project requirements against verified system capability
- [ ] Report only the delta — *"this machine has everything except Postgres"*
- [ ] Cold path preserved: no prior docs ⇒ full discovery, then write them

**Exit:** tenth project on a known machine reports in seconds, and the output is the delta rather than a full sweep.

---

## M13 — Golden path

The real verdict. Everything before it is preflight that explains a failure.

- [ ] `ferret --golden`: temp dir, fresh clone, `env -i`, install → build → test → run
- [ ] Refuses when blockers outstanding: `NOT ATTEMPTED (blocked)`
- [ ] Per-stage timing and failure capture
- [ ] Opt-in; writes only inside its temp directory

**Exit:** passes on a known-good repo, fails informatively on a repo with a broken lockfile.

---

## Later, explicitly not now

- Cross-machine diff (`evidence.json` already supports it)
- `windows-latest` CI job doing nothing but clone — catches path-length and reserved-name defects nothing else will
- Non-Node ecosystems
- `--json` for downstream tooling

---

## Failure modes to watch for while building

- **Growing the manifest before M4 is read by a human.** The format is the product; validate it on yourself early.
- **Any UNKNOWN rendering green.** Assert it in tests, not in review.
- **False positives.** One wolf-cry and the tool gets ignored. Precision beats coverage — and once M10 lands, a false positive gets written into a document and outlives the run.
- **Interpretation creeping into the runner.** If `runner.go` imports anything from `verdict.go`, the separation is gone.
- **Remedies going stale.** A wrong remedy is worse than none.
- **Remediating before baselining.** Destroys the ability to answer "was this broken before I got here?" — which is the whole value of the tool.
- **Ferret reporting its own footprint as a finding.** Provenance tags exist to prevent this; untagged facts make the tool untrustworthy about itself.
