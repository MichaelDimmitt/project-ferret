# Manifest Schema

The contract between the manifest, the runner, and the verdict engine. Every
later milestone is written against this file, so it is specified before any
runner code exists.

Normative: this document and `manifest/_schema.json` together. Where they
disagree, that is a bug — the JSON Schema is mechanical validation, this is the
meaning. `docs/design/FRAMEWORK.md` is rationale, never a spec.

---

## 1. What a manifest file is

`manifest/NN-name.json` — one file per layer, numbered by
`docs/design/FRAMEWORK.md`'s layers. A file is a JSON object with two keys:

```json
{
  "layer": 4,
  "checks": [ { "id": "...", "...": "..." } ]
}
```

| Key | Type | Required | Meaning |
|---|---|---|---|
| `layer` | integer 0–10 | yes | The layer every check in this file belongs to. |
| `checks` | array of check objects | yes | May be empty; an empty layer file is legal and means "nothing here yet". |

No other top-level keys. Unknown keys are a validation error, not a warning —
see §8.

**Validation runs before any probe executes.** A manifest that fails validation
exits 3. A malformed manifest is Ferret being broken, not a finding about the
machine, and running half of a broken manifest produces a report that looks
complete and is not.

---

## 2. The check object

Every field, in one table. Details and worked examples follow.

| Field | Type | Required | Default | Read by |
|---|---|---|---|---|
| `id` | string | yes | — | both |
| `layer` | integer 0–10 | no | file's `layer` | verdict |
| `title` | string | yes | — | verdict |
| `applies_if` | string (sh) | no | always applies | runner |
| `probe` | string \| array of strings | yes | — | runner |
| `declared` | string \| array of strings | no | absent | runner |
| `expect` | object | yes | — | **verdict only** |
| `severity` | `blocker` \| `warning` \| `info` | yes | — | verdict |
| `decisive` | boolean | no | `true` if `blocker`, else `false` | verdict |
| `tainted_by` | array of check ids | no | `[]` | verdict |
| `remedy` | string | conditional | — | verdict |
| `timeout_s` | number > 0 | no | `10` | runner |
| `redact` | boolean \| string | no | `false` | runner |
| `mutating` | `false` | no | absent | runner |
| `mutating_why` | string | conditional | — | runner |
| `volatility` | see §7 | no | `session` | verdict / M10 |
| `notes` | string | no | — | nobody |

`notes` exists because JSON has no comments and a probe that needs explaining
should carry the explanation. Nothing reads it.

### Who reads what, and why it matters

The `Read by` column is the design, not documentation. **The runner never reads
`expect`, `severity`, `tainted_by`, or `remedy`.** It cannot: knowing what
counts as passing is exactly the interpretation it is forbidden to do. It reads
the fields needed to *execute* — `applies_if`, `probe`, `declared`,
`timeout_s`, `redact`, `mutating` — and copies `id` through to the evidence.

`evidence.json` therefore contains no verdicts, and re-running the verdict
engine against yesterday's evidence with an edited `expect` is a supported
workflow rather than an accident.

---

## 3. Field specifications

### `id` — required, string

Dot-separated lowercase segments: `^[a-z0-9]+(\.[a-z0-9_]+)*$`.

By convention `<layer-topic>.<subject>.<property>` — `runtime.node.version`,
`machine.clock.skew`, `git.repo.dirty`. Unique across the whole manifest, not
per file; `tainted_by` references are global.

The id is the join key across `evidence.json`, `status.md`, and the taint DAG.
Renaming one silently breaks any `tainted_by` that points at it, which is why
§8 makes a dangling reference a hard error.

### `layer` — optional, integer 0–10

Almost always omitted; it comes from the file. Present only when a check
genuinely belongs to a different layer than the file it is stored in, which
should be rare enough to be worth a `notes` explaining it.

### `title` — required, string

