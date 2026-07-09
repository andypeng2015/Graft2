# Graft Threat Model

This document describes the security assumptions and defensive posture for the
local Graft CLI, repository format, Git shadow, remote protocol, hooks, and
coordination runtime.

Graft is a local developer tool that reads and writes source trees. It must be
safe to inspect untrusted repositories, safe to use with sensitive credentials,
and explicit about features that execute code or trust remote state.

## Assets

- Source files, staged content, committed objects, refs, reflogs, and tags.
- Repository metadata under `.graft/`, including transactions, locks, gitmap,
  coordination refs, LFS pointers, modules, and hook configuration.
- The colocated `.git/` shadow repository.
- User configuration under `~/.graftconfig`, including Orchard identity and
  auth metadata.
- Signing keys referenced by user config or release commands.
- Coordination state: claims, feed events, plans, notes, sessions, presence,
  policy traces, and coordd execution traces.
- Remote credentials and bearer tokens passed through config, environment, or
  HTTP headers.

## Trust Boundaries

- Repository content is untrusted until the user chooses to trust that clone.
- Repo-local hooks and policy files are untrusted by default unless explicitly
  trusted through Graft configuration.
- Orchard/Graft remotes are authenticated peers, not automatically trusted for
  local execution or repository repair.
- The Git shadow is an interoperability mirror; Graft history remains
  authoritative.
- `coordd` governed execution crosses from repository metadata into process
  execution and must fail closed when policy cannot be evaluated.
- Support bundles cross from a local machine to a human or service operator and
  must redact secrets by default.

## Threats And Controls

### Malicious Repository Contents

Threats:

- Object, ref, reflog, index, gitmap, transaction, or coord-feed corruption
  causes silent data loss or misleading status.
- Large or malformed source files exhaust parser, memory, or status resources.
- Crafted source attempts to trigger parser crashes or lossy reconstruction.
- Path traversal or platform-specific path names escape the repository root.

Controls:

- `graft verify` validates object storage, refs, index references, reflogs,
  transactions, locks, Git shadow mapping, and coordination feed chains.
- `graft doctor --json` exposes diagnostics with severity, stable codes, and
  repair guidance.
- Entity extraction records parser diagnostics and preserves byte-for-byte
  reconstruction invariants.
- `FuzzExtractReconstructTier1` seeds release-gated fuzzing across Go, Python,
  Rust, TypeScript, and C parser/extraction paths.
- Structural merge confidence and parser diagnostics are surfaced in JSON.
- Atomic writes, repository locks, and transaction records protect critical
  metadata updates.
- Staging and tree flattening reject traversal paths, unclean path segments,
  Windows reserved device names, non-portable trailing dot/space segments, and
  case-folding path collisions.
- `graft add` rejects symlinks instead of following targets, and checkout/reset
  refuse to write through symlinked parent directories.
- `scripts/bench-artifacts.sh` produces release benchmark JSON streams,
  metadata, and command manifests for regression review.
- `scripts/generate-bench-fixture.sh` builds reproducible small, medium, large,
  monorepo, and binary/LFS working-tree fixtures for scale and ignore-policy
  checks.
- The nightly performance workflow generates large and monorepo fixtures, runs
  status/add/commit checks, captures `/usr/bin/time -v` output, and uploads
  structured timing/RSS summaries plus benchmark JSON artifacts for regression
  review.

Open requirements:
None currently tracked in this category.

### Malicious Remote

Threats:

- Remote responses exceed resource limits or contain malformed protocol data.
- A remote advertises unsupported capabilities or stale protocol versions.
- Fetch/clone accepts incomplete history or corrupt objects.
- Push partially advances refs after object upload failure.
- Remote URLs leak credentials through JSON, logs, traces, or support bundles.

Controls:

- The remote protocol contract is machine-readable through `graft protocol
  --json` and documented in `docs/protocol-spec.md`.
- Client response-size caps are enforced for advertised endpoints.
- Remote JSON and support bundles redact credentials and sensitive query
  parameters.
- `graft doctor` warns about manually edited or legacy non-local HTTP and
  `git://` remotes without leaking embedded credentials.
- Push-limit verification and advertised remote limits expose object-size
  boundaries.
- `graft remote add` and `graft remote set-url` reject non-local HTTP and
  `git://` remotes unless `--allow-insecure` is passed.
- Remote client tests cover mock-Orchard ref CAS rejection for stale push
  updates and preserve the structured conflict error.
- Mock-Orchard protocol conformance tests exercise every repository endpoint in
  the machine-readable contract across JSON compatibility and pack/zstd
  transports.
- Resumable pack upload support chunks zstd-compressed packs with per-chunk
  SHA-256, full-pack SHA-256, and retry-token propagation when the server
  advertises `resumable-pack`.
- `graft version --json` reports supported repository and protocol versions.

Open requirements:

- Add real Orchard/server integration concurrent-push CAS tests when the server
  implementation is in scope. The local client now covers stale-ref rejection
  against a mock Orchard server and the partial-upload case where refs must not
  advance after object upload failure.

### Hooks And Local Policy

Threats:

- Repo-local hooks execute attacker-controlled commands.
- Policy syntax errors accidentally allow governed execution.
- Hook trust decisions are unclear to users and support operators.

