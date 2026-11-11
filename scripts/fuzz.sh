#!/usr/bin/env bash
# Runs every Go fuzz target for a bounded time, one after another.
# docs/operations/go-test-tiers.md describes it.
#
#   scripts/fuzz.sh [TIME]   fuzz each target for TIME (default 3m)
#   scripts/fuzz.sh list     print the targets, one "package name" per line
#   scripts/fuzz.sh check    fail when TARGETS and the Fuzz functions disagree
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
	local found listed bad=0
	found=$(git grep -o -E '^func Fuzz[A-Za-z0-9_]+\([a-z]+ \*testing\.F\)' -- '*_test.go' | sed -E 's#^(.*)/[^/]*:func (Fuzz[A-Za-z0-9_]+).*#./\1:\2#' | sort)
	listed=$(printf '%s\n' "$TARGETS" | awk 'NF == 2 { print $1 ":" $2 }' | sort)
	for name in $found; do
		if ! printf '%s\n' "$listed" | grep -q -x -- "$name"; then
			echo "fuzz target $name (package:name) is not in scripts/fuzz.sh TARGETS" >&2
			bad=1
		fi
	done
	# go test -fuzz exits 0 with "no fuzz tests to fuzz" for a name that
	# matches no function, so a stale entry would pass every run.
	for name in $listed; do
		if ! printf '%s\n' "$found" | grep -q -x -- "$name"; then
			echo "scripts/fuzz.sh TARGETS names $name (package:name), which no test file there defines" >&2
			bad=1
		fi
	done
	return "$bad"
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