One line, rendered verbatim in the glance. Write the *subject*, not the
outcome: `Node version matches pin`, not `Node version is wrong`. The state
supplies the outcome, and a title phrased as a failure reads absurd next to a
green check.

Keep it under ~50 characters. The glance is a column.

### `applies_if` — optional, POSIX sh

A shell command. Exit 0 means the check applies; any non-zero means it does
not, and the check resolves **N/A** without the probe ever running.

```json
"applies_if": "test -f package.json"
```

N/A is not a failure and is not UNKNOWN. It means the question is not asked
here — no `gh` checks on a GitLab repo. It is collapsed to a count in the
glance.

`applies_if` is subject to the same mutation denylist and the same timeout as
`probe`. If `applies_if` itself times out or errors in a way that is not a
clean non-zero exit, the check is **UNKNOWN(probe_error)**, never N/A. Silently
converting a broken gate into "doesn't apply here" is the false-green failure
mode wearing a different hat.

### `probe` — required, string or array of strings

The command whose output is the *actual* state. Executed with `sh -c`.

An array is joined with `\n` and passed as a single script, so a long probe
stays readable in JSON:

```json
"probe": [
  "node -v 2>/dev/null || exit 1",
  "# trailing newline is stripped by the runner"
]
```

The runner records stdout, stderr, exit code, and duration. It does not
interpret any of them. A non-zero exit is not a failure — `expect` decides what
the exit code means, and several checks legitimately expect one.

### `declared` — optional, string or array of strings

The command whose output is the *expected* state, read from the repo. Same
execution rules as `probe`.

This field is separate rather than being folded into `expect` because nearly
every check in Ferret is declared-vs-actual, and making that structural means
the comparison lives once in the verdict engine instead of being re-implemented
per check in shell.

```json
"declared": "cat .nvmrc 2>/dev/null || jq -r '.engines.node // empty' package.json"
```

If `declared` is absent, `$declared` is unavailable to `expect` and referencing
it is a validation error (§8).

### `expect` — required, object

**Read only by the verdict engine.** Full type list in §4.

### `severity` — required, enum

| Value | Meaning |
|---|---|
| `blocker` | NO-GO here means the repo cannot proceed. Drives exit code 1. |
| `warning` | Real, worth fixing, does not stop the work. |
| `info` | Context. Almost always `decisive: false`. |

Severity describes the *failure*, not the check. A check that is GO has no
severity in the output.

### `decisive` — optional, boolean

Whether this check participates in the top-line verdict.

Default: `true` when `severity` is `blocker`, `false` otherwise. State it
explicitly when overriding, and prefer explicit on anything non-obvious.

Non-decisive checks are captured in full and collapsed to a count in the
glance. A non-decisive UNKNOWN does **not** produce exit code 2 — that is the
point of the field. A decisive UNKNOWN always does.

### `tainted_by` — optional, array of check ids

Prerequisites. If any listed check resolves NO-GO, this check becomes
**UNKNOWN(tainted)** regardless of what its own probe returned.

Edges point *upward*, declared on the dependent. Adding a check therefore never
requires editing an unrelated one, and a prerequisite has no idea who depends
on it.

```json
"tainted_by": ["machine.clock.skew", "shell.path.node_shadowing"]
```

The probe still ran and its raw output is still in `evidence.json`. Taint is a
judgment about trustworthiness, not a reason to skip collection — which is what
lets you fix the clock and re-run the verdict engine without re-probing.

Every id must exist (§8). Cycles are a manifest authoring error and exit 3
naming both ends.

### `remedy` — string, required when the check can be NO-GO

Required unless `severity` is `info`. A check that reports breakage without an
action is half a tool.

One command where possible, `$declared` and `$probe` interpolated as in §5:

```json
"remedy": "nvm use $declared   # or: mise install"
```

A remedy that cannot be a command is prose stating what a human must do. A
wrong remedy is worse than none — see PLAN.md's failure modes.

### `timeout_s` — optional, number > 0