Controls:

- Hook trust is explicit in repository configuration.
- `graft doctor --bundle` reports hook trust state without including secrets or
  source by default.
- `coordd` sandbox probing is bounded and fails predictably.
- `graft coordd exec --check-only` evaluates policy without executing the
  command.
- Malformed repo-local action and spawn policies fail closed in evaluator and
  CLI check-only tests.
- Governed execution records decision traces for debugging and audit.
- `graft coordd spawn-trace --json --redact` exports support-safe policy traces
  with command arguments, environment values, local paths, and source contents
  omitted or redacted.

Open requirements:
None currently tracked in this category.

### Credential Leakage

Threats:

- Tokens in `~/.graftconfig`, remote URLs, auth headers, errors, JSON, traces,
  or support bundles are exposed to logs or issue reports.
- User config has permissions broad enough for other local users to read.
- Environment-provided credentials are accidentally persisted.

Controls:

- User config permission checks are included in global doctor output.
- Remote JSON and support bundles redact credentials and sensitive query
  parameters.
- Auth flows keep token display out of generic config get/list output.
- `graft auth doctor --json` reports token source, config permissions, and JWT
  expiry status without exposing token values.
- `GRAFT_TOKEN` and other credential environment variables override stored
  config for the current process without being persisted back to
  `~/.graftconfig`; README documents the precedence order.
- Support bundles exclude source and secrets by default and record the redaction
  policy in JSON.
- Shared redaction rules cover credential-bearing URLs, bearer/basic auth
  values, sensitive key-value assignments, command error display, support-bundle
  reflog reasons, collection errors, and coordd support traces.

Open requirements:
None currently tracked in this category.

### Signing And Supply Chain

Threats:

- Tampered commits, tags, release artifacts, or provenance statements are
  accepted as valid.
- Signing-key paths leak in support output.
- Old or rotated keys remain trusted without operator visibility.

Controls:

- Release commands generate manifests, checksums, SBOMs, provenance, and
  signatures.
- Release signature verification supports an allowed-signers file.
- `graft verify --signatures` verifies commit signatures on the current branch.
- `graft verify --signatures --require-signed` fails protected checks when any
  checked commit is unsigned, invalid, malformed, or signed by an untrusted key.
- `graft tag --verify <tag> --require-signed` verifies native annotated tag
  signatures and fails for unsigned lightweight tags in protected checks.
- `graft push --require-signed` runs the same local policy before uploading a
  protected branch or tag.
- Support bundle redaction covers signing-key paths.

Open requirements:

- Wire signature policy into Orchard/protected-branch enforcement rather than
  relying only on local CLI or hook checks.

### Sandbox Escape And Process Execution

Threats:

- A sandbox backend is unavailable and commands hang or silently run without the
  expected isolation.
- Repository metadata influences process execution outside policy.
- Traces record sensitive command arguments, environment, or output.

Controls:

- `coordd` sandbox probe timeouts are bounded.
- `graft coordd guard doctor --json` reports backend probe health, selected
  backend, explicit-backend failures, and `host-direct` isolation degradations.
- Governed execution policy is explicit and decision traces are recorded.
- `graft coordd spawn-trace --json --redact` exports support-safe traces with
  command arguments, environment values, local paths, and source contents
  omitted or redacted.
- Hooks are trust-gated separately from normal repository reads.

Open requirements:

None currently tracked in this category.

### Object Collision And Integrity Assumptions

Threats:

- An attacker creates two different objects with the same content hash.
- Stored object content is modified after being written.
- Pack indexes or loose-object envelopes lie about type or size.

Controls:

- Repository objects use SHA-256 content hashes.
- Object reads verify the object envelope and recompute the hash.
- Pack and pack-index verification is included in `graft verify`.
- Repository format metadata records the object hash algorithm.
- `repo.Open` rejects unsupported `object_hash` values, and
  `docs/object-hash.md` defines the SHA-256-only policy plus the required
  migration contract before any alternate hash algorithm can be introduced.

Open requirements:
None currently tracked in this category.

## Default Safe Behaviors

- Read-only commands should not execute repo-local hooks.
- Mutating commands should acquire repository locks and leave transaction
  records for ambiguous failures.
- Git shadow failures must not block Graft data writes, but they must remain
  visible through status, doctor, verify, and repair commands.
- JSON output that may be consumed by automation must include `schemaVersion`.
- Support bundles must omit secrets and source unless a user explicitly opts in
  with a future audited flag.

## Operator Checklist

Before trusting a cloned repository:

```bash
graft doctor --global
graft doctor --json
graft verify --json
graft status
graft coord check --json
```

Before sharing diagnostics:

```bash
graft doctor --bundle
```

Before trusting hooks or governed execution:

```bash
graft config hooks.trusted true
graft coordd exec --check-only -- <command>
```

Before release:

```bash
graft release check --version <version>
graft release manifest dist/
graft release sbom --name graft dist/
graft release provenance --builder-id https://builder.example/graft dist/
graft release sign --sign-key <key> dist/
graft release verify-signature --base-dir dist/ --allowed-signers <file> signatures.json
graft tag --verify v1.0.0 --require-signed --allowed-signers <file>
```
