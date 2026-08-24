# Language choice

What the runner is written in, why, and what would change the answer.

This exists because "we picked X" is not an argument. A choice that survives
only because nobody questioned it is not a choice.

---

## The premise: mise supplies the runtime

**Ferret does not have to run on whatever the machine happens to have.**

Stage zero (`bootstrap/run.sh`) is POSIX sh and measures the box. mise is the
first thing the proposal asks for. You approve it, mise installs, and from that
point mise provides whatever runtime the tool needs — Go toolchain, Erlang/OTP,
a specific Python, anything.

This inverts the usual constraint. The question is not "what is already
installed everywhere" but **"what is the best language for this program, given
we can have any runtime after one approved install."**

Two consequences:

- *"Language X is already on most machines"* carries **no weight**. It was the
  entire argument for Python and it is worth nothing here.
- The approval gate is the mechanism, not an obstacle. mise going through it
  is that machinery working as designed, not a violation of the
  no-mutation-before-baseline rule. That rule governs **stage zero**, which is
  bash and always will be.

### What still runs in bash, permanently

| Stage | Language | Why |
|---|---|---|
| Stage zero — measure the machine | POSIX sh | Runs before mise exists. Cannot be written in a runtime it is measuring. |
| Tier-1 fallback (M6) | POSIX sh | Runs when mise was declined or could not install. Verdict UNKNOWN, never GO. |
| Stage one — the runner | **Go** | Everything after mise is available. |

The bash fallback is not a fallback for the *language* decision — it exists
because a user may decline mise, and a declined proposal must still produce a
diagnosis. That is a product requirement, not a hedge.

---

## The constraint that survives the premise

**The runner cannot depend on what it measures.**

mise makes runtimes available; it does not make circularity acceptable.
Diagnosing a broken Node install using Node means the tool that would explain
the failure is broken by the failure.

The minimal example:

```
$ export NODE_OPTIONS="--require /tmp/deleted.js"
$ node --version
Error: Cannot find module '/tmp/deleted.js'
```

Node is installed and healthy. One environment variable breaks every Node
process on the machine. A Node-based Ferret dies with that error. Anything
else prints:

```
NO-GO  NODE_OPTIONS references a missing file: /tmp/deleted.js
       remedy: unset NODE_OPTIONS
```

The failure mode is a stack trace where a report should be — you cannot
distinguish "Node is broken" from "Ferret is broken." That is the same class
of failure as an UNKNOWN rendering green, which this whole design is organized
against.

Node is a large fraction of what Ferret measures (`.nvmrc`, lockfiles,
`packageManager`, registry reachability, `NODE_EXTRA_CA_CERTS`). **Node is
disqualified.** This says nothing about Node generally.

---

## What the runner actually does

Ranking languages requires knowing the workload. It is:

1. Parse a JSON manifest into typed records.
2. Spawn dozens of short-lived subprocesses, each with a hard timeout.
3. Capture raw stdout, stderr, exit code, duration. Interpret nothing.
4. Build a DAG from `tainted_by`, topologically sort it, detect cycles.
5. Apply predicates to evidence; emit four states.
6. Serialize `evidence.json`; render a text report.
7. Exit.

**Short-lived batch process doing structured data manipulation and process
control.** No concurrency to speak of beyond running probes in parallel, no
long-lived state, no network service, no UI.

That description is the whole ranking. Everything below follows from it.

---

## The choice: Go

**Go, built with mise, shipped as a static binary.**