Wall-clock ceiling per command. `applies_if`, `probe`, and `declared` each get
the full budget separately; it is not a total for the check.

Default 10. Exceeding it kills the process group and records
**UNKNOWN(timeout)** with whatever partial output was captured. Anything
touching the network should set this deliberately — the whole point of a
status tool is that it answers fast on a broken machine, and the machines that
hang are the ones being diagnosed.

### `redact` — optional, boolean or string

Redaction level for this check's captured output, applied **at capture time in
the runner**, before anything is written.

| Value | Meaning |
|---|---|
| `false` (default) | Standard capture. Global redaction still applies. |
| `true` | Same as `"identity"`. |
| `"identity"` | Strip user, home, tmpdir, repo path. Level 1 in `scripts/lib/redact.sh`. |
| `"secret"` | The probe is **asserted not to read a value**. Shaping is a backstop, not the mechanism. |

`"secret"` is a declaration by the check's author: *this probe touches
something secret-adjacent, and it does not read the value.* A probe under this
level must already be written so nothing sensitive reaches stdout — testing
whether a variable is set rather than printing it, counting a key rather than
matching its value.

The shaping in the runner (`<set, 40 chars, ghp_…>`) still runs, but it is a
**backstop against an authoring mistake, not the guarantee**. The guarantee is
that the value was never read. See AGENTS.md § *Never read a secret*, which
binds probes, scripts, and agents alike.

The distinction matters because redaction can only promise *we did not keep
it* — the value still transited the process, the pipe, and possibly a debug
print. Not reading promises **we never saw it**, which is the version that
survives someone adding a `fmt.Println` while chasing a bug.

The levels must agree with `scripts/lib/redact.sh`, which is the shell
precedent and already tested. `false`/`"identity"` map to its levels 0 and 1.
`"secret"` has no shell equivalent because stage zero never captures secrets.

Redaction being a runner field rather than a renderer concern is deliberate:
a value that never enters `evidence.json` cannot leak out of it. Blocklisting
at render time fails the moment someone invents a new secret-shaped variable,
and fails silently.

### `mutating` — optional, literal `false`

The escape hatch for the mutation denylist (§6). Only the value `false` is
legal; there is no `"mutating": true`, because a check that admits to mutating
is a check that does not belong in phase 1.

Requires `mutating_why` (§8) — a justification, in the file, next to the
override. An override without a stated reason is how a denylist rots.

### `isolate` — optional, literal `true`

Runs the check's commands under a cleared environment — `env -i` in spirit,
implemented as an explicitly constructed environment rather than an inherited
one. Only `true` is legal: `"isolate": false` is the default and states
nothing, so writing it is noise.

The environment the check receives is `PATH` (set to a fixed, conservative
value), `HOME`, and nothing else. Not `NODE_OPTIONS`, not `NVM_DIR`, not
`http_proxy`, not the forty other variables a login shell exports.

This exists because of a specific false negative. A developer's `.zshrc` puts
a tool on `PATH`, the probe finds it, Ferret reports GO — and CI, which never
sources `.zshrc`, cannot find it at all. The check measured the developer's
shell rather than the environment the build will actually run in, and reported
a green that does not survive contact with the build machine.

Isolation is opt-in rather than the default because most checks legitimately
want the real environment: "is the proxy configured *here*" is a question
about this shell, and clearing it would answer a different question. Use
`isolate` for checks whose answer must hold in a fresh shell — tool presence,
version resolution, `PATH` shadowing.

`HOME` survives because too much breaks without it (`git` finds no config,
version managers find no installs) and it is not the variable that causes the
false negative. `PATH` is set rather than cleared, because a probe with no
`PATH` cannot run `sh` builtins' external counterparts and would fail as
`tool_absent` regardless of what is installed — which is the same false
answer in the other direction.

Contract: `docs/design/ARCHITECTURE.md` §5, "fresh-shell isolation".

### `volatility` — optional, enum

