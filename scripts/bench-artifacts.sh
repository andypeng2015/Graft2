#!/usr/bin/env sh
set -eu

out_dir="${1:-bench_out/$(date -u +%Y%m%dT%H%M%SZ)}"
count="${GRAFT_BENCH_COUNT:-10}"
gomaxprocs="${GRAFT_BENCH_GOMAXPROCS:-1}"
benchtime="${GRAFT_BENCH_TIME:-}"
repo_bench="${GRAFT_BENCH_REPO_PATTERN:-BenchmarkStatus_|BenchmarkIgnoreChecker|BenchmarkFlattenTree|BenchmarkBinaryCommitGraph}"
core_bench="${GRAFT_BENCH_CORE_PATTERN:-.}"
dry_run="${GRAFT_BENCH_DRY_RUN:-0}"

mkdir -p "$out_dir"

json_escape() {
	printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

generated_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
go_version="$(go version 2>/dev/null || printf 'unavailable')"
go_os="$(go env GOOS 2>/dev/null || printf 'unknown')"
go_arch="$(go env GOARCH 2>/dev/null || printf 'unknown')"
git_commit="$(git rev-parse HEAD 2>/dev/null || printf 'unknown')"
git_dirty="unknown"
if git diff --quiet --ignore-submodules -- 2>/dev/null; then
	git_dirty="false"
else
	git_dirty="true"
fi
cpu_model="unknown"
if [ -r /proc/cpuinfo ]; then
	cpu_model="$(awk -F: '/model name/ { sub(/^[ \t]+/, "", $2); print $2; exit }' /proc/cpuinfo)"
fi

cat > "$out_dir/metadata.json" <<EOF
{
  "generatedAt": "$(json_escape "$generated_at")",
  "goVersion": "$(json_escape "$go_version")",
  "goos": "$(json_escape "$go_os")",
  "goarch": "$(json_escape "$go_arch")",
  "gomaxprocs": "$(json_escape "$gomaxprocs")",
  "count": "$(json_escape "$count")",
  "benchtime": "$(json_escape "$benchtime")",
  "gitCommit": "$(json_escape "$git_commit")",
  "gitDirty": "$(json_escape "$git_dirty")",
  "cpuModel": "$(json_escape "$cpu_model")"
}
EOF

repo_cmd="GOMAXPROCS=$gomaxprocs go test ./pkg/repo -run '^$' -bench '$repo_bench' -benchmem -count=$count"
core_cmd="GOMAXPROCS=$gomaxprocs go test ./pkg/entity ./pkg/merge ./pkg/object ./pkg/diff3 -run '^$' -bench '$core_bench' -benchmem -count=$count"
if [ -n "$benchtime" ]; then
	repo_cmd="$repo_cmd -benchtime=$benchtime"
	core_cmd="$core_cmd -benchtime=$benchtime"
fi
repo_cmd="$repo_cmd -json > $out_dir/repo.json"
core_cmd="$core_cmd -json > $out_dir/core.json"

cat > "$out_dir/commands.txt" <<EOF
$repo_cmd
$core_cmd
EOF

if [ "$dry_run" = "1" ]; then
	printf 'dry-run: wrote benchmark artifact plan to %s\n' "$out_dir"
	exit 0
fi

export GOMAXPROCS="$gomaxprocs"

if [ -n "$benchtime" ]; then
	go test ./pkg/repo \
		-run '^$' \
		-bench "$repo_bench" \
		-benchmem \
		-count="$count" \
		-benchtime="$benchtime" \
		-json > "$out_dir/repo.json"

	go test ./pkg/entity ./pkg/merge ./pkg/object ./pkg/diff3 \
		-run '^$' \
		-bench "$core_bench" \
		-benchmem \
		-count="$count" \
		-benchtime="$benchtime" \
		-json > "$out_dir/core.json"
else
	go test ./pkg/repo \
		-run '^$' \
		-bench "$repo_bench" \
		-benchmem \
		-count="$count" \
		-json > "$out_dir/repo.json"

	go test ./pkg/entity ./pkg/merge ./pkg/object ./pkg/diff3 \
		-run '^$' \
		-bench "$core_bench" \
		-benchmem \
		-count="$count" \
		-json > "$out_dir/core.json"
fi

printf 'wrote benchmark artifacts to %s\n' "$out_dir"
