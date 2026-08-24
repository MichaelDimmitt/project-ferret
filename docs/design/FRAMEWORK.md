# Evaluating a Git Project in a Possibly-Misconfigured Environment

A complete evaluation surface, plus the execution order that makes a first pass hold.

---

## Part 0 — The Three Axes

The single most common structural mistake is treating this as one flat list of layers. It isn't. There are three orthogonal questions, and conflating them is what makes evaluations wrong.

**Axis 1 — Layer (where state lives).** Hardware → OS → filesystem → network → shell → runtime → package manager → git → forge → project → people. This is a *dependency stack*: each layer presupposes the one below. It is **not** the order you execute in.

**Axis 2 — Ambient vs Declared (whether state travels with the repo).** Cuts across every layer. This is where the defects are.

**Axis 3 — Interpretation state (how bytes become meaning).** Locale, encoding, normalization. Also cuts across every layer, because nearly every tool consumes text.

Layers tell you *what to capture*. Axes 2 and 3 tell you *what the finding actually is*.

---

## Part 1 — Ambient vs Declared

### Definition

**Ambient environment** = all state that influences the result of a command but is neither written in the repository nor stated in the command itself. Inherited from the surroundings rather than declared.

**Operational test:** *If I ran this exact command, on this exact commit, on a different machine — or in a different shell on the same machine — could the result change?* Everything that could cause that change is ambient.

### Defining properties

- **Implicit** — nothing in the repo mentions it
- **Inherited** — established before you arrived, by an installer, a previous run, or a coworker's 2023 setup doc
- **Unversioned** — no commit records its value; `git log` cannot explain it
- **Mutable out-of-band** — can change without any repo action, so an identical tree fails Wednesday after working Tuesday

### Where ambient state lives (ranked by how often it bites)

1. Process env vars — `PATH`, `NODE_ENV`, `HTTP_PROXY`, `GH_TOKEN`, stray `.env` exports
2. Shell state — rc files, aliases, functions, `direnv`/`asdf`/`nvm`/`mise` hooks, cwd, login vs non-login
3. Global config outside the repo — `~/.gitconfig`, `~/.npmrc`, `~/.ssh/config`, `/etc/hosts`
4. Caches and leftovers — `node_modules`, Turbo/Nx remote cache, PM caches, `.git/index`
5. Credentials — keychain entries, SSH agent contents, cached tokens, live SSO sessions
6. Running processes — dev server, database, Docker daemon, an already-bound port
7. Machine facts — clock, arch, libc, locale, filesystem case sensitivity, disk space
8. Network position — VPN state, DNS resolver, TLS interception, firewall rules

### Why it matters

Ambient environment is precisely the gap between declared and actual. The repo declares intent; the ambient environment supplies everything the declaration forgot. When they agree, the project works and you learn nothing. When they disagree:

- **False green** — builds only because of ambient state (global binary, warm cache, token in your keychain). Ships broken to everyone else.
- **False red** — fails only because of ambient state. You debug a project that was fine.

### The two halves fail differently

| | Ambient | Declared |
|---|---|---|
| Reproducibility | Temporal, per-machine | Universal, deterministic |
| Symptom | Same commit, different result | Everyone hits it |
| Fix lives in | The environment | A commit |

**The diagnostic that splits them faster than reading any config:** does this reproduce in a clean container on a different network?

### Hybrid cases — the most dangerous

`~/.npmrc` and `~/.gitconfig` are *files* (feels declared) but live outside the repo and aren't versioned (behaves ambient). **Ambient by location, declared by form.** These produce the most confident wrong diagnoses, because you can point at a file and feel you have evidence.

### The defect class you are actually hunting

> For every layer, the finding is **the thing that is ambient but should be declared.**

A project is well-configured to exactly the degree that it has migrated dependencies from the ambient column into the declared one. A devcontainer, a lockfile, a pinned CA path, a vendored registry config — all are ambient state that someone bothered to commit.

### Worked example: the network layer, split

