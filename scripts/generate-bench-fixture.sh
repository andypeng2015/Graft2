#!/usr/bin/env sh
set -eu

profile="${1:-small}"
out_dir="${2:-bench_fixtures/$profile}"
dry_run="${GRAFT_FIXTURE_DRY_RUN:-0}"
overwrite="${GRAFT_FIXTURE_OVERWRITE:-0}"

files=""
vendor_files="0"
generated_files="0"
binary_mb="0"

case "$profile" in
small)
	files="100"
	;;
medium)
	files="10000"
	;;
large)
	files="250000"
	;;
monorepo)
	files="10000"
	vendor_files="2000"
	generated_files="1000"
	;;
binary-lfs)
	files="100"
	binary_mb="64"
	;;
*)
	printf 'usage: %s [small|medium|large|monorepo|binary-lfs] [out-dir]\n' "$0" >&2
	exit 2
	;;
esac

files="${GRAFT_FIXTURE_FILES:-$files}"
vendor_files="${GRAFT_FIXTURE_VENDOR_FILES:-$vendor_files}"
generated_files="${GRAFT_FIXTURE_GENERATED_FILES:-$generated_files}"
binary_mb="${GRAFT_FIXTURE_BINARY_MB:-$binary_mb}"

if [ -d "$out_dir" ] && [ -n "$(find "$out_dir" -mindepth 1 -maxdepth 1 -print -quit)" ]; then
	if [ "$overwrite" != "1" ]; then
		printf 'fixture output exists and is not empty: %s\n' "$out_dir" >&2
		printf 'set GRAFT_FIXTURE_OVERWRITE=1 to replace it\n' >&2
		exit 1
	fi
	rm -rf "$out_dir"
fi

mkdir -p "$out_dir"

json_escape() {
	printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

generated_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

cat > "$out_dir/metadata.json" <<EOF
{
  "generatedAt": "$(json_escape "$generated_at")",
  "profile": "$(json_escape "$profile")",
  "sourceFiles": $files,
  "vendorFiles": $vendor_files,
  "generatedFiles": $generated_files,
  "binaryMegabytes": $binary_mb
}
EOF

cat > "$out_dir/commands.txt" <<EOF
graft init "$out_dir"
(cd "$out_dir" && graft add . && graft commit -m "benchmark fixture baseline" --author "Benchmark <bench@example.com>")
EOF

if [ "$dry_run" = "1" ]; then
	printf 'dry-run: wrote fixture plan to %s\n' "$out_dir"
	exit 0
fi

cat > "$out_dir/.graftignore" <<EOF
dist/
tmp/
*.log
EOF

if [ "$profile" = "monorepo" ]; then
	cat >> "$out_dir/.graftignore" <<EOF
vendor/
generated/
EOF
fi

if [ "$profile" = "binary-lfs" ]; then
	cat > "$out_dir/.gitattributes" <<EOF
assets/*.bin filter=lfs diff=lfs merge=lfs -text
EOF
fi

cat > "$out_dir/README.fixture.md" <<EOF
# Graft Benchmark Fixture

Profile: $profile
Source files: $files
Vendor files: $vendor_files
Generated files: $generated_files
Binary MiB: $binary_mb

Generated at: $generated_at
EOF

generate_go_files() {
	count="$1"
	prefix="$2"
	i=1
	last_shard=""
	while [ "$i" -le "$count" ]; do
		shard=$(( (i - 1) / 100 ))
		shard_name="$(printf 'pkg%04d' "$shard")"
		if [ "$shard_name" != "$last_shard" ]; then
			dir="$out_dir/$prefix/$shard_name"
			mkdir -p "$dir"
			last_shard="$shard_name"
		fi
		file="$dir/file$(printf '%06d' "$i").go"
		symbol="$(printf '%06d' "$i")"
		cat > "$file" <<EOF
package $shard_name

func Value$symbol() int {
	return $i
}
EOF
		i=$((i + 1))
	done
}

generate_plain_files() {
	count="$1"
	prefix="$2"
	extension="$3"
	i=1
	last_shard=""
	while [ "$i" -le "$count" ]; do
		shard=$(( (i - 1) / 100 ))
		shard_name="$(printf 'pkg%04d' "$shard")"
		if [ "$shard_name" != "$last_shard" ]; then
			dir="$out_dir/$prefix/$shard_name"
			mkdir -p "$dir"
			last_shard="$shard_name"
		fi
		file="$dir/file$(printf '%06d' "$i").$extension"
		printf 'fixture=%s index=%s shard=%s\n' "$profile" "$i" "$shard_name" > "$file"
		i=$((i + 1))
	done
}

generate_go_files "$files" "src"

if [ "$vendor_files" -gt 0 ]; then
	generate_plain_files "$vendor_files" "vendor/example.com/dependency" "txt"
fi

if [ "$generated_files" -gt 0 ]; then
	generate_plain_files "$generated_files" "generated/proto" "pb.go"
fi

if [ "$binary_mb" -gt 0 ]; then
	mkdir -p "$out_dir/assets" "$out_dir/lfs"
	dd if=/dev/zero of="$out_dir/assets/large.bin" bs=1048576 count="$binary_mb" 2>/dev/null
	oid="unknown"
	if command -v sha256sum >/dev/null 2>&1; then
		oid="$(sha256sum "$out_dir/assets/large.bin" | awk '{print $1}')"
	fi
	size=$((binary_mb * 1048576))
	cat > "$out_dir/lfs/large.bin.pointer" <<EOF
version https://git-lfs.github.com/spec/v1
oid sha256:$oid
size $size
EOF
fi

printf 'wrote %s benchmark fixture to %s\n' "$profile" "$out_dir"
