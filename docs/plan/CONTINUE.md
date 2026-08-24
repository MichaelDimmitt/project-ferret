# Continuation prompt

Paste the block below to start a fresh session and carry the build to the
finish line. It is written to be re-pasteable: it derives position from the
repo rather than from conversation history, so starting over mid-project
works.

`PROMPT.md` is the standing instruction — the design, the invariants, what
good looks like. This document is the operating procedure for a session that
picks up mid-build. Read both.

---

## The prompt

```
You are continuing Ferret Sniffer. M0-M4 are built, tested, and committed:
the pipeline runs end to end, but the manifest is empty, so it currently
checks nothing. Take it to the finish line.

FIRST, ORIENT — do this before proposing anything

  1. Read docs/plan/PROMPT.md in full. It is the standing instruction:
     the design, the invariants, and what good looks like. Everything
     below assumes it.
  2. Read AGENTS.md. Two rules there bind you directly, not just the
     code you write: never read a secret, and read machine state from
     bootstrap/default-results.md rather than guessing it.
  3. Read these, in order:
       README.md                        what the tool is
       docs/design/ARCHITECTURE.md      how it is built, and why
       docs/design/MANIFEST_SCHEMA.md   THE CONTRACT — every field, every
                                        expect type, and §11's known limits
       docs/design/DOCS_MODEL.md        the three documents
       docs/plan/PLAN.md                milestones, in dependency order
     docs/design/FRAMEWORK.md is rationale for coverage, never a spec.
     LANGUAGE_CHOICE.md and BOOTSTRAP_PIPELINE.md are settled background;
     read them if a decision turns on why Go or how stage zero works.
     Never execute from prose.
  4. Determine the current milestone from PLAN.md checkboxes and the
     repo state, NOT from anything I say. Restate it in one line.
  5. Run ./tests/run.sh. Confirm green before you change anything. If it
     fails, fix that first and say so.

WHERE THINGS STAND

  Built, tested, committed:
    bootstrap/run.sh + scripts/os/{common,darwin,linux}.sh
      Stage zero. POSIX sh, per-OS dispatch, writes default-results.md.
      Still the only part a user can run today and learn something from.
    ferret/manifest.go    loading + the twelve §8 validation rules
    ferret/runner.go      probes, timeouts, denylists. Interprets nothing.
    ferret/verdict.go     GO/NO-GO/UNKNOWN/N-A, taint, exit codes
    ferret/semver.go      exactly the range table in §4, nothing more
    ferret/render.go      the glance -> stdout + .ferret/status.md
    ferret/redact.go      capture-time filtering (a backstop; see below)
    tests/run.sh          shell suite + shellcheck + go vet/test/gofmt
      137 Go assertions plus the shell tests. All green.

  THE GAP THAT MATTERS: manifest/ holds _schema.json and a README, and
  no checks at all. `./scripts/sweep.sh` runs the whole pipeline and
  reports on nothing. Everything above is machinery waiting for M7 to
  give it work.

  Toolchain: mise is installed and provides the pinned Go 1.23. If `go`
  is missing, `mise install` restores it — it is already approved, so
  this is not a new install decision.

  NOT done — this is your work:
    M5  redaction + read-only hardening   <- start here
    M6  bootstrap fallback
    M7  the checks that matter            <- the milestone that creates
                                             user value; nothing before
                                             it does
    M8  remaining layers
    M9  forge
    M10 document emission
    M10.5 stack derivation + approval gate
    M11 remediation
    M12 cross-project reuse
    M13 golden path
    Also open, carried from M3: verdict-time cycle detection. Load-time
    detection exists; the verdict-time guard and exit-3 path do not.

WHAT CHANGED SINCE THE PLAN WAS WRITTEN — read PLAN.md's inline notes

  The plan has been edited where reality diverged, and each divergence
  says why. Three worth knowing before you start:

  - Taint landed in M2, not M3. A NO-GO prerequisite whose dependents
    rendered green would have been a false green shipped for a whole
    milestone.
  - "Never read a secret" is now a standing rule (AGENTS.md), stronger
    than the redaction M5 was scoped around. Ferret determines a secret
    EXISTS; it never learns the value. Shaping/hashing still exist but
    are a backstop against an authoring mistake, not the mechanism. This
    permanently removes some checks — token validity, auth-line contents
    — which are UNKNOWN(unverifiable) forever. Do not reintroduce them.
  - The glance has a NOTED section that is not in the plan. Non-decisive
    checks that FAILED were being swallowed by the "n passed" count.

HOW TO WORK

  One milestone at a time. Meet its exit criteria before proposing the
  next. Update PLAN.md checkboxes as you go; if reality diverges from
  the plan, edit the plan and say why.

  COMMIT AFTER EACH MILESTONE, and after each self-contained piece
  within one. Every commit must stand alone: it builds, its tests pass,
  and it is a usable bisect point. Verify that — clone to a temp dir,
  checkout the commit, run the tests — rather than assuming it.

  Commit messages: what changed and WHY. State the bug a fix fixes and
  how it manifested. Say what you verified and what you could not.
  Never claim a test passed without running it.

  Work on a branch. Do not push or open a PR unless I ask.

  Run ./tests/run.sh before every commit. It runs shellcheck, go vet,
  go test, and gofmt.

  CHECKING EXIT CODES: redirect to a file, never pipe to tail or grep.
  `cmd | tail` reports tail's exit code, and that has already produced
  two false "verified" claims in this project.

  Stop and ask when a decision contradicts ARCHITECTURE.md, when exit
  criteria are ambiguous, or when a design choice is load-bearing and
  not already settled. Do not silently pick.

INVARIANTS — violating any is a failed milestone, not a tradeoff

  Full list in docs/plan/PROMPT.md and AGENTS.md. Most at risk from here:

  - Go standard library only. No third-party modules. Not one.
  - Never read a secret — not to hash it, not to redact it, not in
    passing. Test for presence; mark the file; never open it. Binds you
    directly, not only the probes you write.
  - The runner never interprets. If runner.go reads `expect`,
    `severity`, `remedy`, or `tainted_by`, the design is broken.
  - No code path converts UNKNOWN into GO.
  - Phase 1 is read-only. M5 owes the test asserting a fixture repo is
    byte-identical after a sweep.
  - Nothing mutates before a baseline is written. Exit 3 instead.
  - Nothing installs before the proposed list is approved. mise is
    proposed, never silently installed — including for Ferret's own
    runtime.
  - Every fact carries provenance. Every NO-GO carries a remedy.
  - Exit code 2 (unknowns present) is never collapsed into 0.
  - .ferret/ is gitignored; M10 must add the un-ignore for
    requirements.md in the same commit that first writes it.

WHAT GOOD LOOKS LIKE

  The tool gets this machine working, and shows its plan first.

  The report is read in ten seconds, ordered by severity rather than by
  layer, and never implies it verified something it could not check.

  A false positive is worse than a missing check. A tool that cries
  wolf gets ignored, and an ignored tool has failed completely rather
  than partially. Once M10 lands, a false positive gets written into a
  document and outlives the run that produced it.

  M7 IS WHERE THIS BECOMES USEFUL. Everything committed so far is
  machinery. Precision over coverage: every reported NO-GO must be real,
  and every false positive is a bug, not a rough edge.

START

State the current milestone. Confirm the tests are green. Then begin.
```

---

## Notes for whoever pastes this

**The format has been read and judged.** M4's intervention point is done: the
glance was run against a real machine, found four defects by being read rather
than tested, and they were fixed. The remaining open questions on it are
listed at the end of that session — placeholder sections, one glyph for both
warnings and blockers, and a passed-count that names nothing.

**The next intervention point is during M7**, and it is the important one.
Every false positive is a bug report. The plan says run against three real
repos of different shapes; do that on repos you know well enough to spot a
wrong answer, because a plausible-looking wrong answer is the failure mode
that matters.

**Known gaps deliberately left for their milestone:**

| Gap | Belongs to |
|---|---|
| `.ferret/` un-ignore for `requirements.md` | M10, which creates it |
| Manifest schema has no fields for preferred tool / fallbacks / install gating | M10.5 |
| `ferret/bootstrap.sh` does not exist | M6 |
| Verdict-time cycle detection | M3's remainder |

**Where to push back.** If a milestone's exit criteria turn out to be wrong
once there is real code, the plan should change — a stale plan is worse than no
plan. That is explicitly allowed, and it is better than quietly building
something that does not match. This document has itself been rewritten once for
exactly that reason.