| Ambient half | Declared half |
|---|---|
| DNS resolver and current answers | `.npmrc` registry and scope routing |
| VPN connected or not | Lockfile `resolved` URLs (pinned hosts) |
| TLS interception active today | Git remotes in `.git/config` |
| System CA trust store | `NODE_EXTRA_CA_CERTS` in a devcontainer |
| Firewall / egress allowlist | Dockerfile `FROM` registry |
| Registry actually up | CI workflow service containers |
| Inherited `HTTP_PROXY` | Hardcoded URLs in build scripts |
| Live SSO session | |

Every layer decomposes this way.

---

## Part 2 — Interpretation State

Locale and timezone feel homeless in a layer model because they aren't about *where state lives* — they're about *how bytes become meaning*.

### The discriminator

Placement should predict how you test it:

- If `LC_ALL=C <command>` changes the result → **locale**. Same files, same volume, different interpretation.
- If the command is unchanged but moving files to another volume/OS changes the result → **filesystem semantic**.

### Locale variables and their real blast radius

- **`LC_COLLATE`** — sort order. Affects `sort`, `ls`, glob ranges like `[a-z]`. Under `C` it's bytewise; under `en_US.UTF-8` it ignores punctuation and case. Silently changes generated-file output and diffs. The glibc 2.28 collation change famously required PostgreSQL index rebuilds.
- **`LC_CTYPE`** — whether a tool can decode a filename at all (Python 3 `UnicodeDecodeError` on `os.listdir`). Reads filesystem bytes, but the failure is in the process's decoder.
- **`LC_NUMERIC`** — `1,5` vs `1.5`. Breaks CSV and any script parsing numbers.
- **`LC_MESSAGES`** — output language. Breaks scripts grepping for English error strings.

**Check locale at every layer where a tool consumes text** — which is most of them.

### Timezone belongs to build reproducibility, not environment

Filesystems store mtimes as UTC epoch seconds; `TZ` only affects *rendering*. Its real blast radius is test determinism (DST boundaries, date parsing), log correlation, and cron. The one adjacent exception: ZIP's MS-DOS timestamp format stores *local* time with no offset, which is why reproducible builds need `SOURCE_DATE_EPOCH`. That's an archive-format quirk.

File TZ alongside `SOURCE_DATE_EPOCH`, clock skew, and tarball file ordering.

---

## Part 3 — The Full Surface (Layers 0–10)

Within a layer there is no stable ranking — what bites hardest depends on the stack. In a container it's `safe.directory`; on corporate macOS it's the CA bundle; in a monorepo it's remote cache poisoning. Rank these only against a specific target.

### Layer 0 — Physics and identity of the box

- CPU arch (`arm64`/`x86_64`), Rosetta/QEMU emulation, arch mismatch in prebuilt binaries
- Memory, disk free space, **inode exhaustion** (masquerades as "permission denied")
- OS + kernel version, distro, libc flavor (**glibc vs musl** — silently breaks native modules)
- WSL2 vs native Windows vs macOS vs Linux; crossing `/mnt/c` (catastrophic I/O perf)
- Container / VM / devcontainer / Nix shell — *are you even where you think you are?*
- **Clock skew** — breaks TLS handshakes, JWTs, cache invalidation; produces nonsense `git log` ordering

### Layer 1 — Filesystem semantics

- **Case sensitivity** — macOS APFS default insensitive, CI Linux sensitive → import breaks only in CI
- **Unicode normalization** — HFS+ enforced NFD; APFS is normalization-preserving but insensitive; Linux ext4 treats filenames as opaque bytes. A filename with `é` committed on macOS as NFD is a *different filename* on Linux. See `core.precomposeunicode`. Locale cannot fix this; it's a volume property.
- Windows NTFS storing UTF-16 vs POSIX byte strings
- Line endings, `core.autocrlf`, `.gitattributes` — the phantom "entire file changed" diff
- Symlink support; **path length and reserved filenames** (see Part 4)
- Permissions, umask, ownership, `core.fileMode` churn
- Network drive / synced folder (Dropbox, OneDrive) corrupting `node_modules`
- `inotify` / file-watcher limits → dev servers that "hang"

### Layer 2 — Network and trust

