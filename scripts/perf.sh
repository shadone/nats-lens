#!/usr/bin/env bash
# Measures the API and the store against synthetic stores; see
# docs/operations/performance-baseline.md.
#
#   scripts/perf.sh                  both baseline profiles, all scenarios
#   PERF_PROFILES=smoke scripts/perf.sh   a two-minute check of the harness
#
# For each profile it generates a store, records the heaviest queries with
# EXPLAIN ANALYZE, runs the load scenarios and deletes the store.
#
#   PERF_PROFILES   profiles to run (default: "entities history")
#   PERF_DIR        where stores are generated (default: a new temporary dir)
#   PERF_OUT        where results go (default: .perf/<UTC time>)
#   PERF_DURATION   length of each concurrent scenario (default: 60s)
#   PERF_SCENARIOS  comma-separated scenarios (default: all)
#   PERF_KEEP=1     keep the generated stores
set -euo pipefail

cd "$(dirname "$0")/.."
profiles="${PERF_PROFILES:-entities history}"
out="${PERF_OUT:-.perf/$(date -u +%Y%m%dT%H%M%SZ)}"
dir="${PERF_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/natslens-perf.XXXXXX")}"
mkdir -p "$out" "$dir"
bin="$dir/natslens-perf"
go build -o "$bin" ./cmd/natslens-perf

# Bytes a profile's store needs, with room for the archive the mixed
# scenario exports beside it.
need() {
  case "$1" in
    entities) echo $((7 * 1024 * 1024 * 1024)) ;;
    history) echo $((6 * 1024 * 1024 * 1024)) ;;
    *) echo $((1024 * 1024 * 1024)) ;;
  esac
}

read -r -a list <<<"$profiles"
for p in "${list[@]}"; do
  free=$(($(df -Pk "$dir" | awk 'NR == 2 { print $4 }') * 1024))
  if [ "$free" -lt "$(need "$p")" ]; then
    echo "perf: $p needs $(($(need "$p") >> 30)) GiB free in $dir; $((free >> 30)) GiB are" >&2
    exit 1
  fi
  db="$dir/$p.duckdb"
  rm -f "$db" "$db.wal"
  echo "perf: $p: generating $db" >&2
  uptime >"$out/$p-environment.txt"
  "$bin" generate -profile "$p" -out "$db" >"$out/$p-generate.json"
  echo "perf: $p: explaining the heaviest queries" >&2
  "$bin" explain -profile "$p" -db "$db" -out "$out/$p-explain"
  echo "perf: $p: load scenarios" >&2
  args=(load -profile "$p" -db "$db" -duration "${PERF_DURATION:-60s}" -json "$out/$p-load.json")
  if [ -n "${PERF_SCENARIOS:-}" ]; then
    args+=(-scenarios "$PERF_SCENARIOS")
  fi
  "$bin" "${args[@]}" >"$out/$p-load.md"
  uptime >>"$out/$p-environment.txt"
  if [ "${PERF_KEEP:-}" != 1 ]; then
    rm -f "$db" "$db.wal"
  fi
done
echo "perf: results in $out" >&2