`permanent` | `stable` | `session` | `volatile` | `expiring`. Default `session`.

Per `docs/design/DOCS_MODEL.md` §3. The runner ignores this field entirely; it
governs how long a claim derived from this check stays trustworthy in
`system.md`, which is M10's problem. Specified here so M10 does not have to
change the schema after eighty checks exist.

---

## 4. `expect` types

Every type is an object with `type` and its own fields. Values interpolate per
§5.

A predicate that cannot be evaluated — a missing operand, an unparseable
version, a non-numeric input to a numeric comparison — is
**UNKNOWN(probe_error)**, never NO-GO. "The check is broken" and "the machine
is broken" are different findings, and conflating them is the false positive
that gets a tool ignored.

### `equals`

```json
{ "type": "equals", "actual": "$probe", "value": "20.11.0" }
```

Exact string comparison after trimming leading and trailing whitespace. Both
operands are trimmed; internal whitespace is significant.

### `matches`

```json
{ "type": "matches", "actual": "$probe", "pattern": "^v?20\\." }
```

Go `regexp` (RE2) match, unanchored unless the pattern anchors itself. Optional
`"invert": true` inverts the result. An invalid pattern is a validation error
at load (§8), not a runtime UNKNOWN.

### `one_of`

```json
{ "type": "one_of", "actual": "$probe", "values": ["arm64", "aarch64"] }
```

Trimmed exact match against any member. `values` must be non-empty.

### `non_empty`

```json
{ "type": "non_empty", "actual": "$probe" }
```

GO when the trimmed value has length > 0.

### `absent`

```json
{ "type": "absent", "actual": "$probe" }
```

GO when the trimmed value is empty. The inverse of `non_empty`, spelled
separately because `{"invert": true}` on a presence check reads backwards at
the call site and the manifest is meant to be skimmable.

### `numeric_lt` / `numeric_gt`

```json
{ "type": "numeric_lt", "actual": "$probe", "value": 300 }
```

Parses the trimmed actual as a float. Non-numeric input is
**UNKNOWN(probe_error)**, not NO-GO — a probe returning `command not found` on
stdout is Ferret's bug, not a disk-space finding.

Optional `"unit"`, purely for rendering: `"unit": "s"` renders `11.2s`.

### `semver_satisfies`

```json
{ "type": "semver_satisfies", "actual": "$probe", "range": "$declared" }
```

Supported range syntax, deliberately small:

| Form | Meaning |
|---|---|
| `20` / `20.11` / `20.11.0` | Prefix match — `20` accepts any `20.x.y` |
| `^20.11.0` | `>=20.11.0 <21.0.0` |
| `~20.11.0` | `>=20.11.0 <20.12.0` |
| `>=20.11.0` | Also `>`, `<`, `<=`, `=` |
| `A \|\| B` | Either range |
| `*` / empty | Any version |

Leading `v` is stripped from both sides. Prerelease and build metadata are
compared per semver: `20.0.0-rc1 < 20.0.0`.

Anything beyond this table — hyphen ranges, `x` placeholders, complex
compositions — is **UNKNOWN(probe_error)** with the unparsed range in the
reason, never a guess. This is the escape hatch's job, not the parser's.

An empty `range` (a `declared` that found nothing) is
**UNKNOWN(probe_error)**: nothing was declared, so nothing can be satisfied.
That is a different fact from "the version is wrong" and must not render as
NO-GO.

### `exit_code`

```json
{ "type": "exit_code", "of": "probe", "value": 0 }
```

`of` is `"probe"` or `"declared"`. `value` is an integer, or `values` an array
of acceptable integers.

The escape hatch, used sparingly. Every use is a small piece of logic that
moved from the manifest into shell, where it is neither validated nor
re-runnable against old evidence. Prefer a real predicate; when `exit_code` is
the honest answer, say why in `notes`.

---

## 5. Interpolation

Inside `expect` values, `remedy`, and nowhere else:

| Token | Expands to |
|---|---|
| `$probe` | Trimmed stdout of `probe` |
| `$declared` | Trimmed stdout of `declared` |
| `$probe_stderr` / `$declared_stderr` | Trimmed stderr |
| `$probe_exit` / `$declared_exit` | Exit code, as a decimal string |

A literal `$` is `$$`. Unknown tokens are a validation error, not a silent
empty string.

**Interpolation happens in the verdict engine, never in the shell.** The
runner does not expand these; `probe` and `declared` are executed exactly as
written. A `$probe` inside a `probe` string is passed to `sh` untouched and
means whatever the shell says it means — almost certainly a mistake, which §8
warns about.

---

## 6. The mutation denylist

Before executing any `applies_if`, `probe`, or `declared`, the runner matches
the command text against a denylist. On a match without an override, the check
is refused: no execution, result **UNKNOWN(probe_error)** with the matched
token in the reason.

Denied tokens: `install`, `fetch`, `pull`, `clone`, `push`, `commit`,
`checkout`, `prune`, `gc`, `rm`, `mv`, `write`, `set`, `add`, `init`, `reset`,
`stash`, `apply`, `>`, `>>`, `tee`, `sudo`, `chmod`, `chown`, `mkdir`,
`touch`, `ln`.

It over-matches on purpose. `git config --get` contains "config" only by
coincidence and `git rev-parse` is safe despite `parse`; the list is tuned to
catch the dangerous cases and accept false alarms, because the override makes a
false alarm cheap and a miss expensive.

The override:

```json
"probe": "git config --get remote.origin.url",
"mutating": false,
"mutating_why": "config --get reads; only --set/--add/--unset write"
```

A denylist without an escape hatch produces workarounds worse than the rule —
probes rewritten into obfuscated forms that pass the regex. Requiring a stated
reason keeps the override reviewable.

Two things the denylist explicitly does not do. It does not sandbox: a probe
determined to mutate can trivially evade a regex, and the real guarantees are
manifest review at authoring time and the byte-identical fixture test (M5).
And it does not run at manifest-validation time — only immediately before
execution — so a denied check still appears in the evidence with its reason,
rather than the sweep refusing to start.

---

## 7. Evidence output

`evidence.json`, written by the runner. The contract that M2 onward reads.

```json
{
  "schema_version": 1,
  "ferret_version": "0.1.0-dev",
  "started_at": "2026-08-24T14:02:11Z",
  "finished_at": "2026-08-24T14:02:19Z",
  "redact_level": "identity",
  "manifest_files": ["manifest/04-runtime.json"],
  "records": [
    {
      "id": "runtime.node.version",
      "layer": 4,
      "applies": true,
      "applies_if": { "exit": 0, "duration_ms": 4, "timed_out": false },
      "probe": {
        "stdout": "v18.19.0\n",
        "stderr": "",
        "exit": 0,
        "duration_ms": 41,
        "timed_out": false,
        "truncated": false
      },
      "declared": {
        "stdout": "20\n",
        "stderr": "",
        "exit": 0,
        "duration_ms": 6,
        "timed_out": false,
        "truncated": false
      },
      "error": null,
      "volatility": "session",
      "provenance": "observed",
      "captured_at": "2026-08-24T14:02:14Z"
    }
  ]
}
```

Rules:

- **No verdict appears anywhere in this file.** No `state`, no `passed`, no
  `severity`. If one shows up, the runner has started interpreting.
- `applies: false` ⇒ `probe` and `declared` are `null`. The commands did not
  run.
- `error` is `null` or `{ "kind": "...", "detail": "..." }`, where `kind` is
  one of the UNKNOWN reasons the runner can determine on its own:
  `timeout`, `tool_absent`, `permission`, `probe_error`, and `unverifiable`.
  The runner never emits `tainted` (M3's judgment) or `expired` (M10's).

  `unverifiable` has exactly one runner-side cause: a probe was refused for
  attempting to read a secret value (AGENTS.md § *Never read a secret*). That
  is unverifiable *by design* — the answer is not unavailable, it is one Ferret
  declines to obtain — which is precisely what the reason means in
  ARCHITECTURE.md §5.