- DNS resolution, split-horizon DNS, `/etc/hosts` overrides
- `HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY` — **and their lowercase twins**; tools disagree on which they honor
- **Corporate TLS MITM** → custom CA bundle, `NODE_EXTRA_CA_CERTS`, `SSL_CERT_FILE`, `GIT_SSL_CAINFO`
- Egress allowlists / firewall — which registries and hosts are reachable at all
- Registry mirrors and their staleness
- VPN state, IPv6-only failures, air-gap

### Layer 3 — Shell and ambient environment

- Which shell; login vs non-login vs non-interactive — **CI is non-interactive, so your `.zshrc` hooks don't exist there**
- `PATH` ordering and shadowing — `which -a node`; the plural matters
- rc-file load order, aliases masking real binaries
- Shell hooks: `direnv`, `mise`, `asdf`, `nvm`, `volta`, `pyenv` — *and conflicts between two installed simultaneously*
- Env var precedence: shell → `.env` → `.env.local` → CI secrets → process defaults

### Layer 4 — Runtimes and toolchains

- Version pins: `.nvmrc`, `.node-version`, `.tool-versions`, `engines`, `packageManager`, `corepack`
- Actual runtime version vs declared (Node, Python, Ruby, Go, Rust, JVM)
- Multiple runtimes coexisting (a Python for `node-gyp`, a JVM for tooling)
- Native build chain: `make`, `gcc`/`clang`, `python3`, platform SDK/headers
- System libraries the project links against

### Layer 5 — Package manager

- Which one, and **does the lockfile agree** — more than one lockfile present is a red flag
- Lockfile version vs installed PM version
- `.npmrc` / `.yarnrc.yml`: registry, scopes, auth tokens, `node-linker`, PnP vs `node_modules`
- Private registry auth and token expiry
- Workspaces / monorepo topology, hoisting behavior
- `overrides` / `resolutions` / peer dep strategy
- **`postinstall` scripts** — trust boundary; `ignore-scripts` may be why nothing works
- Cache location, cache poisoning, offline mirror
- Install determinism: `npm ci` vs `npm install`

### Layer 6 — Git itself

- Git version — features silently absent on old versions
- `safe.directory` / dubious ownership — the classic container failure
- Config precedence: system → global → local → worktree → env (`GIT_CONFIG_*`)
- Remotes, protocol (SSH vs HTTPS), default branch name
- **HEAD state** — detached, mid-rebase, mid-merge, dirty tree, stashes
- Submodules (initialized? recursive? pinned?), **Git LFS** (installed? pointers or real files?)
- Shallow / sparse / partial clone / worktrees — *why "the file isn't there"*
- Hooks: `.git/hooks`, `core.hooksPath`, husky, lefthook, pre-commit — and whether they're bypassed
- `.gitignore` vs already-tracked files; `.gitattributes` smudge/clean filters
- Repo size, history bloat, secrets in history
- Signing: GPG/SSH, `commit.gpgsign`, expired keys
- `core.protectNTFS` (see Part 4)

### Layer 7 — Code host (forge)

**Definition:** a forge is **everything a mirror clone leaves behind.** `git clone --mirror` captures the complete repository — every object, ref, branch, tag, and note. Whatever is still on the server afterward, and would be lost if that server vanished, is the forge. Layer 6 is what the mirror captures; Layer 7 is the remainder.

Boundary cases, stated rather than hidden: **wikis** are themselves git repos, mirrorable separately, so they sit on the seam. **External CI** (Jenkins, CircleCI) satisfies the test but isn't a forge — the definition requires *attached to the hosted repository*, not merely server-side.

**Two properties that make this layer unlike every other:**

- **It is the most purely ambient layer in the stack.** Its state lives on someone else's server. It cannot be versioned, cannot be containerized away, and can change while you are looking at it — a revoked SSO session, a rate limit, a branch protection rule someone added this morning.
- **It is the only layer where evidence completeness depends on your own identity.** Two people running the identical sweep produce different reports, and neither is wrong. Sweep output from this layer is therefore **not portable between people**.

#### Capture tiers

Sort by how the evidence is obtained, because that determines what to do when you can't get it.

