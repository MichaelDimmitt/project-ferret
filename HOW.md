# How to run things

What to run, when, and in what order. For *why* it's built this way, see
`docs/design/`. For what's built so far, see `docs/plan/PLAN.md`.

> **Today:** only stage zero does real work. `./scripts/sweep.sh` is a stub
> until M1. Everything below marked *(not yet)* is described so the order is
> clear, not because you can run it now.

---

## The short version

```sh
./bootstrap/run.sh      # 1. measure this machine      — works now
./scripts/sweep.sh      # 2. check this repo           — stub until M1
```

Stage zero first, always. Everything after it reads what it measured.

---

## 1. `./bootstrap/run.sh` — measure the machine

**Run it:** first, on a machine you haven't measured yet. Again whenever the
machine changes — new tool installed, different container, moved to another
box. Cheap to re-run.

```sh
./bootstrap/run.sh
```

**What it does:** detects your OS, runs every probe appropriate to it, and
writes `bootstrap/default-results.md` — a table of what's installed, what
capabilities exist (network, DNS, sudo, writable paths, clock), and what the
environment looks like. Every row carries the command that produced it, so you
can check any line you don't believe.

**Why it's shell and not Go:** it runs *before* mise installs the toolchain, so
writing it in Go would need the thing it's there to find. It needs nothing but POSIX `sh`
and coreutils, so it runs on a box with no network, no agent, and no
interpreter.

**It installs nothing.** It only measures. A missing tool is a finding, not an
error — the script exits 0 even when probes fail.

**Read the output:**

```sh
cat bootstrap/default-results.md
```

That file describes one machine and is gitignored — it never travels.

### Sharing it — use `--redact`

**`bootstrap/default-results.md` is not safe to paste publicly.** It contains
your username, home-directory paths, exact OS build, hardware, and your full
PATH — which inventories the tooling you have installed. Together that
identifies you and tells a reader which exploits would land on your machine.
The file says so in its own header.

There are two levels, because "safe to share" depends on who you're sharing
with. Both write `bootstrap/default-results.redacted.md` and **never
overwrite** the full results — you keep both.

```sh
./bootstrap/run.sh --redact             # identity
./bootstrap/run.sh --redact=paranoid    # identity + fingerprint + posture
```

**`--redact` (identity)** — for an issue tracker, a colleague, or an AI.
Removes *who you are*, keeps everything about the machine, so a reader can
still tell you your Docker is four years stale.

| Removed | Becomes |
|---|---|
| Username | `<user>` |
| Home directory paths | `~/...` |
| Repo path | `<repo>` |
| TMPDIR (a stable per-user identifier) | `<tmpdir>` |
| PATH contents | a count: `40 entries (30 unique)` |
| uid | `non-root` or `0 (root)` |

**`--redact=paranoid`** — for somewhere public and indexed. Adds everything
above, plus the machine fingerprint and security posture that together turn a
help request into a targeting packet.

| Also removed | Becomes |
|---|---|
| Exact OS build (`26.5.2` / `25F84`) | `26.x`, build `<redacted>` |
| CPU / hardware | `<redacted>` |
| Kernel revision | `<redacted>` |
| SIP, sudo state | `<redacted>` |
| Locale | `<redacted>` |
| Absolute clocks | `clock-skew: under 5s` |
| Xcode path | `present (full Xcode)` |

**Both levels keep every tool version.** Those are the file's reason to exist —
strip them and a reader can't help. Level 2 removes the *combination* that
targets an exploit (exact build + hardware + what defenses are on), not the
signal.

Two deliberate choices worth knowing:

- **Stripped rows still appear**, as `<redacted>`. A silently missing row is
  indistinguishable from a probe that failed, and this tool's whole premise is
  that absence and unknown are different results.
- **Clock skew survives level 2.** Skew is a real finding that taints other
  checks; only the absolute timestamps (which place you in a timezone) go.

Redaction happens in `cell()`, so every value is filtered on the way into the
table and a new probe cannot forget to apply it. `./tests/run.sh` asserts all
of the above — including a control that fails if redaction silently becomes a
no-op, and a check that level 2 hasn't destroyed the version strings.

> Still one machine's state. Share it deliberately; it's gitignored too.

### Using redaction from another script

The substitution logic lives in `scripts/lib/redact.sh`, standalone. Any future
script can source it without pulling in the probe machinery:

```sh
. "$root/scripts/lib/redact.sh"

safe=$(redact "$value")     # level from $FERRET_REDACT; 0 returns input as-is
```

It needs `FERRET_REDACT` (0/1/2) and, optionally, `FERRET_REPO` for `<repo>`
substitution — the repo root isn't derivable inside the library, since the
working directory may be anywhere. `HOME`, `TMPDIR`, and the username come from
the environment.

**Apply it at capture time, to each value — not to finished output.** Running a
completed document through a substitution pass is a *denylist*: it strips the
patterns someone remembered to enumerate and silently passes an API key in a
stack trace. Filtering each value as it is recorded is the allowlist discipline
ARCHITECTURE.md requires of the runner, and it's why `cell()` is the only path
into the table.

