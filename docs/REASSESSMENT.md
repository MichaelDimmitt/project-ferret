# Reassessment — 2026-08-24

Written at the end of M7, when the tool works and the question stopped being
*does it run* and became *is this the thing we meant to build*.

The short answer: the checks are excellent and the delivery vehicle is
heavier than the intent supports. `simple-ferret` is the response. This
document records the evidence, so the decision is re-examinable rather than
a mood.

`docs/SPIRIT.md` is the intent this is measured against.

---

## 1. The measurements

Taken on the machine this was built on, macOS, warm caches.

| Path | What runs | Time |
|---|---|---|
| `ferret/bootstrap.sh` | Tier-1 shell checks, no runtime | **~0.13s** |
| Compiled binary, warm | 34 checks across 6 layers | **~0.9s** |
| `scripts/sweep.sh` cold | compile, then run | **~1.8s** |

And the cost of the fast path:

| Component | Lines | Note |
|---|---|---|
| Go engine (non-test) | 3,124 | manifest, runner, verdict, render, redact, semver |
| Go tests | 2,800 | 89 test functions |
| Shell (stage zero + fallback) | 1,107 | POSIX sh, no runtime |
| Manifest checks (JSON) | 517 | 34 checks — **the actual product** |
| Go toolchain to build it | — | **261 MB** |

**261 MB of prerequisite to save 0.7 seconds.**

That is the whole finding. Not that the Go engine is bad — it is careful,
well-tested code that caught four real defects this session. But it is
answering a performance question nobody asked, at an installation cost that
contradicts the premise.

**The comparison is not apples-to-apples, and the honest version is still
damning.** The shell fallback runs 8 Tier-1 checks; the engine runs 34. So
the fair reading is not "shell does the same work 7× faster" — it is that
**both are already fast enough**, and the difference between them is
imperceptible to a human waiting for an answer. Per-check, shell costs
roughly 16ms and the engine roughly 26ms; neither is the bottleneck, because
the bottleneck is the probes themselves. Nothing about the workload — run a
command, compare a string — needs a compiled language. The toolchain was
never buying speed that mattered.

## 2. Where the value actually is

Sort the artifact by "what would I miss if it vanished":

**Irreplaceable — the hard-won part.** The 517 lines of manifest, and more
precisely the *judgment encoded in them*: which questions are worth asking,
what counts as an answer, and what must not be tainted by what. Every one of
those 34 checks was run against a real machine or a real repo and confirmed
by hand. The `notes` fields carry the reasoning, and several carry a defect
that shipped and was caught.

**Valuable and portable — the lessons.** `manifest/README.md`,
`docs/plan/PLAN.md`'s inline post-mortems, and the defect record in the git
history. `command -v -a` is a bashism. Over-tainting buries the best finding.
A green glance is not a correct glance. These are worth more than the code
they were learned in.

**Good code, wrong weight class — the engine.** The taint DAG, the validation
layer, the redaction subsystem, the semver range table. All correct, all
tested, all requiring a 261 MB toolchain to run 34 shell commands and compare
strings.

**Already right — stage zero.** POSIX sh, per-OS dispatch, no runtime. This
part never drifted, because it *couldn't*: it was specified as shell for a
reason that still holds.

## 3. What went wrong, mechanically

No single bad decision. A sequence of locally-correct ones:

1. Checks need to be data, not code, so they can be reviewed → a manifest.
2. A manifest needs validation, or a typo silently means "10 seconds
   forever" → a schema and a loader.
3. Results need to distinguish "broken" from "unmeasurable" → a verdict
   engine with four states.
4. A failed prerequisite makes dependents untrustworthy → a taint DAG.
5. Captured output might contain secrets → a redaction layer.
6. All of that needs a real language → Go, and therefore a toolchain.

Every step follows. The premise — *drop it on a machine, get an answer in
seconds* — was never revisited against step 6, and by then the toolchain was
load-bearing.