**T1 — In the mirror.** Objects, refs, branches, tags, notes. Objective; identical for everyone; no auth required beyond read access.

**T2 — Forge-only, retrievable.** Reachable via API or CLI at your current scopes. Note the ceiling is the **API surface and token scope**, not the tooling — `gh api` is a generic authenticated proxy to REST and GraphQL, so anything the API exposes is reachable even without a dedicated subcommand. Coverage differs by forge (`gh`, `glab`, or nothing for a bare Gitea instance), and GitHub Enterprise Server lags cloud, so documented endpoints may 404 on the instance you're actually pointed at.

**T3 — Forge-only, not retrievable.** Split by *reason*, because each implies a different action:

| | Cause | Examples | Action |
|---|---|---|---|
| **3a** | Write-only by design | Secret values, deploy key private halves | **Unverifiable, permanently.** No scope unlocks it. Cross-reference instead: list secret *names*, grep workflows for referenced names, report the diff. A missing name is a finding; a wrong value never is — it surfaces only as a CI job failing at use time. |
| **3b** | Above your permission ceiling | Audit log, org policy, admin-only settings | **Unverified, fixable.** Escalate scope or ask someone who has it. |
| **3c** | Expired | Logs past retention, artifacts, run-history caps | **Gone.** Not a permissions problem — the data doesn't exist. Only remedy is a fresh run. |
| **3d** | Not a resource | SAML authorization of a PAT, session validity, rate-limit standing | **Discovered by failing.** SAML status isn't a field you fetch; you get a 403 with a specific header. Rate limit is a measurement at an instant, and reading it consumes budget. Probe deliberately rather than waiting to be surprised. |

Also note that **UI-only settings** exist: features ship in the web UI before reaching the API. That list churns, so verify against current docs rather than any snapshot.

**Stamp every T2/T3 result** with identity, token scopes, and timestamp. **Report T3 explicitly** — as *"unverified: insufficient token scope"* or *"unverifiable by design."* An evaluation that silently omits what it couldn't see is the false-green failure from Part 6, relocated to this layer.

#### The seam — where one concept splits across tiers

Each row is a **declared/ambient pair**: the file is committed, the enforcement is not. This table *is* the finding surface for Layer 7. A CODEOWNERS file with no enforcement is exactly the "ambient but should be declared" defect class from Part 1.

| Concept | T1 (in the mirror) | T2/T3 (forge-only) |
|---|---|---|
| CODEOWNERS | the file | whether it's enforced |
| Workflows | `.github/workflows/*.yml` | runners, secrets, run history |
| Releases | tags | release notes, binary assets |
| Branch policy | — | protection rules, required checks, merge queue |
| Wiki | mirrorable separately | — |

#### Surface

- Which forge, cloud vs Enterprise Server, SSO/SAML session state
- Auth chain: SSH key → agent → keychain; or credential helper → PAT → **token scopes and expiry**
- `gh` CLI: installed, version, `gh auth status`, host config, extensions, `GH_TOKEN` overriding interactive auth
- API rate limits — the "random" failure
- Branch protection, required checks, CODEOWNERS, merge queue
- Actions: workflow files, runners (hosted vs self-hosted), secrets/variables, permissions, caching
- Dependabot / Renovate, security advisories, secret scanning, SBOM
- Releases, tags, published packages, environments

#### Prioritized capture

Not everything reachable is worth reaching for. Sort by whether it changes the verdict:

1. **Reachable and decisive** — `gh auth status`, token scopes, branch protection on the default branch, required checks, whether CI has ever passed on `main`, secret *names* vs what workflows reference
2. **Reachable but rarely decisive** — issue counts, release history, webhook lists
3. **Unreachable, where the gap is itself the finding** — secret values, org policy you can't read, expired logs, SSO state until it fails

### Layer 8 — Project configuration