- Stdout and stderr are truncated at 64 KiB with `truncated: true`; the
  untruncated text goes to `.ferret/raw/<id>.{out,err}` for debugging Ferret
  itself.
- `provenance` is `observed` for everything the runner captures. Other values
  arrive in M11.
- Records appear in manifest order. The runner does no ordering by result,
  because ordering by result requires knowing results.

`schema_version` is an integer that increments on any breaking change to this
structure. A verdict engine reading a version it does not know exits 3.

---

## 8. Validation

`manifest/_schema.json` validates structure. These rules are semantic and are
checked by the loader in addition; both run **before any probe executes**, and
any failure exits 3.

Errors:

1. Unknown field anywhere in a check or file. Typo-as-silent-default is how a
   `timout_s` ends up meaning "10 seconds" forever.
2. Duplicate `id` across all manifest files.
3. `tainted_by` naming an id that does not exist.
4. A cycle in the `tainted_by` graph. Reported with both ends named.
5. `remedy` missing on a check whose `severity` is not `info`.
6. `mutating: false` without `mutating_why`, or `mutating` with any value
   other than `false`.
7. `$declared` referenced in `expect` or `remedy` when `declared` is absent.
8. An unknown interpolation token.
9. An `expect.type` that is not in §4, or a missing required field for its
   type.
10. An invalid regex in `matches`.
11. `timeout_s` ≤ 0, or `severity`/`volatility` outside their enums.
12. `layer` outside 0–10, or a `checks` entry that is not an object.

Warnings — printed, do not fail:

- `$probe` or `$declared` appearing inside a `probe`, `declared`, or
  `applies_if` string. Legal shell, almost certainly a misunderstanding of §5.
- `severity: blocker` with `decisive: false`. Legal, and worth a second look.
- A `timeout_s` above 30 on a check with no network-shaped command in it.

---

## 9. Worked examples

### Declared vs actual, with taint

```json
{
  "id": "runtime.node.version",
  "title": "Node version matches pin",
  "applies_if": "test -f package.json",
  "probe": "node -v 2>/dev/null",
  "declared": "cat .nvmrc 2>/dev/null || sed -n 's/.*\"node\": *\"\\([^\"]*\\)\".*/\\1/p' package.json",
  "expect": { "type": "semver_satisfies", "actual": "$probe", "range": "$declared" },
  "severity": "blocker",
  "tainted_by": ["shell.path.node_shadowing", "toolchain.version_manager.conflict"],
  "remedy": "nvm use $declared   # or: mise install",
  "timeout_s": 10,
  "volatility": "session"
}
```

Note what is *not* here: `decisive` (defaults true from `blocker`), `redact`
(defaults false), `layer` (from the file).

### A taint source

```json
{
  "id": "machine.clock.skew",
  "title": "System clock within tolerance",
  "probe": "date +%s",
  "declared": "curl -sI --max-time 5 https://cloudflare.com | sed -n 's/^[Dd]ate: //p'",
  "expect": { "type": "numeric_lt", "actual": "$skew_abs", "value": 120, "unit": "s" },
  "severity": "blocker",
  "remedy": "sudo sntp -sS time.apple.com   # macOS",
  "timeout_s": 8,
  "volatility": "volatile",
  "notes": "$skew_abs is NOT a valid token — see the discussion below."
}
```

**This example is deliberately wrong**, and it is the first thing the schema
gets asked to do that it cannot. §5 defines no `$skew_abs`: computing a
difference between two timestamps is arithmetic, and arithmetic in the verdict
engine is a general expression evaluator, which is a language, which is the
thing this manifest exists to avoid.

The honest options are to do the arithmetic in the probe and compare a plain
number:

