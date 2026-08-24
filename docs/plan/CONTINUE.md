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
You are continuing Ferret Sniffer. M0-M7 are built, tested, and committed,
and M3's last box closed with them. The tool runs 34 real checks across six
layers and reports true findings about both a machine and a repo. What
remains is M8 onward. Take it to the finish line.

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
  4. Read manifest/README.md. It is short, and every line of it was
     written after a defect that shipped.
  5. Determine the current milestone from PLAN.md checkboxes and the
     repo state, NOT from anything I say. Restate it in one line.
  6. Run ./tests/run.sh. Confirm green before you change anything. If it
     fails, fix that first and say so.

WHERE THINGS STAND

  Built, tested, committed:
    bootstrap/run.sh + scripts/os/{common,darwin,linux}.sh
      Stage zero. POSIX sh, per-OS dispatch, writes default-results.md.
    ferret/manifest.go    loading + the §8 validation rules
    ferret/runner.go      probes, timeouts, denylists, env isolation.
                          Interprets nothing.
    ferret/verdict.go     GO/NO-GO/UNKNOWN/N-A, taint, cycles, exit codes
    ferret/semver.go      exactly the range table in §4, nothing more
    ferret/render.go      the glance -> stdout + .ferret/status.md
    ferret/redact.go      capture-time filtering (a backstop; see below)
    ferret/bootstrap.sh   the Tier-1 fallback, when there is no Go runtime
    manifest/00-machine.json         layer 0 — clock, disk, inodes, arch, libc
    manifest/02-network.json         layer 2 — proxy pairs, CA, registry
    manifest/03-shell.json           layer 3 — PATH dupes, shadowing, fresh shell
    manifest/04-runtime.json         layer 4 — pins, version managers, build chain
    manifest/05-packagemanager.json  layer 5 — lockfiles, PM version, .npmrc
    manifest/06-git.json             layer 6 — repo state, mid-op, shallow, safe.dir
      34 checks. Every one run against real repos and confirmed by hand.
    tests/run.sh          shell suite + shellcheck + go vet/test/gofmt
      89 Go test functions plus 3 shell test files. All green.

  THE TOOL WORKS ON BOTH AXES. Tier 1 judges the machine; Layers 4 and 5
  judge a repo. Run against three real repos it produced three different
  and correct verdicts: NO-GO on a repo whose .nvmrc pin was violated and
  whose node_modules was missing, GO on a repo whose npm-version warning
  was real but not blocking, and GO with 17 N/A on a Rust repo where every
  node question correctly declined to fire.

  Toolchain: mise is installed and provides the pinned Go 1.23. If `go`
  is missing, `mise install` restores it — it is already approved, so
  this is not a new install decision.

  NOT done — this is your work:
    M8  remaining layers                  <- START HERE. See below.
    M9  forge
    M10 document emission
    M10.5 stack derivation + approval gate
    M11 remediation
    M12 cross-project reuse
    M13 golden path
    Also open, carried from M6: the container run with no Go toolchain.
    The invariant it would protect is already asserted by
    tests/bootstrap-fallback-test.sh; the container itself was never run.
    Also open, and NOT a milestone: four Layer 5 branches have never seen
    a repo that exercises them. See "the weakest thing in the tree".

M8 — REMAINING LAYERS

  PLAN.md orders these by value, not by number. They split by what they
  need, and that split should drive your order:

    Testable on the machine you are on, like Tier 1 was:
      Layer 3 remainder — shell state, hook conflicts, login vs non-login
      Layers 0, 2, 10 remainder — machine, network, process items

    Needs a repo of a specific shape, and should NOT be written without one:
      Layer 8 — project config, entry points, .env.example vs required
      Layer 1 — case sensitivity, normalization, Windows reserved names.
                Meaningless to test on a case-insensitive volume, which
                macOS gives you by default. Ask for a case-sensitive
                volume or defer with a reason.
      Layer 9 — AI/agent config, and conflicts between coexisting
                instruction files. Needs a repo with several.
      Submodules and LFS — deferred here from M7 Tier 1, still deferred
                for the same reason. They belong in manifest/06-git.json
                when a repo that HAS them turns up.

  M8's exit criterion needs docs/COVERAGE.md, WHICH DOES NOT EXIST YET.
  That file is where anything deliberately not implemented gets listed
  with its reason. It is arguably the more valuable half of this
  milestone: the deferrals have been accumulating since M6 and currently
  live scattered across commit messages and PLAN.md prose. Silent gaps
  are the failure this tool exists to prevent, and Ferret is not exempt.

