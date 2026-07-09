# Graft Object Hash Policy

Graft production repositories use SHA-256 object identifiers only.

The repository config records this as:

```json
{
  "repository_format_version": 1,
  "object_hash": "sha256"
}
```

`repo.Open` rejects any non-empty `object_hash` value other than `sha256`.
This is intentional: accepting an alternate algorithm before migration support
exists would create ambiguous object identity and interoperability failures.

## Upgrade Requirements

Before Graft introduces another object hash algorithm, the implementation must
ship all of the following in the same compatibility window:

- A new repository format version and explicit migration command.
- A multi-hash object identifier encoding that cannot be confused with current
  64-character SHA-256 hashes.
- Verification that checks both object envelope integrity and the configured
  algorithm.
- Pack, pack-index, entity-trailer, reflog, ref, git-shadow, remote protocol,
  LFS, release manifest, and signing payload compatibility notes.
- Downgrade and rollback behavior for repositories created before the
  migration.
- Protocol negotiation so old clients fail closed instead of accepting objects
  with unknown identity semantics.

Until those requirements are implemented and documented, `sha256` remains the
only production object hash.