```json
{
  "id": "machine.clock.skew",
  "title": "System clock within tolerance",
  "probe": [
    "remote=$(curl -sI --max-time 5 https://cloudflare.com | sed -n 's/^[Dd]ate: //p')",
    "[ -n \"$remote\" ] || exit 1",
    "r=$(date -j -f '%a, %d %b %Y %T %Z' \"$remote\" +%s 2>/dev/null) || exit 1",
    "l=$(date +%s)",
    "d=$((r - l)); [ $d -lt 0 ] && d=$((-d)); echo $d"
  ],
  "expect": { "type": "numeric_lt", "actual": "$probe", "value": 120, "unit": "s" },
  "severity": "blocker",
  "remedy": "sudo sntp -sS time.apple.com   # macOS",
  "timeout_s": 8,
  "volatility": "volatile"
}
```

...at the cost of a probe that is no longer skimmable, and which fails as
`exit 1` → empty stdout → UNKNOWN(probe_error) rather than distinguishing "no
network" from "unparseable date". **This is a real limitation of the schema,
and §11 is where it is on the record.**

### Presence, without reading the secret

```json
{
  "id": "forge.token.present",
  "title": "GH_TOKEN is set",
  "applies_if": "git remote -v 2>/dev/null | grep -q github.com",
  "probe": "if [ -n \"${GH_TOKEN:-}\" ] || [ -n \"${GITHUB_TOKEN:-}\" ]; then echo set; fi",
  "expect": { "type": "equals", "actual": "$probe", "value": "set" },
  "severity": "warning",
  "remedy": "export GH_TOKEN=...   # or: gh auth login",
  "redact": "secret",
  "volatility": "expiring"
}
```

Read the probe carefully: it emits the literal `set`, never the token. `-n` in
the shell tests the value without printing it, so the secret never reaches
stdout, never enters `evidence.json`, and never exists anywhere for a later
mistake to expose.

`printenv GH_TOKEN` would have been shorter and is **forbidden** — it puts the
value in the probe's stdout, which is reading it. The shaping in the runner
would have caught that particular case, but relying on it inverts the
guarantee: shaping is the backstop, not-reading is the rule. AGENTS.md
§ *Never read a secret* is normative here.

The same shape applies to files. To check `.npmrc`:

```json
{
  "id": "pm.npmrc.auth_present",
  "title": ".npmrc carries an auth token",
  "applies_if": "test -f .npmrc",
  "probe": "grep -c '_authToken' .npmrc || true",
  "expect": { "type": "numeric_gt", "actual": "$probe", "value": 0 },
  "severity": "info",
  "decisive": false,
  "redact": "secret",
  "notes": "grep -c counts lines; grep without -c would put the token in stdout."
}
```

`grep -c` yields a count. `grep '_authToken=.*'` would yield the token — a one
character difference between a check and a leak, which is why the rule is
stated as *never open the file* rather than *be careful with the file*.

### An override, and a non-decisive check

```json
{
  "id": "git.remote.url",
  "title": "Origin remote configured",
  "applies_if": "git rev-parse --git-dir >/dev/null 2>&1",
  "probe": "git config --get remote.origin.url",
  "expect": { "type": "non_empty", "actual": "$probe" },
  "severity": "info",
  "decisive": false,
  "mutating": false,
  "mutating_why": "config --get reads; --set/--add/--unset are the writers",
  "redact": "identity",
  "volatility": "stable"
}
```

`severity: info` means `remedy` is not required. `decisive: false` keeps it out
of the verdict and collapses it into the glance's context count.

### N/A

```json
{
  "id": "pm.pnpm.lockfile_version",
  "title": "pnpm lockfile matches pnpm major",
  "applies_if": "test -f pnpm-lock.yaml",
  "probe": "pnpm --version 2>/dev/null",
  "declared": "sed -n 's/^lockfileVersion: .\\(.*\\).$/\\1/p' pnpm-lock.yaml",
  "expect": { "type": "matches", "actual": "$probe", "pattern": "^9\\." },
  "severity": "warning",
  "remedy": "corepack enable && corepack prepare pnpm@9 --activate"
}
```

