# Graft Performance Budgets

This document defines the benchmark fixtures, initial operation budgets, and
JSON artifact commands used to keep Graft predictable as repositories grow.

The numbers below are release gates, not marketing claims. They should be
tightened only after benchmark artifacts show stable headroom across supported
platforms.

## Fixture Sizes

| Fixture | Shape | Purpose |
| --- | --- | --- |
| Small | 100 tracked source files | PR smoke and developer laptop checks |
| Medium | 10,000 tracked files | Release-candidate status, add, checkout, grep, and merge checks |
| Large | 250,000 tracked files | Nightly scale check for status, pack, clone/fetch/push, and sparse checkout |
| Monorepo | Medium or large plus `vendor/`, generated code, and ignored output dirs | Ignore-policy and parser-skip validation |
| Binary/LFS | Source fixture plus large binary and LFS pointer paths | Large-file skip and pack streaming checks |

## Operation Budgets

Initial budgets are intentionally conservative until nightly artifacts establish
real baselines.

| Operation | Small | Medium | Large | Required artifact |
| --- | ---: | ---: | ---: | --- |
| `graft status` on clean tree | 250 ms | 2 s | 15 s | duration, allocs, file count |
| `graft add .` with warm parser cache | 2 s | 45 s | 15 min | duration, allocs, parsed/skipped count |
| `graft commit` after staged changes | 500 ms | 5 s | 60 s | duration, object count, ref writes |
| `graft checkout` clean branch | 1 s | 20 s | 10 min | duration, files updated |
| `graft merge` independent structural changes | 1 s | 30 s | 10 min | duration, confidence counts |
| `graft grep --entity` | 500 ms | 10 s | 2 min | duration, files scanned |
| `graft clone/fetch/push` | 5 s | 2 min | 30 min | duration, bytes, object count |

Release branches fail if an operation exceeds budget by more than 20 percent
against the previous accepted artifact unless a maintainer records an explicit
exception with the release notes.

## Existing Benchmarks

Current benchmark coverage includes:

- `pkg/repo`: status stat shortcut, ignore checker, tree flattening, commit graph.
- `pkg/entity`: extraction scaling.
- `pkg/merge`: structural and text-fallback merges.
- `pkg/object`: loose object, serialization, pack-index cache.
- `pkg/diff3`: diff and three-way merge.

## JSON Artifact Commands

Store artifacts under `bench_out/<date-or-build-id>/`. Use `GOMAXPROCS=1` for
comparison runs so scheduler variance does not hide regressions.

```bash
scripts/bench-artifacts.sh bench_out/local
```

The script writes:

- `metadata.json`
- `commands.txt`
- `repo.json`
- `core.json`

The JSON event streams are the canonical benchmark artifacts. Validate the
artifact contract without running benchmarks through dry-run mode:

```bash
GRAFT_BENCH_DRY_RUN=1 scripts/bench-artifacts.sh bench_out/dry-run
```

The script expands to these benchmark families by default:

```bash
GOMAXPROCS=1 go test ./pkg/repo \
  -run '^$' \
  -bench 'BenchmarkStatus_|BenchmarkIgnoreChecker|BenchmarkFlattenTree|BenchmarkBinaryCommitGraph' \
  -benchmem \
  -count=10 \
  -json > bench_out/<id>/repo.json

GOMAXPROCS=1 go test ./pkg/entity ./pkg/merge ./pkg/object ./pkg/diff3 \
  -run '^$' \
  -bench . \
  -benchmem \
  -count=10 \
  -json > bench_out/<id>/core.json
```

Human summaries can be derived from the same run:

```bash
go test ./pkg/repo -run '^$' -bench 'BenchmarkStatus_' -benchmem -count=10 \
  | tee bench_out/local/repo-status.txt
```

Compare two accepted artifact directories with:

```bash
go run ./scripts/bench-compare \
  -base bench_out/previous \
  -candidate bench_out/local \
  -max-regression 0.20
```

The comparator reads Go `-json` benchmark event streams, uses median values
when repeated samples are present, and fails when candidate `ns/op`, `B/op`, or
`allocs/op` exceeds the baseline by more than the configured threshold.

## Fixture Generation

Generate reproducible working-tree fixtures under `bench_fixtures/`:

```bash
scripts/generate-bench-fixture.sh small bench_fixtures/small
scripts/generate-bench-fixture.sh medium bench_fixtures/medium
scripts/generate-bench-fixture.sh large bench_fixtures/large
scripts/generate-bench-fixture.sh monorepo bench_fixtures/monorepo
scripts/generate-bench-fixture.sh binary-lfs bench_fixtures/binary-lfs
```

The script writes `metadata.json`, `commands.txt`, `.graftignore`, and fixture
source files. `monorepo` adds ignored `vendor/` and `generated/` trees.
`binary-lfs` adds a large binary and LFS pointer metadata. Validate the fixture
contract without creating source files through:

```bash
GRAFT_FIXTURE_DRY_RUN=1 scripts/generate-bench-fixture.sh large bench_fixtures/dry-run
```

Use `GRAFT_FIXTURE_FILES`, `GRAFT_FIXTURE_VENDOR_FILES`,
`GRAFT_FIXTURE_GENERATED_FILES`, and `GRAFT_FIXTURE_BINARY_MB` for local smoke
runs. Set `GRAFT_FIXTURE_OVERWRITE=1` to intentionally replace an existing
fixture directory.

## Required Release Artifacts

Every release candidate should attach:

- `repo.json`
- `core.json`
- `metadata.json`
- `commands.txt`
- a benchmark summary generated from the JSON streams
- platform metadata: OS, architecture, Go version, CPU model when available
- the previous accepted artifact used for comparison

## Nightly Performance Runs

`.github/workflows/performance.yml` runs on a nightly schedule and through
manual dispatch. It generates the `large` and `monorepo` fixtures, runs
`graft status`, `graft add .`, `graft commit`, and post-commit `graft status`
under `/usr/bin/time -v`, then uploads fixture metadata, timing output, and
package benchmark JSON artifacts. `scripts/time-summary` converts the raw
`/usr/bin/time -v` files into `timings.json` with elapsed seconds and peak RSS.

## Regression Review

When a benchmark regresses:

1. Confirm the regression repeats with `GOMAXPROCS=1` and `-count=10`.
2. Check whether the change affects correctness, safety, or only allocation
   shape.
3. If accepting the regression, record the reason in release notes.
4. If rejecting the regression, add a focused benchmark or test before fixing.

## Open Work

- Add operation-budget enforcement for fixture command timings.
