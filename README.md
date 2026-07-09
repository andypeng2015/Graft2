# graft

Structural version control with built-in coordination and governed execution.

Git treats source files as bags of lines. Two developers add different functions to the same file — conflict. Both add different imports — conflict. One renames a variable, another adds a function nearby — conflict. None of these are real conflicts.

**graft** is a standalone version control system that decomposes source into structural entities via [gotreesitter](https://github.com/odvcencio/gotreesitter) — functions, methods, classes, imports — and merges at that level. Independent additions merge cleanly. Import blocks get set-union merged. Only genuine semantic overlaps produce conflicts.

That structural recording point is what makes the rest of graft possible. Once changes are recorded against entities instead of line hunks, graft can coordinate work on real code objects, record governed decisions around them, and give agents a shared runtime that is tied directly to version control instead of bolted on beside it.

## Agent Skill

Agents working with Graft should use the [using-graft](https://github.com/odvcencio/m31labs-skills/blob/main/skills/using-graft/SKILL.md) skill.

```
# Git: CONFLICT (both modified main.go)
# Graft: clean merge — two independent functions added

$ graft merge feature
merging feature into main...
  main.go: clean
merge completed cleanly
```

## How it works

Graft parses every source file into an ordered list of **entities**:

| Entity kind | Examples |
|------------|---------|
| Preamble | `package main`, license headers |
| Import block | `import (...)`, `from x import y` |
| Declaration | Functions, methods, types, classes, structs, traits |
| Interstitial | Whitespace and comments between declarations |

Each entity has an **identity key** (e.g. `decl:function_definition::ProcessOrder`) that survives editing, reordering, and branch divergence. Merge operates on these identities instead of line numbers:

- **Unchanged** — keep as-is
- **Modified by one side** — take the modification
- **Modified identically by both** — no conflict
- **Modified differently by both** — diff3 fallback on that entity's body
- **Import blocks** — set-union merge (combine all imports, deduplicate)
- **Added by one side** — insert at correct position
- **Deleted by one side, unchanged by other** — remove
- **Deleted vs modified** — real conflict

The critical invariant: reconstructing entities always reproduces the original source byte-for-byte.

## Install

```bash
go install github.com/odvcencio/graft/cmd/graft@latest
```

Requires Go 1.25+. Pure Go, no C dependencies.

After installing, run a global preflight from any directory:

```bash
graft doctor --global
```

## Release

The `v0.10.0` release adds auditable release artifacts and preflight checks. Tag
builds produce cross-platform binaries with embedded version, commit, and build
time metadata, then emit checksum manifests, SBOM, provenance, and optional
signature metadata.

Before cutting a release, run:

```bash
graft release check --version v0.10.0 --changelog CHANGELOG.md
go test ./...
```

## Security

Graft's local threat model is documented in
[docs/threat-model.md](docs/threat-model.md). It covers malicious repository
contents, malicious remotes, hooks and governed execution, credential leakage,
signing, sandboxing, object integrity assumptions, and the current release
blockers that remain open.

Repository object hash policy and future multi-hash migration requirements are
documented in [docs/object-hash.md](docs/object-hash.md).

Commit and release signature formats, allowed-signers verification, and key
rotation guidance are documented in [docs/signing.md](docs/signing.md).

## Performance

Performance fixtures, operation budgets, and JSON benchmark artifact commands
are documented in [docs/performance.md](docs/performance.md).

## Usage

Graft is two things at once:

- A structural VCS that stores and merges code by entity.
- A governed coordination runtime that lets humans and agents claim work, publish plans, keep shared notes, and run actions through policy.

Graft follows the same mental model as Git:

```bash
# Initialize a repository
graft init myproject
cd myproject

# Stage and commit
echo 'package main

func Hello() {}
' > main.go
graft add main.go
graft commit -m "initial commit"

# Branch and diverge
graft branch feature
graft checkout feature
# ... add func Goodbye() ...
graft add main.go
graft commit -m "add Goodbye"

# Back to main, make a different change
graft checkout main
# ... add func Greet() ...
graft add main.go
graft commit -m "add Greet"

# Structural merge — no conflict
graft merge feature
```

### Commands

**Core**
```
graft init [path]                     Create a new repository
graft add <files...>                  Stage files for commit
graft commit -m <message>             Record changes
graft status                          Show working tree status
graft diff [ref1..ref2] [--staged] [--entity] [--review] [--json]
                                      Show changes (line-level, entity-level, or review summary)
graft log [--oneline] [-n N] [--entity <selector>]  Show commit history
graft show [commit-ish]               Show commit metadata and changed files
graft workflows [topic]               Show common workflow guides
graft protocol [--json]               Show the supported remote protocol contract
graft completion [shell]              Generate shell completion scripts
graft man [--dir <dir>]               Generate man pages
```

**Branching & Merging**
```
graft branch [name] [-d name]        List, create, or delete branches
graft checkout <target|-> [-b]        Switch branches
graft switch <branch|-> [-c <new>]    Switch branches (modern alternative to checkout)
graft merge <branch>                  Three-way structural merge
graft rebase [--onto] [-i] <upstream> Reapply commits on a new base (--continue/--abort/--skip/--autostash)
graft cherry-pick [--entity <sel>] <commit>  Cherry-pick a commit or entity (--continue/--abort/--skip)
graft revert <commit>                 Revert a commit by creating an inverse commit (--continue/--abort)
```

**Remote**
```
graft clone <url> [dir]               Clone from Graft/Orchard or Git forge
graft push [remote] [branch] [--require-signed] [--allowed-signers <file>]  Push local branch or tag to remote
graft pull [remote] [branch]          Fetch and fast-forward local branch
graft fetch [remote]                  Download objects and refs without merging
graft remote [--json]                 Manage remotes (add, remove, list)
graft publish [owner/repo]            Create remote repo on Orchard, set origin, and push
graft auth                            Authenticate with Orchard (setup, ssh-login, bootstrap-ssh, status, logout)
```

**History & Inspection**
```
graft blame [<path>] [--entity <path::key>] [--limit N] [--json]
                                      Structural blame for an entity or every entity in a file
graft bisect start|good|bad|skip|reset|log|run  Binary search for a bug-introducing commit
graft reflog [--json] [--head]        Show local ref update history
graft shortlog [-s] [-n]              Summarise commit history by author
graft tag [name] [--sign|--sign-key <path>] [--verify <name>]  List, create, sign, verify, or delete tags
```

**Working Tree**
```
graft clean [-n] [-f] [-d]            Remove untracked files from the working tree
graft grep [-i] [-F] [--entity] [--kind <kind>] [--json] <pattern>
                                      Search file content or entity names for a pattern
graft stash [push|pop|apply|list|drop|show]  Stash and restore working directory changes
graft reset [paths...]                Unstage paths (restore index from HEAD)
graft rm [--cached] <paths...>        Remove paths from index and/or working tree
graft sparse-checkout set|add|list|disable  Manage sparse checkout patterns
graft worktree add|list|remove|prune  Manage multiple linked working trees
```

**Modules**
```
graft module add <url> [path]         Add a module (--track <branch> or --pin <tag>)
graft module rm <name>                Remove a module and its working tree
graft module update [name...]         Fetch latest objects for modules (--depth N)
graft module sync                     Sync module working trees from lock file
graft module status                   Show module state vs lock vs upstream
graft module list                     List configured modules with paths and versions
```

**Large Files**
```
graft lfs track <pattern>             Track files matching pattern with LFS
graft lfs untrack <pattern>           Stop tracking pattern with LFS
graft lfs ls-files                    List LFS-tracked files in staging
graft lfs status                      Show LFS status for tracked files
```

**Archive & Maintenance**
```
graft archive [--format=tar|zip] <tree-ish>  Create an archive of files from a commit
graft release check [--json] [--version <version>]  Run release preflight checks
graft release manifest [--json] <file-or-dir>...  Generate SHA-256 release checksums
graft release verify-manifest [--json] [--base-dir <dir>] <manifest>  Verify release checksums
graft release sbom [--name <name>] <file-or-dir>...  Generate SPDX JSON SBOM
graft release provenance [--builder-id <id>] <file-or-dir>...  Generate provenance statement
graft release sign [--sign-key <path>] <file-or-dir>...  Sign release artifact metadata
graft release verify-signature [--json] [--base-dir <dir>] [--allowed-signers <file>] <signature-json>  Verify release signatures
graft gc                              Pack loose objects and prune unreachable data
graft verify [--signatures] [--require-signed] [--allowed-signers <file>] [--json]  Verify object integrity and commit signatures
graft doctor [--global] [--json]      Diagnose repository health or installed environment
graft version                         Print version
```

**Coordination & Runtime**
```
graft workon --as <name>              Join coordination as an agent identity
graft workon --recover --as <name>    Replace a stale or missing local coordination identity
graft coord                           Show coordination dashboard
graft coord check [--stale-after <duration>]  Inspect claims/impact before acting
graft coord cleanup-stale [--dry-run] [--stale-after <duration>]  Remove stale coordination agents and their claims
graft coord plan ...                  Manage canonical shared plans in refs/coord/plans/
graft coord note ...                  Manage shared scratch/handoff/status notes in refs/coord/notes/
graft coord task ...                  Manage operational work items in refs/coord/tasks/
graft coordd serve                    Run the local coordination/event daemon
graft coordd exec -- <cmd...>         Run a governed command through policy + runtime selection
graft coordd exec --check-only -- <cmd...>  Evaluate coordd policy without executing
graft coordd spawn ...                Authorize or launch governed child workstreams
graft coordd spawn-trace --id <id> --json --redact  Export support-safe policy traces
graft coordd guard doctor [--json] [--profile <profile>]  Check sandbox backend health
graft workspace ...                   Register related repos for cross-repo coordination
graft mcp ...                         Expose graft as an MCP server for AI hosts
```

### Exit Codes

`graft` uses stable exit codes for automation:

| Code | Meaning |
| ---: | --- |
| 0 | Success |
| 1 | General failure |
| 2 | Usage or flag error |
| 3 | Merge/rebase/cherry-pick/revert conflict |
| 4 | Verification or preflight check failure |
| 5 | Authentication or authorization failure |
| 6 | Network or remote protocol failure |
| 7 | Repository state needs repair |

External commands run through governed execution preserve their process exit code where applicable.

### JSON Output Contract

Command JSON emitted through `--json` includes a top-level `schemaVersion` integer. Version `1` is the current CLI JSON schema. Additive fields may appear in compatible releases; field removals or type changes require a schema version bump and are guarded by compatibility tests.

`graft protocol --json` emits the supported remote protocol contract, including the protocol version, headers, capabilities, endpoints, server limit keys, response read caps, object types, and error JSON shape. This is the supported machine-readable reference for independent Graft/Orchard client and server implementations.

### Hook Trust

Repo-provided hooks are trusted for repositories created with `graft init`. Cloned or imported repositories mark repo hooks untrusted by default, so executable `.graft/hooks/*` scripts and repo `hooks.toml` entries are skipped until acknowledged:

```bash
graft config hooks.trusted true
```

Use `graft config hooks.trusted false` to disable repo-provided hooks again.

### Repo-local coordd policies

`coordd` can load repo-local Arbiter bundles from `.graft/coordd/policies/`.

- Action policy roots: `.graft/coordd/policies/action.arb` or `.graft/coordd/policies/action/main.arb`
- Spawn policy roots: `.graft/coordd/policies/spawn.arb` or `.graft/coordd/policies/spawn/main.arb`
- These are the only auto-loaded roots. Files like `action.example.arb`, `spawn.example.arb`, or `something.example.arb` are ignored unless a live policy explicitly `include`s them.
- For examples and starter layouts, prefer either commented-out snippets inside the real bundle or separate `*.example.arb` files that can never be mistaken for active policy.

### coordd sandbox backends

`coordd` chooses the execution backend from the guard config and the policy-selected runtime profile.

- `auto` tries a configured container image first, then `host-bwrap`, then falls back to `host-direct`.
- `container` requires a configured image plus an available `podman` or `docker` runtime.
- `host-bwrap` requires Linux and an available `bwrap` binary.
- `host-direct` is always available, but it cannot enforce repo filesystem scope, network deny, or delete scope isolation.

Use `graft coordd guard doctor --json` to check the selected backend, probe failures, and any isolation degradations before relying on governed execution on a machine. Explicit `container` or `host-bwrap` preferences fail the doctor check when the backend is unavailable; `auto` may still pass while reporting a warning when it falls back to `host-direct`.

### Plans vs notes

Graft intentionally keeps active coordination material out of tracked source history.

- Canonical plans live in `refs/coord/plans/`. They are the checked-in program record for multi-step work, ownership, and completion state. Use `graft coord plan ...`.
- Shared notes live in `refs/coord/notes/`. They are the shared scratchspace for in-progress thinking, handoffs, short-lived status updates, and coordination context that should not clutter the source tree. Use `graft coord note ...`.
- Source-controlled docs stay for durable product or user documentation. The repo should not fill up with transient plan/spec Markdown when that material belongs in coord refs instead.

### Remote shorthand

Use `orchard:owner/repo` instead of full URLs:

```bash
graft remote add origin orchard:alice/demo
graft clone orchard:alice/demo
graft publish alice/demo
```

`graft remote add` and `graft remote set-url` require HTTPS, SSH, file, or
loopback HTTP by default. Use `--allow-insecure` only for explicitly trusted
non-local HTTP or `git://` remotes.

### Auth configuration

`graft` supports global auth/config in `~/.graftconfig` (token, default host, owner/username).
Environment variables still override file values.

Credential and host precedence:

- Orchard host: explicit `--host`, then `GRAFT_ORCHARD_URL`, then `~/.graftconfig`, then the command default.
- Bearer token: explicit token flag where a command supports one, then `GRAFT_TOKEN`, then the host-matching profile in `~/.graftconfig`.
- Owner: `GRAFT_OWNER`, then `ORCHARD_OWNER`, then the host profile owner, then the host profile username.
- Remote HTTP basic credentials: `GRAFT_USERNAME` and `GRAFT_PASSWORD`, then URL userinfo when no bearer token is available.

Environment-provided tokens are process-local overrides. `graft` uses them for the current command but does not persist them to `~/.graftconfig`; use `graft auth setup`, `graft auth ssh-login`, or `graft config --global` for intentional stored configuration.

```bash
# Non-secret global defaults
graft config --global orchard.url https://orchard.dev
graft config --global orchard.username alice
graft config --global orchard.owner alice
graft config --global signing.key ~/.ssh/id_ed25519
graft config --global signing.auto true

# Interactive setup (magic-link login + optional SSH key registration)
graft auth setup --host https://orchard.dev

# Agent-native login (no browser/magic-link flow, uses registered SSH key)
graft auth ssh-login --host https://orchard.dev --username alice --ssh-key ~/.ssh/id_ed25519

# First-key bootstrap for headless agents.
# If already authenticated, graft auto-mints a short-lived bootstrap token.
graft auth bootstrap-ssh --host https://orchard.dev --username alice --ssh-key ~/.ssh/id_ed25519

# First-time from terminal (no prior auth token):
# requests magic-link auth, verifies, mints bootstrap token, registers key.
graft auth bootstrap-ssh --host https://orchard.dev --email alice@example.com --username alice --ssh-key ~/.ssh/id_ed25519

# Optional explicit token override for automation:
GRAFT_BOOTSTRAP_TOKEN=... graft auth bootstrap-ssh --host https://orchard.dev --username alice --ssh-key ~/.ssh/id_ed25519

# Inspect stored auth state
graft auth status

# Machine-readable credential diagnostics without printing tokens
graft auth doctor --json
```

See [docs/signing.md](docs/signing.md) for the stable signing payload formats,
allowed-signers verification, and key rotation guidance.

Git forge shorthand is also supported:

```bash
graft clone github:owner/repo
graft clone gitlab:group/subgroup/repo
graft clone bitbucket:workspace/repo
```

For Git-forge clones, `graft` bootstraps a local `.graft` repository from the cloned Git HEAD snapshot.

For self-hosted instances, set `GRAFT_ORCHARD_URL`:

```bash
export GRAFT_ORCHARD_URL=https://code.example.com
graft remote add origin orchard:alice/demo
```

When a remote is a Git forge URL, `graft` routes `clone/pull/push` through Git transport; Orchard remotes continue to use native Graft transport.
`graft clone` from a Git forge bootstraps `.graft` from the cloned Git HEAD snapshot so structural workflows can start immediately.

### Structural diff

```bash
# Line-level diff (default)
graft diff

# Entity-level diff — shows which functions/types changed
graft diff --entity

# Review summary — declaration-level changes only, good for PR review
graft diff --review

# Diff between two branches or commits
graft diff main..feature
graft diff main..feature --entity

# JSON output for tooling (pairs with --entity or ref range)
graft diff --json
graft diff main..feature --json
```

## Architecture

```
.graft/
  HEAD                    ref: refs/heads/main
  objects/                SHA-256 content-addressed store (2-char fan-out)
  refs/heads/             Branch tips
  refs/coord/             Coordination state: agents, claims, plans, notes, tasks, feed, policy
  index                   Staging area
```

**Object types:** blob, entity, entitylist, tree, commit

**Hashing:** SHA-256 with type-length envelope (`type len\0content`)

### Packages

| Package | Purpose |
|---------|---------|
| `pkg/object` | Content-addressed store with atomic writes and pack files |
| `pkg/entity` | Tree-sitter entity extraction and reconstruction |
| `pkg/diff3` | Myers diff + three-way line merge |
| `pkg/diff` | Entity-level diff computation |
| `pkg/merge` | Structural three-way merge orchestrator |
| `pkg/repo` | Repository operations (init, commit, branch, checkout, merge, rebase, stash, bisect, ...) |
| `pkg/coord` | Shared coordination state stored in `refs/coord/` |
| `pkg/coordd` | Local coordination daemon, governed execution, spawn, traces |
| `pkg/remote` | Remote sync, pack transport, and protocol client |
| `pkg/userconfig` | Global user configuration (`~/.graftconfig`) |

## Language support

Graft uses [gotreesitter](https://github.com/odvcencio/gotreesitter), a pure-Go tree-sitter runtime with 206 embedded grammars. Entity extraction is tested against:

- Go
- Python
- Rust
- TypeScript
- C

Release parser fuzzing covers the same tier-1 languages through:

```bash
go test -run x -fuzz FuzzExtractReconstructTier1 ./pkg/entity/
```

Any language with a tree-sitter grammar can be parsed. Declaration classification is extensible via node type maps.

## Status

Active development. Structural merge is already the foundation; coordination, sandboxing, and governed multi-agent runtime are the frontier being built directly into the VCS.

What exists:
- Content-addressed object store (SHA-256)
- Entity extraction via tree-sitter (206 languages)
- Three-way structural merge with entity-level resolution
- Set-union import merging
- Entity-level, line-level, and review-summary diff (`--entity`, `--review`)
- Branch-to-branch diff (`graft diff ref1..ref2`) with entity and JSON output
- Pack files with insert-only delta encoding (`graft gc`) and repository verification (`graft verify --json`)
- Full CLI: 50+ commands covering core workflows, branching, remotes, history, working tree, modules, LFS, and maintenance
- Stash workflow (push, pop, apply, list, drop, show)
- Rebase (standard, `--onto`, interactive, `--autostash`, conflict resolution with `--continue`/`--abort`/`--skip`)
- Cherry-pick at commit level and entity level (`--entity`), with `--continue`/`--abort`/`--skip`
- Revert with conflict resolution (`--continue`/`--abort`)
- Bisect with automated script runner (`bisect run`)
- Modules (`.graftmodules` + `.graftmodules.lock`) with branch tracking, shared object store, bidirectional development, merge-aware version resolution, and recursive fetch
- Multiple worktrees, sparse checkout, clean, shortlog, archive
- Batch blame: `graft blame <path>` attributes every entity in a file (`--json` for tooling)
- Entity search: `graft grep --entity <pattern>` finds entities by name across the repo (`--kind`, `--json`)
- SSH challenge/response auth for Orchard remotes
- Git forge clone support (GitHub, GitLab, Bitbucket shorthand)
- Large file storage (LFS) with pattern-based tracking
- `.graftignore` support
- Agent coordination with shared refs for plans, notes, tasks, claims, feed, and sessions
- `coordd` local daemon with governed exec/spawn, runtime profiles, snapshots, and decision traces

## Dependencies

- [gotreesitter](https://github.com/odvcencio/gotreesitter) — Pure-Go tree-sitter runtime (206 languages, no CGo)
- [cobra](https://github.com/spf13/cobra) — CLI framework
- [klauspost/compress](https://github.com/klauspost/compress) — Zstd compression for pack transport
- [golang.org/x/crypto](https://pkg.go.dev/golang.org/x/crypto) — SSH key parsing and challenge/response auth

## License

MIT
