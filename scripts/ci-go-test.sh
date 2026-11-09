#!/usr/bin/env bash
# Runs one share of the full Go suite in CI and reports how long it took.
#
#   scripts/ci-go-test.sh matrix               the shares as a GitHub Actions
#                                              matrix (JSON)
#   scripts/ci-go-test.sh share <pkg> <index>  the tests of <pkg> whose sorted
#                                              position modulo its share count
#                                              is <index>
#   scripts/ci-go-test.sh rest                 every package not in SPLIT
#
# Tests run with -count=1 and a 40-minute timeout per package, as in
# verify.py's go-full stage, but with go test's default -p: a CI runner has
# its cores to itself. The per-package and slowest-test times go to
# $GITHUB_STEP_SUMMARY (or stdout), the output of failed tests is printed,
# and the raw `go test -json` stream is kept in $GO_TEST_JSON when set.
set -euo pipefail

# Packages split by test name, with their share counts. The slowest packages
# go here: each share is one CI job. Shares take every nth test by sorted
# name, so changing a count, or adding or renaming a test in a split
# package, moves tests between shares; check the shares' times in the run
# summary afterwards. storeprov at 4 put its two slowest tests
# (TestStoreConformance, a fifth of the package, and TestFinal2_BusyPool)
# in one share, slower than at 3.
SPLIT="./internal/checks=4 ./internal/api/storeprov=5 ./internal/store=2"

usage() { echo "usage: $0 matrix | share <pkg> <index> | rest" >&2; exit 2; }

count_of() {
  local entry
  for entry in $SPLIT; do
    if [ "${entry%%=*}" = "$1" ]; then echo "${entry#*=}"; return; fi
  done
  echo "$1 is not in SPLIT" >&2
  exit 2
}

mode=${1:-}
case "$mode" in
  matrix)
    [ $# -eq 1 ] || usage
    {
      for entry in $SPLIT; do
        pkg=${entry%%=*} n=${entry#*=}
        for ((i = 0; i < n; i++)); do
          printf '{"name":"%s %d of %d","args":"share %s %d"}\n' "${pkg#./}" $((i + 1)) "$n" "$pkg" "$i"
        done
      done
      echo '{"name":"other packages","args":"rest"}'
    } | jq -c -s '{include: .}'
    exit 0
    ;;
  share)
    [ $# -eq 3 ] || usage
    pkg=$2 index=$3
    count=$(count_of "$pkg")
    all=$(go test -list '.*' "$pkg")
    names=$(printf '%s\n' "$all" | awk '/^(Test|Fuzz|Example)/' | LC_ALL=C sort |
      awk -v i="$index" -v n="$count" '(NR - 1) % n == i')
    if [ -z "$names" ]; then echo "no tests in share $index of $count of $pkg" >&2; exit 1; fi
    echo "share $index of $count of $pkg: $(printf '%s\n' "$names" | wc -l | tr -d ' ') tests"
    pattern="^($(printf '%s\n' "$names" | paste -sd'|' -))\$"
    args=(-run "$pattern" "$pkg")
    ;;
  rest)
    [ $# -eq 1 ] || usage
    module=$(go list -m)
    listed=$(go list ./...)
    skip=""
    for entry in $SPLIT; do p=${entry%%=*}; skip="$skip $module/${p#./}"; done
    args=()
    while read -r p; do
      case " $skip " in *" $p "*) ;; *) args+=("$p") ;; esac
    done <<< "$listed"
    if [ "${#args[@]}" -eq 0 ]; then echo "no packages left to test" >&2; exit 1; fi
    ;;
  *) usage ;;
esac

out=${GO_TEST_JSON:-$(mktemp)}
status=0
go test -json -count=1 -timeout 40m "${args[@]}" > "$out" || status=$?

summary=${GITHUB_STEP_SUMMARY:-/dev/stdout}
{
  echo "### Packages"
  echo
  echo "| Package | Result | Seconds |"
  echo "| --- | --- | ---: |"
  jq -r 'select(.Test == null and (.Action == "pass" or .Action == "fail" or .Action == "skip"))
    | "| \(.Package) | \(.Action) | \(.Elapsed // 0) |"' "$out" | LC_ALL=C sort -t'|' -k4 -rn
  echo
  echo "### Slowest tests"
  echo
  echo "| Test | Result | Seconds |"
  echo "| --- | --- | ---: |"
  # awk reads to the end: a reader that stops early (head) kills sort with
  # SIGPIPE, which pipefail turns into the script's exit status.
  jq -r 'select(.Test != null and (.Test | contains("/") | not) and (.Action == "pass" or .Action == "fail"))
    | [.Elapsed, "| \(.Package | sub(".*/"; "")).\(.Test) | \(.Action) | \(.Elapsed) |"] | @tsv' "$out" |
    LC_ALL=C sort -rn | awk -F'\t' 'NR <= 25 { print $2 }'
} >> "$summary"

# Build errors, and in each failed package the output of every test that did
# not pass or skip: a test stopped by the timeout panic gets no fail event.
jq -j -s '
  (map(select(.Action == "pass" or .Action == "skip") | {key: "\(.Package) \(.Test // "")", value: true}) | from_entries) as $ok
  | (map(select(.Action == "fail" and .Test == null) | {key: .Package, value: true}) | from_entries) as $failed
  | .[]
  | select(.Action == "build-output"
      or (.Action == "output" and $failed[.Package] and ($ok["\(.Package) \(.Test // "")"] | not)))
  | .Output' "$out"

exit "$status"
