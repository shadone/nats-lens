#!/usr/bin/env bash
# Runs every Go fuzz target for a bounded time, one after another.
# docs/operations/go-test-tiers.md describes it.
#
#   scripts/fuzz.sh [TIME]   fuzz each target for TIME (default 3m)
#   scripts/fuzz.sh list     print the targets, one "package name" per line
#   scripts/fuzz.sh check    fail when a Fuzz function is missing from TARGETS
#
# FUZZ_PARALLEL sets the workers per target (default 4). A failing input is
# written to the package's testdata/fuzz/<name>/ directory; the script runs
# the remaining targets and exits nonzero at the end.
set -euo pipefail
cd "$(dirname "$0")/.."

TARGETS="
./internal/api FuzzDecodeCursor
./internal/api FuzzEntityToken
./internal/api FuzzEpisodeCursor
./internal/api FuzzTimelineBuckets
./internal/api FuzzEntityKey
./internal/collect FuzzSnapshotApply
./internal/config FuzzParseDuration
./internal/config FuzzParseConfigFile
./internal/events FuzzDecodeEvent
./internal/eventselection FuzzFromURL
./internal/eventselection FuzzPageFor
./internal/investigation FuzzParseContext
./internal/investigation FuzzContextFromURL
"

check() {
	local found missing=0
	found=$(git grep -h -o -E '^func Fuzz[A-Za-z0-9_]+\(f \*testing\.F\)' -- '*_test.go' | sed -E 's/^func (Fuzz[A-Za-z0-9_]+).*/\1/' | sort)
	for name in $found; do
		if ! printf '%s\n' "$TARGETS" | grep -q -w -- "$name"; then
			echo "fuzz target $name is not in scripts/fuzz.sh TARGETS" >&2
			missing=1
		fi
	done
	return "$missing"
}

case "${1:-}" in
list)
	printf '%s\n' "$TARGETS" | sed '/^$/d'
	exit 0
	;;
check)
	check
	exit
	;;
esac

time=${1:-3m}
parallel=${FUZZ_PARALLEL:-4}
check
failed=""
while read -r pkg name; do
	[ -n "$pkg" ] || continue
	echo "== $pkg $name ($time, $parallel workers)"
	# Minimizing a new input holds up the other workers; with the default
	# 60 s per input a run sat idle for most of a minute at a time.
	if ! go test -run='^$' -fuzz="^${name}\$" -fuzztime="$time" -fuzzminimizetime=5s -parallel="$parallel" "$pkg"; then
		failed="$failed $pkg:$name"
	fi
done <<EOF
$TARGETS
EOF
if [ -n "$failed" ]; then
	echo "failed:$failed" >&2
	exit 1
fi