**The tell was visible earlier than it was noticed.** M6 built a shell
fallback that implements the same Tier-1 area independently, explicitly as a
floor for machines with no runtime. The two implementations were
cross-checked and agree on every overlapping value. That was recorded as
*validation of the duplication*. Read against intent, it is something else:
**the shell version was sufficient, and we built it second and called it the
fallback.**

## 4. What `simple-ferret` should be

A restatement, not a rewrite from zero.

**Shape:** a single POSIX `sh` script, plus the checks as data, plus a skill
or prompt that reads the output. No compile step, no runtime to install, no
binary to trust. `curl`-able, readable in one sitting, works on a machine
with nothing on it — which is the machine it exists for.

**What it keeps, verbatim where possible:**
- The 34 checks' *substance* — every probe, expectation, and severity
- The taint relationships, which are the non-obvious part
- The `notes` reasoning, especially the defect post-mortems
- Stage zero, which is already the right thing
- The four rules in `SPIRIT.md`, as the standing instruction for the AI half

**What it drops, and why:**
- **The Go engine.** Its whole job — run a command, compare a string, decide
  a state — is what shell does. It bought 0.7 seconds for 261 MB.
- **JSON as the check format.** Chosen so Go could parse it. Shell cannot,
  without `jq` (a dependency) or a fragile parser. Checks should live in a
  format `sh` reads natively.
- **The four-state verdict engine as code.** GO/NO-GO/UNKNOWN/N-A is the
  right *model* and stays. Its implementation is a case statement.
- **Redaction as a subsystem.** *Never read a secret* is the real guarantee;
  redaction was always documented as a backstop. Probes that don't read
  values don't need a shaping layer behind them.

**What moves to the AI half:** severity ordering, cross-check synthesis,
remediation planning, and the entire proposal/approval flow that M10.5 and
M11 were going to build. A model reading a plain status dump does this better
than code can, and adapts to machines its author never saw. That is the
`simple-ferret` bet: **deterministic measurement, model-driven judgment.**

## 5. What this costs, stated honestly

Not free, and the losses should be named rather than discovered:

- **Validation goes away.** No schema means a malformed check fails at
  runtime instead of before any probe runs. The mitigation is fewer checks in
  a simpler format, reviewed by eye.
- **`--verdict` against stored evidence gets harder.** Re-deciding yesterday's
  evidence with an edited expectation is a genuinely good property that a
  shell version will do worse.
- **The taint DAG becomes manual.** Fixed-point propagation in `sh` is
  unpleasant. Likely answer: a flat prerequisite list and one pass, accepting
  shallower chains than the DAG supports.
- **Tests get weaker.** 89 Go test functions become shell assertions. Some
  properties currently pinned by tests — cycle refusal, remedy interpolation —
  will be pinned by convention instead, which is worse.

**These are real, and the trade is still right**, because a tool that needs a
261 MB install to answer in 0.9s instead of 0.13s doesn't get installed, and
an uninstalled tool's test coverage is irrelevant.

## 6. What this project becomes

**Not deleted, and not a failure.** `project-ferret` is where the checks were
learned, where the defect record lives, and where the design questions were
answered in enough detail to be re-answered quickly. Several answers —
never read a secret, the gate is never isolated, taint means untrustworthy
not related — are correct independent of implementation.

It stays as the **reference implementation and the source of record**.
`simple-ferret` is the deliverable.

**One caveat that applies to both**, and is written nowhere else: this has
**only ever run on macOS**. `scripts/os/linux.sh` exists for stage zero, but
the manifest has never been exercised on Linux, and I introduced
Homebrew-specific paths (`/opt/homebrew/opt/nvm/nvm.sh`) into layer 4 today.
The portability claim is untested. Given the class of defect found by simply
running on one real machine, running on a second OS should be expected to
find more — and should happen before anything mutates a machine.

## 7. The immediate consequence for this repo

M8 through M13 as planned are now questionable, since they build further on
the engine that is being set aside. Before writing more of it, the
`simple-ferret` restatement should be attempted, because it will show which
of those milestones survive contact with a shell implementation.

M10.5 and M11 in particular — stack derivation, approval gate, remediation —
are the ones most likely to belong to the AI half rather than to code.