On an npm repo, `applies_if` exits 1, the probe never runs, and the check is
N/A — not a missing-pnpm finding.

---

## 10. What the runner does, in order

Stated as a sequence because the ordering carries the guarantees.

1. Load every `manifest/*.json` except `_schema.json`. Validate structurally,
   then semantically (§8). **Any failure exits 3 before a single probe runs.**
2. Build the id index. Resolve `tainted_by` references; a dangling one is an
   error here, not at verdict time.
3. Write the evidence header — `started_at`, versions, redaction level.
4. For each check, in manifest order:
   a. Denylist-check `applies_if`, `probe`, `declared` (§6). A refusal records
      `error.kind = probe_error` and skips execution for that command.
   b. Run `applies_if` under `timeout_s`. Non-zero ⇒ `applies: false`, record,
      next check.
   c. Run `probe`, then `declared`, each under the full `timeout_s`.
   d. Apply `redact` to captured output **before it enters the record**.
   e. Append the record.
5. Write `evidence.json` and `.ferret/raw/`.

The runner exits 0 when it captured everything it was asked to, and 3 when it
could not do its job — bad manifest, unwritable output. **It never exits 1 or
2**, because those are verdicts and it has no idea what it captured. The
process that decides is a different process.

---

## 11. Known limits, on the record

Stated rather than discovered later.

- **No arithmetic in `expect`.** The clock-skew example (§9) is the case that
  hurts, and the workaround pushes real logic into shell where it is neither
  validated nor re-evaluable against stored evidence. Adding one predicate
  (`numeric_delta_lt` over two operands) would fix that specific case without
  opening a general expression language, and is the obvious first extension if
  a second check needs it. Deliberately not added now, on one example.
- **No structured extraction.** Everything is a string from stdout. A check
  wanting one field of JSON writes `sed` or depends on `jq` being installed,
  and `jq` absent means UNKNOWN(probe_error) rather than UNKNOWN(tool_absent),
  because the runner cannot tell those apart from an exit code alone.
- **`tool_absent` is barely detectable.** The runner can only guess it from
  exit code 127 or a stderr matching `not found`, which is a heuristic and is
  documented as one.
- **No fields for preferred tool, ordered fallbacks, or install gating.** That
  is M10.5, and it will add keys to the check object. §8's unknown-field error
  means those keys cannot be used before then, which is intended — the schema
  version does not need to change to add optional fields, but this document
  does.
- **One probe per check.** A check needing two independent measurements must
  either compose them in shell or become two checks. Two checks is usually the
  right answer and taint expresses the relationship, but not always.
- **The denylist is not a sandbox.** §6 says so explicitly. The read-only
  guarantee rests on manifest review and the M5 fixture test; the denylist
  catches accidents, not adversaries.
- **Secret values are unreadable by policy, so some checks cannot exist.**
  AGENTS.md § *Never read a secret* forbids reading a value at all, not merely
  storing it. Ferret can therefore report that a token is present, its file's
  permissions, and whether a workflow references a name it cannot find — but
  never whether a token is *valid*, *correctly scoped*, or *pointing at the
  right host on its auth line*. Those are UNKNOWN(unverifiable) with a
  cross-reference, permanently, and no future milestone should quietly
  reintroduce them by reading. This is a deliberate loss of coverage in
  exchange for a guarantee that survives a careless debug print.

---

## 12. Changing this document

The schema is the contract. Changing it after M7 means revisiting every check
written against it.

- Adding an optional field with a default: edit this file and
  `manifest/_schema.json`, no `schema_version` bump.
- Adding an `expect` type: same, plus a test with a fixture.
- Changing the meaning of an existing field, or the shape of `evidence.json`:
  bump `schema_version`, and state the migration here.
