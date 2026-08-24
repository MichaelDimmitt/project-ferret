# Repo conventions

Start with `docs/plan/PROMPT.md`. It is the standing instruction for work in this repo.
`HOW.md` says what to run and in what order.
`docs/plan/CONTINUE.md` is the re-pasteable prompt for resuming the build.

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
- **Never read a secret.** See below — this binds probes, scripts, and you.

## Never read a secret

**Applies to every milestone, M7 included, and to you directly — not only to
the code you write.**

Ferret determines that a secret *exists* and what *shape* it has. It never
learns its value. Not to hash it, not to redact it, not to check it, not in
passing. A value that is never read cannot leak from a log, a crash dump, a
scrollback buffer, a debug print added at 2am, or a transcript.

This is stronger than redaction, and it replaces redaction as the primary
guarantee for secrets. Redaction says *we didn't keep it*. This says **we
never saw it** — the only version of the claim that survives someone adding a
`fmt.Println` while debugging.

### The rule

1. **Never open a file suspected of holding a secret.** Not with `cat`, `head`,
   `sed`, `grep -o`, Read, or anything else. Suspicion is enough — you do not
   confirm by looking.
2. **Never pull a secret value into a process.** `printenv GH_TOKEN` puts it in
   the probe's stdout, which is reading it. Test whether it is *set* instead.
3. **Mark the file, don't inspect it.** A suspected secret-bearing path is
   recorded as marked, with what was inferred and how. That marking is itself
   a finding and belongs in the report.
4. **You may write scripts that operate on secrets** — a script may pass
   `~/.npmrc` to `npm`, or export a var into a child process. What it must not
   do is *surface the value*: no echoing it, no writing it to a file you then
   read, no capturing it into a variable you inspect.
5. **Never write a secret.** Not into a fixture, a doc, a commit message, a
   test, or a scratch file. A realistic-looking fake is still a string someone
   will grep for and mistake for real.
6. **When in doubt, mark and move on.** UNKNOWN(unverifiable) with a stated
   reason beats a value you should not have.

### What this permits

```sh
test -f ~/.npmrc                       # existence
stat -f '%Sp %z' ~/.npmrc              # permissions, size
grep -c '_authToken' ~/.npmrc          # is the KEY present — count only
grep -o '^registry=.*' ~/.npmrc        # a non-secret line, named explicitly
printenv GH_TOKEN >/dev/null; echo $?  # set or unset, never the value
git remote -v | sed 's|://[^@]*@|://<redacted>@|'   # strip before capture
```

### What it forbids

```sh
cat ~/.npmrc                     # opens a file known to hold a token
printenv GH_TOKEN                # value into stdout
echo "$GH_TOKEN" | wc -c         # length is still a read
grep '_authToken=.*' ~/.npmrc    # the match includes the value
```

### The cost, stated plainly

Some checks become impossible, and that is accepted rather than worked around.
"Is this token valid" and "does the auth line point at the right registry"
cannot be answered without reading. They resolve **UNKNOWN(unverifiable)** with
a cross-reference — which is exactly the state ARCHITECTURE.md §5 already
defines for things that cannot be read by design, and the same move Layer 7
makes for secret *names* versus secret *values*.

A check that cannot be performed honestly is not performed. That is the whole
posture of this tool.

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