- Entry point: `Makefile` / `justfile` / `Taskfile` / `scripts/bootstrap` — **the real README**
- `package.json` scripts, and whether they're honest
- TypeScript config, project references, path aliases that resolve under only one bundler
- Linter/formatter (ESLint flat vs legacy, Prettier, Biome), `.editorconfig`
- Bundler/framework config, transpiler, target/browserslist
- Test runner, coverage thresholds, fixtures
- Monorepo orchestrator (Turbo/Nx/Lerna) and its cache — **remote cache can fake a passing build**
- Migrations, seeds, required services (Postgres, Redis), ports in use
- `.env.example` vs actually-required vars

### Layer 9 — AI / agent configuration

- `CLAUDE.md`, `AGENTS.md` (increasingly the cross-tool standard)
- `.claude/`: `settings.json`, `settings.local.json`, permissions, commands, skills, hooks, subagents
- `.mcp.json` / MCP server config — and whether those servers are reachable
- Other tools: `.cursor/rules`, `.github/copilot-instructions.md`, `.windsurfrules`, `.aider.conf.yml`, `.clinerules`, `.continue/`
- `.devcontainer/`, `.vscode/{settings,extensions,tasks,launch}.json` — closest thing to declared intent
- Which are committed vs gitignored — local overrides that don't travel
- Conflicts between them — three files giving contradictory instructions

**Recommendation for a bare repo:** author `AGENTS.md` as the single source of truth (broadest tool support); keep `CLAUDE.md` as a thin pointer to it; put machine-executable setup in `.devcontainer/` or a `justfile` rather than prose; commit `.claude/settings.json` and gitignore `settings.local.json`.

### Layer 10 — Human and process

- Bus factor, commit recency, open PR/issue backlog
- Docs drift — does the README's install command still exist?
- License, and dependency license compatibility
- Who to ask when the above fails

---

## Part 4 — Windows Path Constraints (Detail)

Two NTFS/Win32 legacy constraints with the same symptom: clones fine on Linux/macOS, **fails partway through checkout on Windows**, leaving a broken working tree.

### MAX_PATH = 260

The limit is on the **full absolute path**, not the filename. Budget is `C:\` (3) + path + null terminator ≈ 256 usable characters.

- **Clone location eats the budget.** `C:\src\repo` vs `C:\Users\firstname.lastname\Documents\GitHub\repo` differ by ~45 characters before the repo contributes anything. This is why "works on my machine" is literally true here.
- Common offenders: nested `node_modules`, deep monorepo paths, long scoped package names, generated fixtures, `.next`/`dist` trees.

**Escapes, and why they only half-work:**

- `\\?\C:\...` prefix raises it to ~32,767 UTF-16 units, but disables path normalization — no forward slashes, no `.`/`..`, must be fully qualified
- Registry: `HKLM\SYSTEM\CurrentControlSet\Control\FileSystem\LongPathsEnabled = 1` (Win10 1607+)
- **The application must also declare `longPathAware` in its manifest.** Both conditions or nothing — which is why the registry key "doesn't work"
- Git specifically: `git config --system core.longpaths true`
- Reality: Git core, PowerShell 7, modern Node mostly cope. `explorer.exe`, Windows PowerShell 5.1, older installers, many native build tools do not. One unaware tool reintroduces the failure.

### Reserved device names

MS-DOS device namespace, still honored by the Win32 path parser: `CON`, `PRN`, `AUX`, `NUL`, `COM1`–`COM9`, `LPT1`–`LPT9`.

- **Extensions don't help** — `aux.c`, `con.js`, `nul.txt`, `prn.md` are all reserved
- **Reserved in every directory**, not just the drive root — `src/utils/aux.ts` fails
- Also blocked: `< > : " / \ | ? *` and ASCII 0–31 — note **`:`**, which breaks timestamped filenames like `log-12:30.txt` that Unix tooling generates happily
- **Trailing dots and spaces are silently stripped**, so `foo.` and `foo` collide
- Case-insensitivity means `README.md` and `readme.md` cannot coexist

### `core.protectNTFS`

On by default in Git for Windows. Rejects NTFS 8.3 short names and alternate-data-stream paths (`.git::$DATA`, `git~1`). This is a genuine security control — those forms were used to smuggle writes into `.git/` (CVE-2019-1352 and relatives). **Do not disable it.**

### Detection (run from repo root)