Level 2's extra stripping is deliberately *not* in the library. Removing
posture and fingerprint is a per-fact decision, not a string substitution — see
`row_posture()` in `scripts/os/common.sh` for how the probe scripts do it.

`./tests/redact-lib-test.sh` asserts the library stays standalone, and fails if
anything couples it back to `common.sh`.

### First run seeds a copy you own

On first run it copies `scripts/default-script.sh.example` to
`bootstrap/default-script.sh` and runs that. The copy is gitignored and yours
to edit; later runs use it as-is and never overwrite your changes.

To start over from the reference:

```sh
rm bootstrap/default-script.sh && ./bootstrap/run.sh
```

Run `./bootstrap/run.sh`, not the script in `scripts/` directly — the entry
point owns the seeding and the output path.

---

## 2. `./scripts/sweep.sh` — check the repo *(stub until M1)*

**Run it:** after stage zero, from inside the repo you want checked.

```sh
./scripts/sweep.sh
```

**What it will do:** run the manifest checks, apply the verdict engine, and
write `status.md` — ordered by severity, readable in ten seconds.

**Today** it prints `not implemented` and exits 3. That is deliberate: exit 0
means GO, and a sweep that verified nothing has not earned it.

**If there's no Go toolchain** it says so and routes to the reduced-mode
bootstrap checks (M6). It does not crash, and it never reports GO on checks it
couldn't run. mise supplies the toolchain (`mise.toml` pins the version), but
Ferret never installs mise on its own — it's proposed for approval like any
other tool.

---

## The scripts, and which you actually invoke

You run the two entry points. The rest are called for you.

| Path | You run it? | What it is |
|---|---|---|
| `bootstrap/run.sh` | **Yes** | Stage-zero entry point. Seeds, dispatches, writes results. |
| `scripts/sweep.sh` | **Yes** | Stage-one entry point. Builds/runs the Go binary, or falls back. |
| `mise.toml` | No | Pins the Go version. `mise install` provides the toolchain. |
| `scripts/default-script.sh.example` | No | Reference probe script. Read it to see how a fact is measured. |
| `scripts/lib/redact.sh` | No | Redaction, standalone. Source it from any script. |
| `scripts/os/common.sh` | No | Portable probe core. Sourced, never run alone. |
| `scripts/os/darwin.sh` | No | macOS probes. Sourced by the dispatcher. |
| `scripts/os/linux.sh` | No | Linux probes. Sourced by the dispatcher. |
| `bootstrap/default-script.sh` | No | Your seeded copy (gitignored). Run via `run.sh`. |
| `tests/run.sh` | **Yes** | Runs every test. One command. |
| `tests/redaction-test.sh` | No | Asserts `--redact` leaks nothing. Run via `tests/run.sh`. |
| `tests/redact-lib-test.sh` | No | Asserts `redact.sh` stays standalone. |

### How the OS dispatch works

`default-script.sh` reads `uname -s` and sources the matching script from
`scripts/os/`, on top of `common.sh`. The probes genuinely differ — `/proc` is
Linux, `sw_vers` and SIP are macOS, `getent` is glibc — so one script per OS
beats one script that's wrong everywhere.

An OS with no script still works: it runs the common core and records
`os-support: unknown (<uname>)`. It does **not** guess at the nearest match.

**To add an OS:** write `scripts/os/<key>.sh` defining `probe_os()` (call
`probe_common` first, then add your own `row` lines), and add the `uname -s`
value to the `case` in `scripts/default-script.sh.example`.

---

## Exit codes

Same meaning everywhere.

| Code | Means |
|---|---|
| 0 | GO — checked, and it passed |
| 1 | NO-GO — a blocker was found |
| 2 | Unknowns present — never collapsed into 0 |
| 3 | Could not determine — no baseline, no interpreter, or not implemented |

`bootstrap/run.sh` is the exception: it exits 0 whenever it produced a results
file, because a failed *probe* is a finding rather than a failed *run*. What it
couldn't measure is recorded as `unknown` or `absent` in the table — those are
different, and the table keeps them apart.

---

## Checking the scripts still work

After editing anything under `scripts/os/`:

```sh
# Runs clean and produces a well-formed table
./bootstrap/run.sh && grep -c '^| ' bootstrap/default-results.md

# No bashisms — must behave identically under a strict POSIX shell
/bin/dash scripts/default-script.sh.example /tmp/dash-check.md

# All tests — required after touching cell(), redact.sh, or adding a probe
./tests/run.sh

# Lint, if you have it
shellcheck -s sh scripts/default-script.sh.example scripts/lib/*.sh \
  scripts/os/*.sh bootstrap/run.sh
```

A row must have exactly 5 columns. Probe output can contain `|` — the `cell()`
helper escapes it, so add rows with `row`, never with a bare `printf`. That
same helper applies redaction, which is the other reason never to bypass it.

---

## Asking an agent what's on this machine

It should read `bootstrap/default-results.md`, or generate it by running
`./bootstrap/run.sh` — not answer from assumption and not probe ad hoc. Stale
results are UNKNOWN, never GO; regenerate rather than patch. See `AGENTS.md`.
