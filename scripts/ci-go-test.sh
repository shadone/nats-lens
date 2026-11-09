#!/usr/bin/env bash
# Runs one share of the full Go suite in CI and reports how long it took.
#
#   scripts/ci-go-test.sh checks <index> <count>  the tests of internal/checks
#                                                 whose sorted position modulo
#                                                 <count> is <index>
#   scripts/ci-go-test.sh rest                    every other package
#
# Tests run with -count=1 and a 40-minute timeout per package, as `go test`
# in verify.py's go-full stage, but with the default -p: a CI runner has its
# cores to itself. The per-package and slowest-test times go to
# $GITHUB_STEP_SUMMARY (or stdout), and failed tests print their output.
set -euo pipefail

usage() { echo "usage: $0 checks <index> <count> | rest" >&2; exit 2; }

mode=${1:-}
case "$mode" in
  checks)
    [ $# -eq 3 ] || usage
    index=$2 count=$3
    names=$(go test -list '.*' ./internal/checks | awk '/^(Test|Fuzz|Example)/' | sort |
      awk -v i="$index" -v n="$count" '(NR - 1) % n == i')
    if [ -z "$names" ]; then echo "no tests in share $index of $count" >&2; exit 1; fi
    echo "share $index of $count: $(printf '%s\n' "$names" | wc -l | tr -d ' ') tests"
    pattern="^($(printf '%s\n' "$names" | paste -sd'|' -))\$"
    args=(-run "$pattern" ./internal/checks)
    ;;
  rest)
    [ $# -eq 1 ] || usage
    args=()
    while read -r pkg; do args+=("$pkg"); done < <(go list ./... | awk '!/\/internal\/checks$/')
    ;;
  *) usage ;;
esac

out=$(mktemp)
status=0
go test -json -count=1 -timeout 40m "${args[@]}" > "$out" || status=$?

summary=${GITHUB_STEP_SUMMARY:-/dev/stdout}
{
  echo "### Packages"
  echo
  echo "| Package | Result | Seconds |"
  echo "| --- | --- | ---: |"
  jq -r 'select(.Test == null and (.Action == "pass" or .Action == "fail" or .Action == "skip"))
    | "| \(.Package) | \(.Action) | \(.Elapsed // 0) |"' "$out" | sort -t'|' -k4 -rn
  echo
  echo "### Slowest tests"
  echo
  echo "| Test | Result | Seconds |"
  echo "| --- | --- | ---: |"
  jq -r 'select(.Test != null and (.Test | contains("/") | not) and (.Action == "pass" or .Action == "fail"))
    | [.Elapsed, "| \(.Package | sub(".*/"; "")).\(.Test) | \(.Action) | \(.Elapsed) |"] | @tsv' "$out" |
    sort -rn | head -n 25 | cut -f2-
} >> "$summary"

# Output of failed tests, failed packages (panics, timeouts) and build errors.
jq -j -s '
  (map(select(.Action == "fail")) | map({p: .Package, t: (.Test // "")}) | unique) as $failed
  | .[]
  | select(
      .Action == "build-output"
      or (.Action == "output" and ({p: .Package, t: (.Test // "")} as $k | any($failed[]; . == $k))))
  | .Output' "$out"

exit "$status"
