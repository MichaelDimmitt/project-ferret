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
You are continuing Ferret Sniffer. M0-M6 are built, tested, and committed,
and M7's Tier 1 has landed: the tool now runs 19 real checks and reports
true findings. What remains of M7 is Layers 4 and 5, which need real repos.
Take it to the finish line.

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
    ferret/manifest.go    loading + the §8 validation rules
    ferret/runner.go      probes, timeouts, denylists, env isolation.
                          Interprets nothing.
    ferret/verdict.go     GO/NO-GO/UNKNOWN/N-A, taint, exit codes
    ferret/semver.go      exactly the range table in §4, nothing more
    ferret/render.go      the glance -> stdout + .ferret/status.md
    ferret/redact.go      capture-time filtering (a backstop; see below)
    ferret/bootstrap.sh   the Tier-1 fallback, when there is no Go runtime
    manifest/00-machine.json  layer 0 — clock, disk, inodes, arch, libc
    manifest/02-network.json  layer 2 — proxy pairs, CA bundles, registry
    manifest/03-shell.json    layer 3 — PATH dupes, shadowing, fresh shell
    manifest/06-git.json      layer 6 — repo state, mid-op, shallow, safe.dir
      19 checks. Run on a real machine and confirmed by hand.
    tests/run.sh          shell suite + shellcheck + go vet/test/gofmt
      84 Go test functions plus 3 shell test files. All green.

  THE TOOL NOW WORKS. `./scripts/sweep.sh` reports real findings. On the
  machine it was built on it found three node installs, node missing from
  a clean environment, 10 duplicate PATH entries, and a dirty tree — each
  verified by hand before being believed.

  Toolchain: mise is installed and provides the pinned Go 1.23. If `go`
  is missing, `mise install` restores it — it is already approved, so
  this is not a new install decision.

  NOT done — this is your work:
    M7  Layers 4 and 5                    <- START HERE. Tier 1 is done;
                                             these are runtime-vs-pin and
                                             package manager, and they NEED
                                             REAL REPOS. See below.
    M8  remaining layers
    M9  forge
    M10 document emission
    M10.5 stack derivation + approval gate
    M11 remediation
    M12 cross-project reuse
    M13 golden path
    Also open, carried from M3: verdict-time cycle detection. Load-time
    detection exists; the verdict-time guard and exit-3 path do not.
    Also open, carried from M6: the container run with no Go toolchain.
    The invariant it would protect is already asserted by
    tests/bootstrap-fallback-test.sh; the container itself was never run.

