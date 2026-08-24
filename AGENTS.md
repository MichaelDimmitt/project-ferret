# Repo conventions

Start with `docs/plan/PROMPT.md`. It is the standing instruction for work in this repo.
`HOW.md` says what to run and in what order.

Reading order: `README.md` → `docs/design/ARCHITECTURE.md` → `docs/design/DOCS_MODEL.md` →
`docs/plan/PLAN.md`. `docs/design/FRAMEWORK.md` is rationale for coverage decisions, not a
spec — never execute from it.

## Hard rules

- The runner is Go, standard library only. No third-party modules, ever.
  See `docs/design/LANGUAGE_CHOICE.md` for why, and for the conditions that
  would reopen it.
- mise supplies the runtime, after the user approves it. It is proposed like
  any other tool — never silently installed. `mise.toml` pins the Go version.
- Stage zero is shell, and stays shell. It runs before mise exists, so it
  cannot be written in a runtime it is measuring. Preliminary scripts are
  per-OS (`scripts/os/<uname>.sh`), dispatched by `bootstrap/run.sh`.
- The Tier-1 fallback (`ferret/bootstrap.sh`) is POSIX sh. It runs when mise
  was declined or could not install, and its verdict is UNKNOWN, never GO.
- Probes are POSIX sh strings in the manifest. Go is orchestration only.
- `runner.go` never interprets. If it imports the verdict package, the design
  is broken.
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
