# Repo conventions

Start with `docs/plan/PROMPT.md`. It is the standing instruction for work in this repo.

Reading order: `README.md` → `docs/design/ARCHITECTURE.md` → `docs/design/DOCS_MODEL.md` →
`docs/plan/PLAN.md`. `docs/design/FRAMEWORK.md` is rationale for coverage decisions, not a
spec — never execute from it.

## Hard rules

- Python 3.8+ standard library only. No third-party packages, ever.
- Probes are POSIX sh strings in the manifest. Python is orchestration only.
- `runner.py` never interprets. If it imports from `verdict.py`, the design is broken.
- No code path converts UNKNOWN into GO.
- Phase 1 is read-only. No mutation anywhere before a baseline is written.
- Nothing installs before the proposed list is approved. Preferred tool and
  ordered fallbacks are shown *before* approval, never discovered at execution
  time. A do-not-use entry keeps its tool off the list entirely.
- Redaction is allowlist, at capture time, in the runner.
- Every fact carries provenance. Every NO-GO carries a remedy.

## Machine state — read it, don't guess it

`bootstrap/default-script.sh` and `bootstrap/default-results.md` are gitignored:
they describe one machine and never travel. The committed reference is
`scripts/default-script.sh.example`.

When asked what is installed, what shells exist, whether there is network, or
what tooling is available here:

1. Read `bootstrap/default-results.md` if it exists. It is the measured answer.
2. If it does not exist, generate it — run `./bootstrap/run.sh`, which seeds the
   script from the example and executes it. Do not answer from assumption, and
   do not probe ad hoc when a script exists to do it reproducibly.
3. Refer to `scripts/default-script.sh.example` when asked how a fact was
   obtained, or when extending what gets measured.

Stale results are UNKNOWN, never GO. Regenerate rather than patch.

## Working agreement

Identify the current milestone from `docs/plan/PLAN.md` checkboxes and repo state,
not from conversation. One milestone at a time; meet its exit criteria before
moving on. Update the checkboxes as you go, and edit the plan when reality
diverges from it.
