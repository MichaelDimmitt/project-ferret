# Repo conventions

Start with `docs/PROMPT.md`. It is the standing instruction for work in this repo.

Reading order: `README.md` → `docs/ARCHITECTURE.md` → `docs/DOCS_MODEL.md` →
`docs/PLAN.md`. `docs/FRAMEWORK.md` is rationale for coverage decisions, not a
spec — never execute from it.

## Hard rules

- Python 3.8+ standard library only. No third-party packages, ever.
- Probes are POSIX sh strings in the manifest. Python is orchestration only.
- `runner.py` never interprets. If it imports from `verdict.py`, the design is broken.
- No code path converts UNKNOWN into GO.
- Phase 1 is read-only. No mutation anywhere before a baseline is written.
- Redaction is allowlist, at capture time, in the runner.
- Every fact carries provenance. Every NO-GO carries a remedy.

## Working agreement

Identify the current milestone from `docs/PLAN.md` checkboxes and repo state,
not from conversation. One milestone at a time; meet its exit criteria before
moving on. Update the checkboxes as you go, and edit the plan when reality
diverges from it.
