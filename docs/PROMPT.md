# Prompt for Claude Code

Paste this to start a session. It is written to be re-pasteable — starting a fresh session mid-project should work, because step 1 re-derives position from the repo rather than from conversation history.

---

## The prompt

```
You are building Ferret Sniffer, a status-check tool for the machine it runs on.

Read these before writing code:
  README.md               — what the tool is and what it refuses to do
  docs/ARCHITECTURE.md    — design decisions and their reasons
  docs/DOCS_MODEL.md      — the three documents, volatility, provenance, baseline rule
  docs/PLAN.md            — milestones in dependency order
  docs/FRAMEWORK.md       — the evaluation theory the checks derive from

FRAMEWORK.md is rationale, not a spec. It explains why a check matters.
The manifest states how to probe and what passes. Never execute from prose.

WORKING AGREEMENT

1. Identify the current milestone from PLAN.md checkboxes and the repo state,
   not from anything I say. Restate it in one line and start there.
2. One milestone at a time. Meet its exit criteria before proposing the next.
3. Stop and ask when a decision would contradict ARCHITECTURE.md, when exit
   criteria are ambiguous, or when a design choice is load-bearing and not
   already settled. Do not silently pick.
4. Update PLAN.md checkboxes as you go. If reality diverges from the plan,
   edit the plan and say why — a stale plan is worse than no plan.
5. Prefer boring, dependency-free, obvious code. This tool runs on broken
   machines. Cleverness here costs more than it saves.

INVARIANTS — violating any of these is a failed milestone, not a tradeoff

  - No dependencies outside the Python 3.8+ standard library. Not one.
  - The runner never interprets. It records raw stdout, stderr, exit code,
    duration. If runner.py imports from verdict.py, the design is broken.
  - No code path converts UNKNOWN into GO. Assert this in a test.
  - Nothing mutates in phase 1 (observe): no install, fetch, pull, clone,
    config write, or service start. Enforced by denylist AND by a test
    asserting the fixture repo's working tree, index, and config are
    byte-identical after a sweep.
  - No mutation anywhere before a clean baseline is written. Ferret exits 3
    rather than remediate without one. Remediation is opt-in (--remediate).
  - Every fact carries provenance: observed / ferret-installed /
    user-remediated / inferred. Untagged facts make Ferret report its own
    footprint as a finding.
  - Failed remediation is never fatal. No sudo, no egress, immutable FS —
    each is a finding. Record it, mark affected checks reduced-mode, and
    emit the diagnosis already collected.
  - requirements.md is committed, so no machine-specific value — path, port,
    hostname, username — may appear in it. Asserted in a test.
  - Redaction is allowlist at capture time, in the runner, never in the
    renderer. Unallowlisted values are recorded as presence and shape only.
  - .ferret/ is gitignored in the same commit that first creates it.
  - Every check that can be NO-GO carries a remedy.
  - Exit code 2 (unknowns present) is never collapsed into 0.

WHAT GOOD LOOKS LIKE

The tool answers one question: can I proceed on this machine right now, and
if not, what is broken and what do I do about it?

Then it helps get there, and writes down what it learned — so the next
project on this machine starts from what's already known instead of
rediscovering the box.

It is read in ten seconds. It is ordered by severity, not by layer. It never
implies it verified something it could not check.

A false positive is worse than a missing check. A tool that cries wolf gets
ignored, and an ignored tool has failed completely rather than partially.
Once document emission lands, a false positive gets written down and
outlives the run that produced it.

START

State the current milestone. Then begin.
```

---

## Notes on using this

**Keep it re-pasteable.** Point at documents rather than restating them, so the prompt doesn't drift out of sync with the design. Fix ARCHITECTURE.md; leave the prompt alone.

**Milestone-derived, not conversational.** Asking Claude Code to determine position from checkboxes and repo state means a fresh session resumes correctly. Telling it "we're on M3" in chat means a new session doesn't know that.

**The invariants are phrased as failures, not preferences.** "Prefer no dependencies" gets traded away under pressure. "Not one" does not. Everything on that list is something the design collapses without.

**Where to intervene:**

- **After M1**, read `docs/MANIFEST_SCHEMA.md` yourself. It's the contract; every later milestone is expensive to change against a wrong schema.
- **After M4**, run it on your own machine and actually read the output. If it doesn't tell you something true in ten seconds, the format is wrong and now is the cheap time to say so.
- **During M7**, every false positive is a bug report. Precision over coverage.

**A useful follow-up prompt mid-project:**

```
Before continuing: run the sweep on this machine and paste the status.md.
Then tell me which lines you would delete if you had to cut it in half.
```

That surfaces whether the severity ordering is doing real work or whether everything has drifted to "blocker."