| Why | Detail |
|---|---|
| Stdlib covers the entire workload | `encoding/json`, `os/exec`, `context`, `regexp`, `path/filepath`. No third-party packages needed for anything the runner does. |
| Typed JSON is the schema | `encoding/json` unmarshals into structs. The manifest schema becomes a Go type — the contract is checked by the compiler instead of by hand-written validation. |
| `context` is the right timeout model | `exec.CommandContext` with a deadline is exactly M1's `timeout_s` requirement, and it kills the process group properly. |
| Fast start | Single-digit milliseconds. M12 wants the tenth run on a known machine to report "in seconds"; interpreter boot is pure overhead against that. |
| Static binary | Zero runtime dependencies. The output artifact runs on a machine with no toolchain at all — useful for M6-adjacent cases and for anyone who wants Ferret without mise. |
| Boring by design | PROMPT.md mandates obvious code. Go is aggressively unclever, which is a virtue in a tool that runs on broken machines. |

The distribution objection that counted against Go under the old premise —
"you have to build and ship binaries" — **evaporates**. mise installs the Go
toolchain, `go build` runs locally, and the result is a static binary. No
cross-compilation matrix, no release infrastructure, no "why should I trust
this binary" question. Build it on the machine that will run it.

### Go's real costs

Stated plainly, since this document is worth nothing if it only argues one
side:

- **Verbose error handling.** `if err != nil` on every line that can fail. In a
  tool where *every* probe can fail, that is a lot of ceremony.
- **No sum types.** The four-state model (GO / NO-GO / UNKNOWN / N/A) is a
  tagged union, and Go models it with a string constant plus discipline. Rust
  and Elixir both express it better.
- **`encoding/json` is awkward about optional fields.** Distinguishing "absent"
  from "present but zero" needs pointers or `json.RawMessage`. The manifest
  schema has optional fields, so this will come up.

None of these is disqualifying. All of them are annoyances a maintainer feels
weekly.

---

## Rust — held as a live candidate

Rust is genuinely competitive here, and the argument I made against it earlier
(compile times, steep language) was weak. The real trade is different.

**Where Rust wins over Go:**

- **Sum types are the right model.** `enum Verdict { Go, NoGo, Unknown(Reason), NotApplicable }`
  makes "no code path turns UNKNOWN into GO" a *compiler-enforced* property
  rather than a test. PLAN.md M2 lists that test as mandatory; in Rust, a
  non-exhaustive match fails to compile. That is a real advantage for this
  specific invariant.
- **`Option<T>` solves the optional-field problem** that Go handles with
  pointers.
- **`Result<T, E>` with `?`** is less ceremonious than `if err != nil` once you
  are used to it.
- Same static-binary story, same mise-installable toolchain.

**Where Rust loses:**

- **The stdlib does not cover the workload.** No JSON, no regex, no subprocess
  timeout. That is the decisive difference from Go, and it interacts directly
  with the project's no-dependency invariant.
- Compile times are minutes rather than seconds — felt on every iteration of a
  tool this small.
- Steeper for a contributor who does not already write it.

### If Rust: the recommended crate set

The invariant would need rewording from "no dependencies" to "a small, audited,
pinned set." These are the crates that give the best surface area for this
exact workload, chosen for ubiquity and low transitive weight:

| Need | Crate | Why this one |
|---|---|---|
| JSON parse/serialize | `serde` + `serde_json` | The de facto standard. Derive macros make the manifest schema a struct, same benefit as Go's `encoding/json`. |
| Subprocess with timeout | `std::process` + `wait-timeout` | std can spawn but cannot wait with a deadline. `wait-timeout` is tiny and does exactly that, nothing more. |
| Regex | `regex` | Same author as the language's own tooling; no backtracking, linear-time guarantees — appropriate for running untrusted patterns from a manifest. |
| CLI args | `clap` (derive) | Heavier than the rest. `pico-args` or hand-rolled is defensible if you want the dependency count near zero. |
| Error context | `anyhow` | For a binary (not a library), `anyhow` is the low-ceremony choice. Skip `thiserror` — that is for libraries defining error types. |

Deliberately **not** recommended:

- `tokio` / `async` — the workload is a few dozen short subprocesses.
  `std::thread` with a small pool covers parallel probes without dragging in an
  async runtime and its transitive tree.
