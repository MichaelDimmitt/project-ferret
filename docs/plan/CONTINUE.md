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
You are continuing Ferret Sniffer. Stage zero is built, tested, and
committed. The Go runner is not written yet. Take it to the finish line.

FIRST, ORIENT — do this before proposing anything

  1. Read docs/plan/PROMPT.md in full. It is the standing instruction:
     the design, the invariants, and what good looks like. Everything
     below assumes it.
  2. Read these, in order:
       README.md                        what the tool is
       docs/design/ARCHITECTURE.md      how it is built, and why
       docs/design/LANGUAGE_CHOICE.md   why Go, what mise changes
       docs/design/BOOTSTRAP_PIPELINE.md stage zero, already built
       docs/design/DOCS_MODEL.md        the three documents
       docs/plan/PLAN.md                milestones, in dependency order
     docs/design/FRAMEWORK.md is rationale for coverage, never a spec.
     Never execute from prose.
  3. Determine the current milestone from PLAN.md checkboxes and the
     repo state, NOT from anything I say. Restate it in one line.
  4. Run ./tests/run.sh and ./bootstrap/run.sh. Confirm green before
     you change anything. If either fails, fix that first and say so.

WHERE THINGS STAND

  Built and committed:
    bootstrap/run.sh + scripts/os/{common,darwin,linux}.sh
      Stage zero. POSIX sh, per-OS dispatch on `uname -s`, writes
      bootstrap/default-results.md. Verified under dash. An unknown OS
      runs the common core and records os-support: unknown.
    scripts/lib/redact.sh
      Two-level redaction (identity / paranoid), applied inside cell()
      so no probe can bypass it. Sourceable standalone.
    tests/run.sh
      Two test files, 40 assertions, plus shellcheck. All green.
    mise.toml, go.mod, ferret/main.go
      Toolchain pinned to Go 1.23. main.go is a stub that exits 3.

  NOT done — this is your work:
    M1  schema + runner core          <- start here
    M2  verdict engine
    M3  taint
    M4  the glance (the actual product)
    M5  redaction in the runner, read-only hardening
    M6  bootstrap fallback
    M7  the checks that matter
    M8  remaining layers
    M9  forge
    M10 document emission
    M10.5 stack derivation + approval gate
    M11 remediation
    M12 cross-project reuse
    M13 golden path

  NEVER VERIFIED, because this machine has no Go toolchain:
    ferret/main.go has never been compiled. `mise install` first, then
    `go vet ./...` and `go build`. Expect to fix it. Do not assume it
    is correct because it is short.

START WITH M1, AND STOP IN THE MIDDLE OF IT

  M1 is the contract. Every later milestone is expensive to change
  against a wrong schema, so it gets reviewed before it is built on.

  1. Write docs/design/MANIFEST_SCHEMA.md — every field, every `expect`
     type, worked examples. Then STOP and ask me to read it. Do not
     write runner.go until I have.
  2. After I approve: manifest/_schema.json, validated before any probe
     executes.
  3. Then ferret/runner.go.

HOW TO WORK

  One milestone at a time. Meet its exit criteria before proposing the
  next. Update PLAN.md checkboxes as you go; if reality diverges from
  the plan, edit the plan and say why.

  COMMIT AFTER EACH MILESTONE, and after each self-contained piece
  within one. Every commit must stand alone: it builds, its tests pass,
  and it is a usable bisect point. Verify that — checkout the commit
  and run the tests — rather than assuming it.

  Commit messages: what changed and WHY. State the bug a fix fixes and
  how it manifested. Say what you verified and what you could not.
  Never claim a test passed without running it.

  Work on a branch. Do not push or open a PR unless I ask.

  Run ./tests/run.sh before every commit. It runs shellcheck too.
  For Go: `go vet ./...` and `gofmt -l .` must be clean.

  Stop and ask when a decision contradicts ARCHITECTURE.md, when exit
  criteria are ambiguous, or when a design choice is load-bearing and
  not already settled. Do not silently pick.

INVARIANTS — violating any is a failed milestone, not a tradeoff

  Full list in docs/plan/PROMPT.md. The ones most at risk from here:

  - Go standard library only. No third-party modules. Not one.
  - The runner never interprets. It records raw stdout, stderr, exit
    code, duration. If the runner imports the verdict package, the
    design is broken. Keep them separate processes, not just packages.
  - No code path converts UNKNOWN into GO. Assert it in a test.
  - Phase 1 is read-only. Nothing installs, fetches, pulls, clones,
    writes config, or starts a service. Enforced by a denylist AND by a
    test asserting a fixture repo is byte-identical after a sweep.
  - Nothing mutates before a baseline is written. Exit 3 instead.
  - Nothing installs before the proposed list is approved. Preferred
    tool and ordered fallbacks are shown before approval, never
    discovered at execution time.
  - mise is proposed, never silently installed — not even to satisfy
    Ferret's own runtime. If it is declined, that is a finding: fall
    back to Tier-1 shell checks and report UNKNOWN, never GO.
  - Redaction is allowlist, at capture time, in the runner — never in
    the renderer. scripts/lib/redact.sh is the shell precedent; the Go
    side must agree with it on what "redacted" means.
  - Every fact carries provenance. Every NO-GO carries a remedy.
  - Exit code 2 (unknowns present) is never collapsed into 0.
  - .ferret/ is gitignored in the same commit that first creates it,
    with an un-ignore for requirements.md — that rule is not in
    .gitignore yet and M10 needs it.
  - status.md is not gitignored yet either. M4 creates it. Add the rule
    in the commit that creates the file.

WHAT GOOD LOOKS LIKE

  The tool gets this machine working, and shows its plan first.

  The report is read in ten seconds, ordered by severity rather than by
  layer, and never implies it verified something it could not check.

  A false positive is worse than a missing check. A tool that cries
  wolf gets ignored, and an ignored tool has failed completely rather
  than partially. Once M10 lands, a false positive gets written into a
  document and outlives the run that produced it.

  At M4, run it on this actual machine and read the output. If it does
  not tell you something true in ten seconds, the format is wrong and
  that is the cheap moment to say so — not after eighty checks exist.

START

State the current milestone. Confirm the tests are green. Then begin.
```

---

## Notes for whoever pastes this

**Two intervention points are deliberate.** After `MANIFEST_SCHEMA.md` (before
any runner code) and after M4 (the first time output is readable). Both are
moments where a wrong call is cheap now and expensive later. The prompt
instructs a stop at the first; the second is on you to actually do.

**The Go stub is unverified.** No toolchain existed on the machine where it was
written. That is stated in the prompt rather than hidden, because a session
that assumes it compiles will waste time confused.

**Known gaps deliberately left for their milestone**, rather than fixed early:

| Gap | Belongs to |
|---|---|
| `status.md` not gitignored | M4, which creates it |
| `.ferret/` un-ignore for `requirements.md` | M10, which creates it |
| Manifest schema has no fields for preferred tool / fallbacks / install gating | M10.5 |
| `ferret/bootstrap.sh` does not exist | M6 |

The first two are listed in the prompt because forgetting them means committing
a machine-specific file, and the `.gitignore`-in-the-same-commit rule exists
precisely to prevent that.

**Where to push back.** If a milestone's exit criteria turn out to be wrong
once there is real code, the plan should change — a stale plan is worse than no
plan. That is explicitly allowed, and it is better than quietly building
something that does not match.