FINISHING M7 — Layers 4 and 5

  Tier 1 is committed (7adb091). What is left is where the false
  positives actually live, because these checks depend on REPO SHAPE
  rather than on the machine:

    Layer 4 — runtime vs pin
      .nvmrc / .node-version / engines / packageManager / .tool-versions
      versus the version actually resolved. Version manager conflicts
      (two installed at once). Native build chain presence.
    Layer 5 — package manager
      Lockfile count (>1 is a warning), lockfile vs PM version, .npmrc
      registry and scopes and ignore-scripts WITHOUT opening auth lines,
      registry reachability, node_modules vs lockfile agreement.

  DO NOT WRITE THESE BLIND. PLAN.md's exit criterion is three real repos
  of different shapes, and the user is the one who supplies them —
  ideally including one whose pin is currently WRONG, since a repo where
  everything is correct cannot demonstrate the difference between a true
  finding and a false one. If they have not been supplied, ask before
  writing Layer 4/5 checks rather than inventing fixtures: a check that
  has only ever seen a synthetic repo is how a false positive ships.

  Layer 5's .npmrc checks are the sharpest secret-handling case in the
  project: `grep -c '_authToken'` for presence, `grep -o '^registry='`
  for the named non-secret line, and NEVER a match that includes a
  value. One character separates the check from the leak. runner.go
  refuses the leaking forms, and ferret/secrets_test.go pins it.

  THREE LESSONS FROM TIER 1, all found by reading output rather than by
  testing, and all likely to recur in Layers 4 and 5:

    1. PROBES ARE POSIX sh, AND sh IS NOT BASH. `command -v -a` is a
       bashism; macOS /bin/sh rejects it, the error goes to stderr, and
       a `| wc -l` then counts zero. The check reported "no node
       installs" on a machine with three, rendered as a confident pass.
       Run anything clever against /bin/sh before committing it.
    2. TAINT MEANS "THIS ANSWER CANNOT BE TRUSTED", not "something
       related also failed". Over-tainting converted the two most useful
       findings on the machine into UNKNOWNs buried under a GO verdict.
       The test: does the prerequisite's failure make the dependent's
       MEASUREMENT wrong? Clock skew taints registry reachability (skew
       breaks TLS, so the registry looks unreachable when it is fine).
       Duplicate PATH entries do NOT taint a de-duplicated count.
       Recorded in manifest/README.md.
    3. READ THE CAPTURED EVIDENCE, NOT THE RENDERED VERDICT. Both of the
       above looked fine in the glance. Dump evidence.json and check
       every value against what you independently know to be true —
       `applies=false` on a check that should have run, or a suspiciously
       round `0`, is where the bugs were.

  ON THE DUPLICATION, which is deliberate: ferret/bootstrap.sh and the
  manifest both implement the Tier-1 area, in shell and in JSON. The
  fallback is a floor for machines with no runtime. The MANIFEST is
  authoritative; the fallback stays deliberately dumber. This already
  paid off — the two implementations were cross-checked and agree on
  every overlapping value, which is independent confirmation no single
  implementation could give.

WHAT CHANGED SINCE THE PLAN WAS WRITTEN — read PLAN.md's inline notes

  The plan has been edited where reality diverged, and each divergence
  says why. Worth knowing before you start:

  - Taint landed in M2, not M3. A NO-GO prerequisite whose dependents
    rendered green would have been a false green shipped for a whole
    milestone.
  - "Never read a secret" is now a standing rule (AGENTS.md), stronger
    than the redaction M5 was scoped around. Ferret determines a secret
    EXISTS; it never learns the value. Shaping/hashing still exist but
    are a backstop against an authoring mistake, not the mechanism. This
    permanently removes some checks — token validity, auth-line contents
    — which are UNKNOWN(unverifiable) forever. Do not reintroduce them.
  - M5's secret fixtures are NOT fake credentials. AGENTS.md rule 5
    forbids writing a secret at all, calling a realistic fake "still a
    string someone will grep for and mistake for real". The fixtures
    carry a real token's SHAPE on a body that says in words it is not
    one. Follow that pattern; do not add plausible-looking fakes.
  - The glance has a NOTED section that is not in the plan. Non-decisive
    checks that FAILED were being swallowed by the "n passed" count.
  - `isolate` is a manifest field (MANIFEST_SCHEMA.md §3), added in M5.
    Opt-in `env -i`, for checks whose answer must hold in a fresh shell.
    THE GATE IS NEVER ISOLATED: `applies_if` runs in the real
    environment, `probe` and `declared` run cleared. Isolating the gate
    made the fresh-shell check self-cancelling — it failed in the clean
    env, resolved N/A, and silently skipped the exact problem it was
    written to detect. Pinned by TestGateIsNeverIsolated.
  - M6 shipped deliberately thin. All eight Tier-1 areas exist in the
    fallback, but only with assertions whose answer is unambiguous,
    precisely so M7 could decide what they really assert.
  - Submodules and LFS were deferred from M7 Tier 1 to M8, rather than
    written blind. Both need a repo that HAS them to test against, and
    an untested check is how a false positive ships. If you get such a
    repo, they belong in manifest/06-git.json.
  - Layer files are numbered by FRAMEWORK.md's layers, and gaps are
    expected: 00, 02, 03, 06 exist because those are the Tier-1 layers.
    01, 04, 05 and the rest arrive with their milestone.

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

  VERIFYING A COMMIT IN A FRESH CLONE: `mise which go` FAILS inside a
  clone, because the untrusted mise.toml blocks it. tests/run.sh then
  SKIPS every Go test and still exits 0 — which looks like a pass and
  verified nothing. This has already produced a false "verified" claim
  in this project. Resolve Go from the parent repo and pass it in:
    GO_DIR=$(dirname "$(mise which go)")   # from the real repo
    cd /tmp/clone && PATH="$GO_DIR:$PATH" ./tests/run.sh
  Then confirm the output actually says "go: vet and test clean" rather
  than "go: no toolchain — skipped".

  WRITE TEST OUTPUT OUTSIDE THE REPO UNDER TEST. A fixture whose
  status.md lands inside the repo being measured makes the working-tree
  check fire on the test's own artefacts, and it looks exactly like a
  false positive in the code.

  NORMALISE THE ENVIRONMENT IN SHELL TESTS. A test that inherits PATH
  takes whichever branch the developer's dotfiles produce — one machine
  exits 1, another exits 2, and the assertion that matters never runs.

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
  - No code path converts UNKNOWN into GO. The fallback holds this too:
    a clean machine there exits 2 and headlines UNKNOWN, never 0.
  - Phase 1 is read-only. Asserted by ferret/readonly_test.go against a
    real fixture repo, not just by the denylist.
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

  M7 IS WHERE THIS BECOMES USEFUL, and Tier 1 proved it: the tool found
  three node installs and a node that vanishes in a clean shell, on a
  machine whose owner did not know either. Layers 4 and 5 are where it
  gets harder, because they judge a repo rather than a box. Precision
  over coverage: every reported NO-GO must be real, and every false
  positive is a bug, not a rough edge.

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