- `reqwest` — M9 needs a couple of HTTPS calls. Shelling out to `curl` (already
  probed for) or using `ureq` beats pulling in a full HTTP stack with TLS.

That is **five crates**, all extremely widely used, all pinned. A defensible
supply chain — but it is five more than Go needs, and the invariant document
would have to say so honestly.

**Verdict on Rust:** choose it if the compiler-enforced state machine matters
more than the zero-dependency property. Both are legitimate readings of this
project's values. Go is chosen because "no dependencies, not one" is currently
the louder invariant, and because the workload is squarely Go's center.

---

## The rest, briefly

| Language | Verdict |
|---|---|
| **Node** | Disqualified — circular. See the `NODE_OPTIONS` example above. |
| **Python** | Its only advantage was "already installed," which the mise premise deletes. Competing on readability alone, which is the criterion it was objected to on. No longer a candidate. |
| **Elixir** | Viable but a poor shape fit. BEAM strengths — concurrency, supervision, distribution, hot reload — go entirely unused by a short batch process, while its costs (200–500ms boot, Erlang/OTP install weight, `escript` still needing a VM) are all paid. Pattern matching does suit the four-state model. Wins only on maintainer fluency. |
| **POSIX sh** | Correct for stage zero and the Tier-1 fallback. Wrong for the runner: no data structures, no JSON without `jq`, and a topological sort in shell is unmaintainable. |

---

## Summary

| Language | Runtime deps | Stdlib covers workload | Sum types | Start time | Verdict |
|---|---|---|---|---|---|
| **Go** | none (static) | **yes** | no | ~ms | **chosen** |
| **Rust** | none (static) | no — needs ~5 crates | **yes** | ~ms | live alternative |
| Elixir | Erlang/OTP | mostly (`:json` in OTP 27+) | yes | 200–500ms | shape mismatch |
| Python | interpreter | yes | no | ~50ms | premise deleted its advantage |
| Node | Node | yes | no | ~40ms | **disqualified — circular** |
| POSIX sh | none | no | no | ~ms | stage zero only |

---

## When to reopen this

- **The four-state invariant keeps getting violated in review.** That is Rust's
  argument becoming concrete — if discipline is not holding the line, move it
  into the compiler.
- **The runner grows past ~5000 lines.** Go's verbosity compounds; Rust's
  expressiveness starts paying for its compile times.
- **A contributor is fluent in one of these and not the others.** An
  unmaintained good fit loses to a maintained adequate one.
- **The dependency invariant gets formally relaxed.** If the project accepts a
  pinned crate set, Rust's stdlib gap stops being decisive.

**The escape hatch is real and cheap.** `evidence.json` is the contract between
the runner and everything downstream. A rewrite in another language that emits
the same `evidence.json` leaves the verdict engine, renderer, and every test
fixture untouched. **The format is the contract, not the language** — which is
why M1 spends its effort on the schema.

---

## What this changed elsewhere

All applied when this decision was ratified:

- [x] `AGENTS.md` and `docs/plan/PROMPT.md` reworded — the rule is **no
      third-party dependencies in the runner**, and the language is Go.
- [x] `docs/design/ARCHITECTURE.md` §3 rewritten from the mise premise, with a
      pointer here.
- [x] `scripts/sweep.sh` resolves a built binary, then a mise toolchain, then a
      system Go, then falls back to `ferret/bootstrap.sh`. It records which one
      won — "which Go built this" is ambient state, and Ferret is not exempt
      from its own rule.
- [x] `ferret/__init__.py` replaced by `ferret/main.go`.
- [x] PLAN.md renamed `runner.py` → `runner.go` and friends throughout; M6's
      header line is now `runner unavailable`, not `python3 absent`.
- [x] `mise.toml` pins Go 1.23; `go.mod` declares the module. mise is proposed,
      never silently installed.
- [x] `.gitignore` covers the compiled `ferret/ferret`.