THE WEAKEST THING IN THE TREE, and it is not a milestone

  Four Layer 5 branches have only ever run against their absent/default
  case, because no repo was available that exercised them:

    pm.lockfile.count      the >1 branch (no two-lockfile repo)
    pm.npmrc.registry      a private registry line
    pm.npmrc.ignore_scripts a repo that sets it
    pm.npmrc.auth_present  an .npmrc that actually carries a token

  They are written to the same pattern as the branches that were
  confirmed, and they are still the checks most likely to be wrong. By
  this project's own standard — "an untested check is how a false
  positive ships" — M7 met the letter of its exit criterion here and not
  its spirit. If a repo of that shape turns up, exercising these beats
  adding new coverage.

FIVE LESSONS, all found by reading output rather than by testing

  The first three came from Tier 1, the last two from Layers 4 and 5.
  Every one of them produced a green suite and a wrong answer.

    1. PROBES ARE POSIX sh, AND sh IS NOT BASH. `command -v -a` is a
       bashism; macOS /bin/sh rejects it, the error goes to stderr, and
       a `| wc -l` then counts zero. The check reported "no node
       installs" on a machine with three, rendered as a confident pass.
       Run anything clever against /bin/sh before committing it.
    2. TAINT MEANS "THIS ANSWER CANNOT BE TRUSTED", not "something
       related also failed". The test: does the prerequisite's failure
       make the dependent's MEASUREMENT wrong? Clock skew taints
       registry reachability (skew breaks TLS, so the registry looks
       unreachable when it is fine). Duplicate PATH entries do NOT taint
       a de-duplicated count. THIS MISTAKE HAS NOW BEEN MADE TWICE, in
       Tier 1 and again in Layer 4 — where tainting the node-pin check by
       node_shadowing sounded obviously right and was wrong, because none
       of the three installs on PATH satisfied the pin, so the answer held
       whichever one answered. Both times it buried the single most
       valuable finding in the report. Recorded in manifest/README.md.
    3. READ THE CAPTURED EVIDENCE, NOT THE RENDERED VERDICT. Dump
       evidence.json and check every value against what you
       independently know to be true — `applies=false` on a check that
       should have run, or a suspiciously round `0`, is where the bugs
       were.
    4. SHELL TEXT-MUNGING CORRUPTS QUIETLY. `tr -d ' \n'` on a
       package.json turned `>=16 <21` into `>=16<21` (unparseable, so
       UNKNOWN and coverage silently lost) and `^18 || ^20` into
       `^18||^20`, WHICH STILL PARSES — a corrupted range that would have
       rendered as a confident NO-GO. Extract with sed against real
       files and read the resulting string before trusting it.
    5. A CORRECT CHECK CAN STILL GIVE BROKEN ADVICE. Every remedy using
       a §5 token printed it literally: the schema's own example, `nvm
       use $declared`, handed the reader a command naming an unset shell
       variable. The checks were right, the suite was green, and only
       reading a rendered glance caught it. PLAN.md says a wrong remedy
       is worse than none.

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
  - M3's cycle box was reopened and closed, and the gap was NOT what the
    plan described. Both CLI paths already exited 3 naming both ends,
    because LoadManifest guards every route. The real hole was that
    Evaluate is exported and relied on the caller having validated: given
    a cyclic graph directly it did not hang or panic, it returned mutual
    blame — two genuinely-failed checks each naming the OTHER as root
    cause, so two real NO-GOs became zero actionable findings, silently.
    Guarded by findTaintCycle, verified by disabling the guard and
    confirming the tests fail.
  - "Never read a secret" is now a standing rule (AGENTS.md), stronger
    than the redaction M5 was scoped around. Ferret determines a secret
    EXISTS; it never learns the value. Shaping/hashing still exist but
    are a backstop against an authoring mistake, not the mechanism. This
    permanently removes some checks — token validity, auth-line contents
    — which are UNKNOWN(unverifiable) forever. Do not reintroduce them.
  - REDACTION BIT BACK EXACTLY WHERE M5 PREDICTED. `redact: "secret"`
    routed everything through shapeOf, so shapeLines — written precisely
    so `registry=` and `ignore-scripts=` survive — was unreachable from
    the only path that reaches it in production. `grep -c '_authToken'`
    returned `1`, which was shaped into a hash, so an `equals "0"`
    expectation could neither pass nor honestly fail. If you add a check
    at redact: secret, assert what its capture looks like AFTER
    redaction, not just that the probe is safe.
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
  - A CHECK LIVES IN ONE LAYER ONLY. PLAN.md lists registry reachability
    under layer 5; network.registry.reachable already implemented it in
    layer 2. It was NOT duplicated — a second copy reports one outage as
    two findings, which is a false positive by duplication. When the plan
    and the manifest disagree about where a check belongs, the manifest
    wins and the plan gets a note.
  - Layer files are numbered by FRAMEWORK.md's layers, and gaps are
    expected: 00, 02, 03, 04, 05, 06 exist. 01 and the rest arrive with
    their milestone.

HOW TO WORK

  One milestone at a time. Meet its exit criteria before proposing the
  next. Update PLAN.md checkboxes as you go; if reality diverges from
  the plan, edit the plan and say why.

  COMMIT AFTER EACH MILESTONE, and after each self-contained piece
  within one. Every commit must stand alone: it builds, its tests pass,
  and it is a usable bisect point. Verify that — clone to a temp dir,
  checkout the commit, run the tests — rather than assuming it. Prefer
  splitting engine fixes from manifest work: they are separate bisect
  points and separate reasons.

  Commit messages: what changed and WHY. State the bug a fix fixes and
  how it manifested. Say what you verified AND WHAT YOU COULD NOT —
  every commit here has a "not verified" paragraph where one is honest.
  Never claim a test passed without running it.

  Work on a branch. Do not push or open a PR unless I ask.

  Run ./tests/run.sh before every commit. It runs shellcheck, go vet,
  go test, and gofmt.

  WHEN YOU FIX SOMETHING A TEST DID NOT CATCH, DISABLE THE FIX AND
  CONFIRM YOUR NEW TEST FAILS. A test that passes for the wrong reason
  is worse than no test, and this project has shipped one before — a
  runner-level secret test that asserted "no leak" over an empty capture
  because the probe was refused before it ever ran.

  CHECKING EXIT CODES: redirect to a file, never pipe to tail or grep.
  `cmd | tail` reports tail's exit code, and that has already produced
  two false "verified" claims in this project. Beware `a && b` chains for
  the same reason — if the build step fails on something incidental, the
  tests never run and the shell reports the wrong thing.

  VERIFYING A COMMIT IN A FRESH CLONE: `mise which go` FAILS inside a
  clone, because the untrusted mise.toml blocks it. tests/run.sh then
  SKIPS every Go test and still exits 0 — which looks like a pass and
  verified nothing. This has already produced a false "verified" claim
  in this project. Resolve Go from the parent repo and pass it in:
    GO_DIR=$(dirname "$(mise which go)")   # from the real repo
    cd /tmp/clone && PATH="$GO_DIR:$PATH" ./tests/run.sh
  Then confirm the output actually says "go: vet and test clean" rather
  than "go: no toolchain — skipped".

  WRITE TEST OUTPUT OUTSIDE THE REPO UNDER TEST. Use -out, -status, and
  -raw to put evidence somewhere else entirely. A fixture whose
  status.md lands inside the repo being measured makes the working-tree
  check fire on the test's own artefacts, and it looks exactly like a
  false positive in the code.

  NORMALISE THE ENVIRONMENT IN SHELL TESTS. A test that inherits PATH
  takes whichever branch the developer's dotfiles produce — one machine
  exits 1, another exits 2, and the assertion that matters never runs.

  Stop and ask when a decision contradicts ARCHITECTURE.md, when exit
  criteria are ambiguous, or when a design choice is load-bearing and
  not already settled. Do not silently pick. ASK FOR REPOS rather than
  inventing fixtures when a check judges repo shape — that is how M7's
  Layers 4 and 5 were built, and it is why they were right.

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
  - Every fact carries provenance. Every NO-GO carries a remedy, and
    that remedy interpolates §5's tokens rather than printing them.
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

  M7 PROVED THE THESIS on both axes: it found three node installs and a
  node that vanishes in a clean shell on a machine whose owner did not
  know either, and it correctly declined to say anything about node on a
  Rust repo. M8 is breadth, and breadth is where precision usually goes
  to die. Precision over coverage: every reported NO-GO must be real, and
  every false positive is a bug, not a rough edge. A check you cannot
  test against something real belongs in docs/COVERAGE.md, not in the
  manifest.

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

**M7's intervention point is done, and it worked.** Layers 4 and 5 were built
against three real repos supplied at the time — one with a violated `.nvmrc`
pin, one npm monorepo, one Rust repo that must resolve everything to N/A. The
Rust repo was the most valuable of the three, because the interesting question
for a repo-shaped check is not "does it find the problem" but "does it stay
quiet where there is no problem."

Four defects came out of it, and **two were in the verdict engine rather than
in the checks** — reachable only by reading a rendered glance and a dumped
`evidence.json` against known ground truth. The suite was green throughout.

**M8's intervention point is `docs/COVERAGE.md`.** Read it when it exists and
ask whether each stated gap is one you accept. That file is the only place a
deliberate omission becomes visible; without it, "we decided not to check that"
and "we forgot" look identical from outside.

**Known gaps deliberately left for their milestone:**

| Gap | Belongs to |
|---|---|
| `.ferret/` un-ignore for `requirements.md` | M10, which creates it |
| Manifest schema has no fields for preferred tool / fallbacks / install gating | M10.5 |
| Container run with no Go toolchain | M6's remainder |
| `docs/COVERAGE.md` | M8's exit criterion; does not exist yet |
| Submodules and LFS git checks | M8; deferred rather than written blind |
| Layer 1 case sensitivity | M8; needs a case-sensitive volume to mean anything |
| Four Layer 5 branches never exercised | M7 met the letter, not the spirit |

**Four traps this project has already fallen into.** Each produced a
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
- **A correct check can still give broken advice.** Every remedy printed its
  `$declared` token literally for two milestones. The checks were right, the
  tests passed, and the user got a command that could not work.

**And one about the tool's own findings:** it reports a dirty working tree,
which will usually be *your own uncommitted work* mid-session. That is a true
finding, not a bug — but when testing, write outputs outside the repo being
measured with `-out`/`-status`/`-raw`, or the check fires on your own artefacts
and looks like a false positive.

**Where to push back.** If a milestone's exit criteria turn out to be wrong
once there is real code, the plan should change — a stale plan is worse than no
plan. That is explicitly allowed, and it is better than quietly building
something that does not match. This document has itself been rewritten four
times for exactly that reason, and M3's last box is the sharpest example: the
criterion as written was already met, and the real gap was somewhere else
entirely. Re-derive the gap from the code before trusting a checkbox that
describes it.