```bash
# Reserved names, extension-insensitive
git ls-files | grep -Ei '(^|/)(con|prn|aux|nul|com[0-9]|lpt[0-9])(\.|$)'

# Illegal characters and trailing dot/space
git ls-files | grep -E '[<>:"|?*]|[. ]$'

# Case collisions
git ls-files | tr 'A-Z' 'a-z' | sort | uniq -d

# Longest paths — compare against your Windows clone root budget
git ls-files | awk '{print length, $0}' | sort -rn | head -20
```

### Where this sits

Another declared/ambient split. A path *unrepresentable* on a target platform is a **committed defect** — reproducible for every Windows user, fixed by a commit. The 260 budget is **ambient** — depends where the person cloned. Same failure, opposite remediation.

**WSL2 ext4 has none of these limits**, so `\\wsl$` paths sidestep the category — until a Windows-side tool reaches across. And CI on `ubuntu-latest` never catches any of it, which is why these bugs survive to reach users. A `windows-latest` job that does nothing but `git clone` is a cheap, high-value check.

---

## Part 5 — Execution Order

Ordered by **blast radius ÷ cost to check**. This is deliberately *not* the layer order.

1. **Clock** — skew invalidates TLS, tokens, and every timestamp you're about to read
2. **Disk free and inodes** — masquerades as permission errors
3. **Arch and libc** — `uname -m`, glibc vs musl; determines whether prebuilt binaries can work at all
4. **`PATH` and shadowing** — `which -a` on every tool that matters
5. **Proxy and CA trust** — `HTTP(S)_PROXY`, `NO_PROXY`, `NODE_EXTRA_CA_CERTS`, system CA store
6. **Git repo state** — HEAD, dirty tree, mid-rebase/merge, `safe.directory`, shallow/sparse, submodules, LFS
7. **Auth status** — SSH agent, credential helper, `gh auth status`, token scopes and expiry
8. **Runtime versions vs pins** — declared (`.nvmrc`, `engines`, `.tool-versions`) against actual
9. **Lockfile integrity** — one lockfile, matching PM version, `npm ci` clean
10. **Registry reachability** — resolve and fetch, including private scopes
11. **Golden path from a fresh clone in a fresh non-login shell** — clone → install → build → test → run
12. **CI parity** — same path on a runner, including a `windows-latest` clone check
13. **Quality gates** — lint, types, coverage; enforced or decorative?
14. **Security** — secrets in history, `postinstall` trust, advisories, signing
15. **Docs and process drift** — does the README's install command still exist?

**Steps 1–7** take ~30 seconds total and invalidate everything downstream if wrong.
**Steps 8–10** determine whether the result is reproducible.
**Step 11 is the actual evaluation.** Steps 1–10 exist only to make its result trustworthy.

---

## Part 6 — Why First Passes Fail

Not because layers are missing. Because people **interpret while collecting**, so ambient state makes a broken layer look healthy.

**1. Silent success.** A cached `node_modules`, a warm Turbo cache, a running daemon — the build passes for reasons unrelated to correctness.
→ *Counter: evaluate from a fresh clone in a fresh non-login shell, at least once.*

**2. Layer aliasing.** One symptom, six candidate layers. A TLS failure can be clock, CA bundle, proxy, DNS, expired token, or registry outage. Interpreting early locks you onto the wrong one.
→ *Counter: capture all layers before diagnosing any of them.*

**3. Declared ≠ actual.** Every layer has a stated value and an effective value. The gap is the entire bug surface.
→ *Counter: build a two-column table — Declared | Actual — for every pinned thing. Every mismatch is a finding; that table **is** the report.*

### The method

1. One scripted evidence sweep — versions, configs, resolutions, reachability, git state — dumped to a single file. **No interpretation.**
2. Diff declared vs actual.
3. Run the golden path clean.

That is a first pass that holds.

### The closing note on taxonomy

The layer list's job is not to be tidy. It's to guarantee the sweep captures everything once. After capture, correct placement matters only for *diagnosis* — and diagnosis is better served by discriminators (`LC_ALL=C`, clean container on a different network) than by which bucket a thing sits in.
