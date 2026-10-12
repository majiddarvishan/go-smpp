#!/usr/bin/env bash
# Coverage floor check (Task 6.1 / finding T1).
#
# Runs the test suite with coverage and fails if any package, or the module
# total, is below the minimum recorded in .coverage-floor. The floors live in
# version control on purpose: lowering one is a visible, reviewable change, not
# something that happens by accident when tests are deleted or a package grows
# untested code.
#
#   scripts/coverage.sh            check against .coverage-floor (exit 1 on failure)
#   scripts/coverage.sh --suggest  print measured values and suggested floors; changes nothing
#
# GOFLAGS is honoured, so `GOFLAGS=-race scripts/coverage.sh` checks under the
# race detector (needs cgo). See docs/COVERAGE.md.
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
FLOOR_FILE="$ROOT/.coverage-floor"
MARGIN=2        # points below the measured value that --suggest proposes
HEADROOM=8      # points above the floor at which we suggest raising it
cd "$ROOT"

MODE=check
case "${1:-}" in
  "") ;;
  --suggest) MODE=suggest ;;
  *) echo "usage: $0 [--suggest]" >&2; exit 2 ;;
esac

MODULE=$(go list -m)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

# In GitHub Actions the step log is hard to read from outside the web UI, so the
# outcome is also written as an annotation (visible on the run and through the
# API). Newlines are encoded as %0A, as workflow commands require.
annotate() { # level title message-lines...
  [[ -n "${GITHUB_ACTIONS:-}" ]] || return 0
  local level=$1 title=$2; shift 2
  local body=""
  local line
  for line in "$@"; do body+="${line}%0A"; done
  echo "::${level} title=${title}::${body}"
}
failing_lines() { grep -E '^(--- FAIL|FAIL|panic:|    .*_test\.go:[0-9]+:)' "$1" | head -12; }

# 1. Per-package coverage, from each package's own tests. The tests must pass.
if ! go test -count=1 -cover ./... >"$TMP/pkg.txt" 2>&1; then
  cat "$TMP/pkg.txt" >&2
  echo "coverage: tests failed; coverage is not meaningful until they pass" >&2
  mapfile -t failed < <(failing_lines "$TMP/pkg.txt")
  annotate error "coverage.sh: tests failed (first run)" "${failed[@]}"
  exit 1
fi
# "<pkg> <percent>" for every package that reports coverage with statements.
awk -v mod="$MODULE" '
  /coverage: [0-9.]+% of statements/ {
    pkg = ""; pct = ""
    for (i = 1; i <= NF; i++) {
      if ($i ~ "^" mod) pkg = $i
      if ($i == "coverage:") pct = $(i + 1)
    }
    sub("^" mod "/?", "", pkg); if (pkg == "") pkg = "."
    sub("%", "", pct)
    print pkg, pct
  }' "$TMP/pkg.txt" | sort >"$TMP/measured.txt"

# 2. Module total, counting cross-package coverage (a session test exercising
# codec counts for codec).
if ! go test -count=1 -coverpkg=./... -coverprofile="$TMP/total.out" ./... >"$TMP/total.txt" 2>&1; then
  cat "$TMP/total.txt" >&2
  echo "coverage: tests failed in the module-total run" >&2
  mapfile -t failed < <(failing_lines "$TMP/total.txt")
  annotate error "coverage.sh: tests failed (module-total run)" "${failed[@]}"
  exit 1
fi
TOTAL=$(go tool cover -func="$TMP/total.out" | awk '/^total:/ {sub("%", "", $3); print $3}')
echo "total $TOTAL" >>"$TMP/measured.txt"

if [[ "$MODE" == suggest ]]; then
  echo "# measured -> suggested floor (measured minus ${MARGIN} points, rounded down)"
  while read -r name pct; do
    printf '%-14s %6s -> %s\n' "$name" "$pct" "$(awk -v p="$pct" -v m="$MARGIN" 'BEGIN { v = int(p - m); if (v < 0) v = 0; print v }')"
  done <"$TMP/measured.txt"
  exit 0
fi

# 3. Compare with the floor file.
[[ -f "$FLOOR_FILE" ]] || { echo "coverage: $FLOOR_FILE is missing" >&2; exit 1; }
status=0
declare -A floor excluded measured
while read -r kind name rest; do
  [[ -z "${kind:-}" || "$kind" == \#* ]] && continue
  if [[ "$kind" == exclude ]]; then excluded[$name]=${rest:-}; else floor[$kind]=$name; fi
done <"$FLOOR_FILE"
while read -r name pct; do measured[$name]=$pct; done <"$TMP/measured.txt"

printf '%-14s %8s %8s  %s\n' package measured floor result
for name in "${!floor[@]}"; do
  if [[ -z "${measured[$name]:-}" ]]; then
    printf '%-14s %8s %8s  FAIL: in the floor file but not measured (package removed, renamed, or has no tests?)\n' "$name" - "${floor[$name]}"
    status=1
  fi
done
for name in $(printf '%s\n' "${!measured[@]}" | sort); do
  pct=${measured[$name]}
  if [[ -n "${excluded[$name]+x}" ]]; then continue; fi
  if [[ -z "${floor[$name]:-}" ]]; then
    printf '%-14s %8s %8s  FAIL: no floor set; add one (scripts/coverage.sh --suggest) or an explicit "exclude" line\n' "$name" "$pct" -
    status=1
    continue
  fi
  f=${floor[$name]}
  if awk -v p="$pct" -v f="$f" 'BEGIN { exit !(p + 0 < f + 0) }'; then
    printf '%-14s %8s %8s  FAIL: below the floor\n' "$name" "$pct" "$f"
    status=1
  elif awk -v p="$pct" -v f="$f" -v h="$HEADROOM" 'BEGIN { exit !(p + 0 >= f + h) }'; then
    printf '%-14s %8s %8s  ok (headroom >= %s points: consider raising the floor)\n' "$name" "$pct" "$f" "$HEADROOM"
  else
    printf '%-14s %8s %8s  ok\n' "$name" "$pct" "$f"
  fi
done

if (( status != 0 )); then
  echo "coverage: FAILED. Add tests, or lower a floor explicitly and say why in the commit (docs/COVERAGE.md)." >&2
fi
rows=()
for name in $(printf '%s\n' "${!measured[@]}" | sort); do
  rows+=("${name}: measured ${measured[$name]}, floor ${floor[$name]:-none}")
done
if (( status != 0 )); then
  annotate error "coverage.sh: floor not met" "${rows[@]}"
else
  annotate notice "coverage.sh: measured vs floor" "${rows[@]}"
fi
exit $status