**M7's intervention point is half done, and the half that remains is yours.**
Tier 1 was built and checked without repos, because those checks are about the
machine — and reading its output caught three defects that no test would have
(a bashism producing a confident false answer, `isolate` silently cancelling a
check, and over-tainting burying the two best findings under a GO verdict).

Layers 4 and 5 are where the repos become necessary, and they cannot be
honestly built without them. Pick three of different shapes that you know well
enough to spot a wrong answer, and **include one whose pin is currently
wrong** — a repo where everything is correct cannot demonstrate the difference
between a true finding and a false one.

The question to ask of the output is not "did it crash" but "is anything it
says wrong" — in both directions. A NO-GO that is not real is a bug; so is a
GO on something genuinely broken.

**Known gaps deliberately left for their milestone:**

| Gap | Belongs to |
|---|---|
| `.ferret/` un-ignore for `requirements.md` | M10, which creates it |
| Manifest schema has no fields for preferred tool / fallbacks / install gating | M10.5 |
| Verdict-time cycle detection | M3's remainder |
| Container run with no Go toolchain | M6's remainder |
| Layers 4 and 5 — runtime-vs-pin, package manager | M7's remainder; needs real repos |
| Submodules and LFS git checks | M8; deferred rather than written blind |

**Three traps this project has already fallen into.** Each produced a
confident "verified" or a confident wrong answer:

- `cmd | tail` reports *tail's* exit code. Redirect to a file instead.
- `./tests/run.sh` in a fresh clone silently skips every Go test — `mise which
  go` fails on an untrusted `mise.toml` — and still exits 0. Check the output
  says "vet and test clean", not "no toolchain — skipped".
- **A green glance is not a correct glance.** Two Tier-1 checks rendered as
  passing while measuring nothing: a bashism sent the real output to stderr and
  `wc -l` counted the empty result. Dump `evidence.json` and check the captured
  values against what you independently know, especially `applies=false` on a
  check that should have run.

**And one about the tool's own findings:** it reports a dirty working tree,
which will usually be *your own uncommitted work* mid-session. That is a true
finding, not a bug — but when testing, write outputs outside the repo being
measured, or the check fires on your own artefacts and looks like a false
positive.

**Where to push back.** If a milestone's exit criteria turn out to be wrong
once there is real code, the plan should change — a stale plan is worse than no
plan. That is explicitly allowed, and it is better than quietly building
something that does not match. This document has itself been rewritten three
times for exactly that reason.
