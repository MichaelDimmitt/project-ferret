# How to run it — the short list

- **Run these two, in this order:**
  - `./bootstrap/run.sh` — measures this machine. Works now.
  - `./scripts/sweep.sh` — checks this repo. Stub until M1.

- **`./bootstrap/run.sh`**
  - Run it first, before anything else.
  - Re-run it whenever the machine changes. It's cheap.
  - Writes `bootstrap/default-results.md` — what's installed, what works.
  - Read that file to see what's on the machine.
  - Installs nothing. Only measures.
  - It's shell, not Go, because it runs before mise installs the toolchain.
  - First run copies `scripts/default-script.sh.example` to
    `bootstrap/default-script.sh`. That copy is yours to edit and never
    gets overwritten.
  - To start fresh: `rm bootstrap/default-script.sh && ./bootstrap/run.sh`

- **Before you share the results — redact first**
  - `bootstrap/default-results.md` is **not safe to paste publicly.**
  - It has your username, home paths, OS build, hardware, and full PATH.
  - Two levels, both writing `bootstrap/default-results.redacted.md`:
  - `./bootstrap/run.sh --redact`
    - Removes **who you are**: username, home/repo paths, TMPDIR, PATH, uid.
    - Keeps all machine details, so a reader can still spot a stale tool.
    - Good for: an issue tracker, a colleague, an AI.
  - `./bootstrap/run.sh --redact=paranoid`
    - Also removes **what the machine is**: exact OS build, CPU, kernel,
      SIP/sudo state, locale, clocks.
    - Still keeps every tool version — that's the point of the file.
    - Good for: anywhere public and indexed.
  - Neither overwrites your full results — you keep both.
  - All three files are gitignored.

- **`./scripts/sweep.sh`**
  - Run it second, from inside the repo you want checked.
  - Today it prints `not implemented` and exits 3. That's expected.
  - Later it writes `status.md` — what's broken, worst first.
  - If there is no Go toolchain it says so instead of crashing.

- **Don't run these directly — they're called for you:**
  - `scripts/default-script.sh.example` — the reference probe script
  - `scripts/lib/redact.sh` — redaction, sourceable on its own
  - `scripts/os/common.sh` — shared probes
  - `scripts/os/darwin.sh` — macOS probes
  - `scripts/os/linux.sh` — Linux probes
  - `bootstrap/default-script.sh` — your seeded copy

- **Redacting from your own script:**
  - `. scripts/lib/redact.sh` then `safe=$(redact "$value")`
  - Set `FERRET_REDACT` to 0, 1, or 2. Set `FERRET_REPO` for `<repo>`.
  - Level 0 returns the input unchanged, so it's safe to always call.
  - Do it per-value as you record it, **not** to a finished file — filtering
    finished output only catches patterns you thought to list.

- **Exit codes:**
  - `0` — passed
  - `1` — blocker found
  - `2` — unknowns present
  - `3` — couldn't determine (no runner, no baseline, not implemented)
  - Except `bootstrap/run.sh`, which exits `0` if it wrote results at all.

- **After editing anything in `scripts/os/`:**
  - `./bootstrap/run.sh` — still runs?
  - `/bin/dash scripts/default-script.sh.example /tmp/check.md` — no bashisms?
  - `./tests/run.sh` — all tests pass and shellcheck is clean?
  - Add rows with `row`, never a bare `printf` — it escapes `|` and redacts.

- **Adding a new OS:**
  - Write `scripts/os/<name>.sh` defining `probe_os()`.
  - Call `probe_common` first, then add your own `row` lines.
  - Add the `uname -s` value to the `case` in `scripts/default-script.sh.example`.

- **More detail:** `HOW.md`. Why it's built this way: `docs/design/`.
