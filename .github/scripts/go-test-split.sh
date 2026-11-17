#!/usr/bin/env bash
# go-test-split.sh runs `go test` over a set of packages, with the tests of the
# slowest packages split into buckets that run as concurrent `go test`
# processes on the same runner.
#
#   SPLIT="./cli ./internal/fanout" BUCKETS=4 GO_TEST_FLAGS="-race -timeout 10m" \
#   COVER_OUT=coverage.out .github/scripts/go-test-split.sh <packages...>
#
# `go test` already runs packages in parallel, but each package's tests run
# serially in one process. The slow packages here are slow mostly by waiting
# (subprocesses, git, timers), not by CPU: cli used ~15% of a 4-core runner.
# Splitting one package's tests across processes is what fills the cores.
#
# Buckets are a round-robin over the package's top-level test names, read with
# `go test -list` from the same test binary the buckets then run, so every test
# the package compiles lands in exactly one bucket.
#
# SHARD=i/n spreads one package's buckets over n runners: this runner takes
# buckets i, i+n, i+2n, ... and only shard 1 runs the unsplit packages.
#
# SPLIT          packages (as `go list` accepts them) whose tests are bucketed
# BUCKETS        buckets per split package, across all shards (default 4)
# SHARD          i/n, this runner's share of the buckets (default 1/1)
# GO_TEST_FLAGS  flags for every `go test` invocation
# COVER_OUT      when set, write one merged coverage profile here
set -euo pipefail

buckets=${BUCKETS:-4}
shard=${SHARD:-1/1}
si=${shard%/*} sn=${shard#*/}
[[ $si =~ ^[0-9]+$ && $sn =~ ^[0-9]+$ && $si -ge 1 && $si -le $sn ]] || { echo "bad SHARD $shard" >&2; exit 2; }
read -r -a flags <<<"${GO_TEST_FLAGS:-}"
[ $# -gt 0 ] || { echo "usage: $0 <packages...>" >&2; exit 2; }

out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT

# Assigned first, not read through a process substitution, so a `go list`
# failure stops the script instead of leaving nothing to test.
pkgs=$(go list "$@")
splitpkgs=$( [ -z "${SPLIT:-}" ] || go list ${SPLIT})
mapfile -t all <<<"$pkgs"
split=()
[ -z "$splitpkgs" ] || mapfile -t split <<<"$splitpkgs"
rest=()
for p in "${all[@]}"; do
  [[ " ${split[*]} " == *" $p "* ]] || rest+=("$p")
done

cover=()
[ -z "${COVER_OUT:-}" ] || cover=(-cover)

jobs=()
run() { # run <label> <go test args...>
  local label=$1 n=${#jobs[@]}
  shift
  echo "$label" >"$out/$n.label"
  local prof=()
  [ -z "${COVER_OUT:-}" ] || prof=(-coverprofile="$out/$n.cover")
  go test "${flags[@]}" "${prof[@]}" "$@" >"$out/$n.log" 2>&1 &
  jobs+=($!)
}

# Started first so it overlaps the listing builds below.
[ ${#rest[@]} -eq 0 ] || [ "$si" -ne 1 ] || run "unsplit packages" "${rest[@]}"

for p in "${split[@]}"; do
  # Listing builds the test binary once, up front: concurrent `go test`
  # processes for one package would otherwise each compile it.
  list=$(go test "${flags[@]}" "${cover[@]}" -list '.*' "$p")
  mapfile -t names < <(grep -E '^(Test|Example|Fuzz)' <<<"$list" | sort)
  if [ ${#names[@]} -lt "$buckets" ]; then
    [ "$si" -ne 1 ] || run "$p" "$p"
    continue
  fi
  for ((b = si - 1; b < buckets; b += sn)); do
    mine=()
    for ((i = b; i < ${#names[@]}; i += buckets)); do mine+=("${names[i]}"); done
    run "$p [bucket $((b + 1))/$buckets, ${#mine[@]} tests]" -run "^($(IFS='|'; echo "${mine[*]}"))\$" "$p"
  done
done

status=0
for n in "${!jobs[@]}"; do
  if wait "${jobs[n]}"; then result=ok; else result=FAILED; status=1; fi
  echo "::group::$(cat "$out/$n.label"): $result"
  cat "$out/$n.log"
  echo "::endgroup::"
  [ "$result" = ok ] || { echo "::error::go test failed: $(cat "$out/$n.label")"; grep -E -- '^(--- FAIL|FAIL|panic:)' "$out/$n.log" || true; }
done

if [ -n "${COVER_OUT:-}" ]; then
  # One mode line, then every bucket's blocks; cover tooling sums a block that
  # appears in more than one profile.
  shopt -s nullglob
  profs=("$out"/*.cover)
  if [ ${#profs[@]} -gt 0 ]; then
    head -n1 "${profs[0]}" >"$COVER_OUT"
    for f in "${profs[@]}"; do tail -n +2 "$f" >>"$COVER_OUT"; done
  fi
fi
exit $status
