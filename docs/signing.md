# Graft Signing

Graft uses SSH keys for commit signatures, annotated tag signatures, and release
artifact signatures. This document is the stable operator contract for the
current formats.

## Key Material

Graft signing keys are OpenSSH Ed25519 keys.

```bash
graft auth setup --host https://orchard.dev
graft config --global signing.key ~/.ssh/id_ed25519
graft config --global signing.auto true
```

`graft auth setup` can generate a key at `.graft/signing_key` for local use.
Private keys are written with mode `0600`, and public keys are written as
OpenSSH `authorized_keys` lines with mode `0644`.

## Signature Encoding

Commit, tag, and release signatures use the same encoded SSH signature string:

```text
sshsig-v1:<algorithm>:<public-key-b64>:<signature-b64>
```

- `sshsig-v1` is Graft's current signature envelope version.
- `algorithm` is the SSH signature algorithm returned by the signer, normally
  `ssh-ed25519`.
- `public-key-b64` is the base64 encoding of the raw SSH public key bytes.
- `signature-b64` is the base64 encoding of the SSH signature blob.

The embedded public key lets Graft verify that a signature is internally
consistent before checking whether the key is trusted.

## Commit Payload

Commit signatures sign the canonical serialized commit object with the
`signature` header omitted.

The signed byte stream is produced by `object.CommitSigningPayload`. It is the
same deterministic text format used by `object.MarshalCommit`, except the
commit's `Signature` field is set to empty first.

Current header order:

```text
version 1
tree <tree-hash>
parent <parent-hash>
parent <parent-hash>
author <author>
timestamp <unix-seconds>
author_tz <offset>
committer <committer>
committer_timestamp <unix-seconds>
committer_tz <offset>

<commit message bytes>
```

Optional headers are omitted when their value is empty or zero. The `parent`
header repeats once per parent, preserving parent order. The message bytes are
not normalized.

The `signature` header is not part of the signed payload. This allows the final
commit object to carry the signature without making the signature sign itself.

## Commit Verification

Verify the current branch:

```bash
graft verify --signatures
graft verify --signatures --json
graft verify --signatures --require-signed --allowed-signers ./allowed_signers
```

Verify against an OpenSSH allowed-signers file:

```bash
graft verify commit <commit-hash> --allowed-signers ./allowed_signers
graft verify commit <commit-hash> --require-signed --allowed-signers ./allowed_signers
```

Allowed signers use one entry per line:

```text
alice@example.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA...
```

Graft first verifies the signature against the embedded public key. When
`--allowed-signers` is provided, the embedded public key must match one of the
trusted keys in that file.

By default, unsigned commits are advisory: they are reported in human and JSON
output, but they do not fail the command. Use `--require-signed` to make any
unsigned commit fail with Graft's verification-failure exit code. Invalid,
malformed, or untrusted signatures fail verification even when
`--require-signed` is not set.

## Tag Payload

Native Graft tag signatures apply to annotated tags. Lightweight tags have no
tag payload and are reported as unsigned.

Annotated tag signatures sign the tag annotation bytes with the native
`signature` header omitted. The signed payload is the same tag data stored in
the tag object:

```text
object <target-hash>
type <target-type>
tag <tag-name>
tagger <tagger> <unix-seconds> <timezone>

<tag message bytes>
```

Signed annotated tags add this header before the blank line:

```text
signature <sshsig-v1-signature>
```

Create and verify signed tags:

```bash
graft tag --annotate --message "release 1.0.0" --sign-key ~/.ssh/id_ed25519 v1.0.0
graft tag --verify v1.0.0 --allowed-signers ./allowed_signers
graft tag --verify v1.0.0 --require-signed --allowed-signers ./allowed_signers --json
```

`--sign` uses the configured `signing.key` when present, otherwise it falls
back to the default SSH private-key search path. `--sign-key` signs with the
explicit key path.

## Push Preflight

`graft push` can run the same signature policy before uploading objects:

```bash
graft push --require-signed --allowed-signers ./allowed_signers main
graft push --require-signed --allowed-signers ./allowed_signers refs/tags/v1.0.0
```

For branches, the push preflight checks the commits reachable from the pushed
ref. For tags, it checks the tag signature. Server-side protected-ref
enforcement still belongs in Orchard, but this local gate is suitable for CI and
operator workflows.

## Release Artifact Payload

Release signatures sign each artifact independently. The JSON signature report
declares:

```json
{
  "signatureFormat": "sshsig-v1",
  "payloadFormat": "graft-release-artifact-v1"
}
```

For each artifact, Graft signs this exact text payload:

```text
graft release artifact signature v1
path <clean-slash-path>
size <size-bytes>
sha256 <lowercase-sha256>
```

The path is cleaned with `filepath.Clean`, converted to slash separators, and
verified relative to the selected release base directory.

Create and verify release signatures:

```bash
graft release sign --sign-key ~/.ssh/id_ed25519 dist/graft
graft release verify-signature --allowed-signers ./allowed_signers signatures.json
```

## Rotation

Use overlapping trust windows for key rotation.

1. Generate or register the replacement signing key.
2. Add the new public key to every relevant `allowed_signers` file while keeping
   the old key trusted.
3. Set `signing.key` to the replacement key on machines and agents that create
   commits or release signatures.
4. Verify new commits and release artifacts with both old and new trust files in
   staging.
5. After all active release branches and automation use the new key, remove the
   old key from `allowed_signers`.

Historical signed commits remain verifiable only while their signing key remains
trusted by the verifier. Archive old public keys when long-term auditability is
required.

## Current Limits

- Protected-repository enforcement still requires callers to run
  `graft push --require-signed`, explicit verify commands, or equivalent hook
  policy until Orchard enforces protected refs server-side.
