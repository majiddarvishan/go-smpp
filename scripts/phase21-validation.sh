#!/usr/bin/env bash
# Phase 21 local validation matrix. Run with:
#   bash scripts/phase21-validation.sh
#
# Each gate is logged independently and failures do not stop later gates.
# The script never updates API snapshots, modifies source files, writes to
# .bench, or runs the 60-second Phase 17 reference acceptance.
set -o pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
EXPECTED_BRANCH="feature/product-neutral-library-dlr-completeness"
BRANCH="$(git -C "$ROOT" branch --show-current 2>/dev/null)"
if [[ "$BRANCH" != "$EXPECTED_BRANCH" ]]; then
  echo "BLOCKED: expected branch $EXPECTED_BRANCH; current branch is $BRANCH" >&2
  echo "Switch to the feature branch and pull its latest commit, then run this script again." >&2
  exit 2
fi
cd "$ROOT" || exit 2

LOCAL_HEAD="$(git rev-parse HEAD)"
UPSTREAM="$(git rev-parse --abbrev-ref --symbolic-full-name '@{u}' 2>/dev/null)"
UPSTREAM_HEAD=""
if [[ -n "$UPSTREAM" ]]; then
  UPSTREAM_HEAD="$(git rev-parse "$UPSTREAM" 2>/dev/null)"
  if [[ "$LOCAL_HEAD" != "$UPSTREAM_HEAD" ]]; then
    echo "BLOCKED: local HEAD does not match $UPSTREAM; run git pull --ff-only first." >&2
    echo "local=$LOCAL_HEAD upstream=$UPSTREAM_HEAD" >&2
    exit 2
  fi
fi

OUT="$SMPP_PHASE21_RESULTS_DIR"
if [[ -z "$OUT" ]]; then
  TMPBASE="$TMPDIR"
  if [[ -z "$TMPBASE" ]]; then TMPBASE="/tmp"; fi
  OUT="$TMPBASE/go-smpp-phase21-$(date -u +%Y%m%dT%H%M%SZ)-$$"
fi
if ! mkdir -p "$OUT"; then
  echo "FAIL: cannot create result directory $OUT" >&2
  exit 2
fi

{
  echo "timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "branch=$BRANCH"
  echo "head=$LOCAL_HEAD"
  echo "upstream=$UPSTREAM"
  echo "upstream_head=$UPSTREAM_HEAD"
  echo "default_go=$(go version 2>&1)"
  echo "kernel=$(uname -srmo)"
  echo "git_status:"
  git status --short --branch
} > "$OUT/metadata.txt"

SUMMARY="$OUT/summary.tsv"
printf 'status\tgate\tdetail\n' > "$SUMMARY"
LAST_LOG=""

record() {
  printf '%s\t%s\t%s\n' "$1" "$2" "$3" >> "$SUMMARY"
}

run_gate() {
  local gate="$1"
  shift
  local logfile="$OUT/$gate.log"
  LAST_LOG="$logfile"

  echo
  echo "===== $gate ====="
  "$@" > "$logfile" 2>&1
  local rc=$?
  cat "$logfile"

  if (( rc == 0 )); then
    echo "RESULT: PASS ($gate)"
    record PASS "$gate" "exit=0"
  else
    echo "RESULT: FAIL ($gate, exit=$rc)"
    record FAIL "$gate" "exit=$rc"
  fi
  return 0
}

check_api_and_nopanic_from_unit_log() {
  local version="$1"
  local logfile="$2"

  if grep -Eq '^--- PASS: TestExportedAPISnapshot([[:space:]]|$)' "$logfile"; then
    record PASS "$version-api-snapshot" "passed in full unit run; compile-time api_contract_test.go also compiled"
  elif grep -Eq '^--- FAIL: TestExportedAPISnapshot([[:space:]]|$)' "$logfile"; then
    record FAIL "$version-api-snapshot" "failed in full unit run"
  else
    record BLOCKED "$version-api-snapshot" "TestExportedAPISnapshot result not found in unit log"
  fi

  local missing=0
  local test_name
  for test_name in TestNoExportedEntryPointPanicsOnZeroValues TestNoPanicProbeCoversTheWholeAPI; do
    if grep -Eq "^--- PASS: $test_name([[:space:]]|$)" "$logfile"; then
      :
    elif grep -Eq "^--- FAIL: $test_name([[:space:]]|$)" "$logfile"; then
      missing=1
      record FAIL "$version-$test_name" "failed in full unit run"
    else
      missing=1
      record BLOCKED "$version-$test_name" "test result not found in unit log"
    fi
  done
  if (( missing == 0 )); then
    record PASS "$version-no-panic" "both no-panic and API-coverage probes passed in full unit run"
  fi
}

run_fuzz_gate() {
  local version="$1"
  local name="$2"
  local package="$3"
  local fuzz="$4"
  run_gate "$version-$name" env GOTOOLCHAIN="$version" go test "$package" -run '^$' -fuzz="^$fuzz$" -fuzztime=2s
}

# Use the CI-pinned linter. Install it outside the repository if needed.
LINTER=""
if command -v golangci-lint >/dev/null 2>&1 && golangci-lint version 2>&1 | grep -q '2\.14\.0'; then
  LINTER="$(command -v golangci-lint)"
else
  TOOL_DIR="$OUT-tools-$"
  if mkdir -p "$TOOL_DIR"; then
    run_gate "golangci-lint-install" env GOTOOLCHAIN=go1.27.1 GOBIN="$TOOL_DIR" go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
    if [[ -x "$TOOL_DIR/golangci-lint" ]]; then
      LINTER="$TOOL_DIR/golangci-lint"
    else
      record BLOCKED "golangci-lint-tool" "pinned golangci-lint v2.14.0 could not be installed; see install log"
    fi
  else
    record BLOCKED "golangci-lint-tool" "cannot create temporary tool directory"
  fi
fi

for version in go1.25.0 go1.27.0; do
  echo
  echo "################ TOOLCHAIN: $version ################"

  version_log="$OUT/$version-version.log"
  if ! env GOTOOLCHAIN="$version" go version > "$version_log" 2>&1; then
    cat "$version_log"
    record BLOCKED "$version-toolchain" "toolchain unavailable or automatic download failed"
    continue
  fi
  cat "$version_log"
  if ! grep -Fq "$version" "$version_log"; then
    record BLOCKED "$version-toolchain" "requested toolchain did not report the exact version"
    continue
  fi
  record PASS "$version-toolchain" "exact toolchain available"

  run_gate "$version-gofmt" env GOTOOLCHAIN="$version" bash -c '
    goroot="$(go env GOROOT)" || exit $?
    files="$("$goroot/bin/gofmt" -l .)"
    if [[ -n "$files" ]]; then
      echo "Unformatted Go files:"
      printf "%s\n" "$files"
      exit 1
    fi
    echo "All Go files are gofmt-clean."
  '

  run_gate "$version-unit" env GOTOOLCHAIN="$version" go test -count=1 -v ./...
  unit_log="$LAST_LOG"
  check_api_and_nopanic_from_unit_log "$version" "$unit_log"

  run_gate "$version-race" env GOTOOLCHAIN="$version" go test -race -count=1 ./...

  run_fuzz_gate "$version" fuzz-framer ./codec FuzzFramer
  run_fuzz_gate "$version" fuzz-decode-pdu ./codec FuzzDecodePDU
  run_fuzz_gate "$version" fuzz-framer-chunked ./codec FuzzFramerChunked
  run_fuzz_gate "$version" fuzz-decode-pdu50 ./codec FuzzDecodePDU50
  run_fuzz_gate "$version" fuzz-dlr ./message FuzzParseDeliveryReceipt

  run_gate "$version-protocol-codec-bench-smoke" env GOTOOLCHAIN="$version" go test -run '^$' -bench=. -benchmem ./protocol ./codec

  run_gate "$version-session-bench-smoke" env GOTOOLCHAIN="$version" go test -run '^$' -bench=BenchmarkDeadlineHighOutstanding -benchmem -benchtime=300ms ./session

  run_gate "$version-perflab-bench-smoke" env GOTOOLCHAIN="$version" SMPP_BENCH_SESSIONS=1,2,4,8 SMPP_BENCH_PARALLELISM=16 go test -run '^$' -bench='Benchmark(InMemorySession|LocalhostTCP|LocalhostTLS)Bidirectional' -benchmem -benchtime=200ms ./internal/perflab

  run_gate "$version-phase17-short-smoke" env GOTOOLCHAIN="$version" SMPP_ACCEPTANCE=1 SMPP_ACCEPTANCE_DURATION=2s SMPP_ACCEPTANCE_SESSION_COUNT=1 SMPP_ACCEPTANCE_CALLERS=64 SMPP_ACCEPTANCE_MIN_RPS=1 go test ./internal/perflab -run '^TestPhase17ReferenceAcceptance$' -count=1 -v

  run_gate "$version-no-unsafe" env GOTOOLCHAIN="$version" bash -c '
    set -o pipefail
    imports="$(go list -f "{{range .Imports}}{{.}}{{\"\\n\"}}{{end}}{{range .TestImports}}{{.}}{{\"\\n\"}}{{end}}{{range .XTestImports}}{{.}}{{\"\\n\"}}{{end}}" ./...)" || exit $?
    if printf "%s\n" "$imports" | grep -qx unsafe; then
      echo "Direct import of unsafe is not allowed."
      exit 1
    fi
    echo "No direct unsafe imports."
  '

  if [[ -n "$LINTER" ]]; then
    run_gate "$version-lint" env GOTOOLCHAIN="$version" "$LINTER" run --timeout=5m
  else
    record BLOCKED "$version-lint" "pinned golangci-lint v2.14.0 is unavailable"
  fi

  run_gate "$version-coverage-floor" env GOTOOLCHAIN="$version" bash scripts/coverage.sh
done

echo
echo "===== PHASE 21 VALIDATION SUMMARY ====="
cat "$SUMMARY"

PASS_COUNT="$(awk -F '\t' '$1=="PASS"{n++} END{print n+0}' "$SUMMARY")"
FAIL_COUNT="$(awk -F '\t' '$1=="FAIL"{n++} END{print n+0}' "$SUMMARY")"
BLOCKED_COUNT="$(awk -F '\t' '$1=="BLOCKED"{n++} END{print n+0}' "$SUMMARY")"
echo "PASS=$PASS_COUNT FAIL=$FAIL_COUNT BLOCKED=$BLOCKED_COUNT"
echo "The Phase 17 invocation above is a 2-second CI smoke only, not the 60-second reference acceptance."
echo "The .bench directory was not read, deleted, or modified by this script."
if [[ -n "$TOOL_DIR" && -d "$TOOL_DIR" ]]; then
  rm -rf "$TOOL_DIR"
fi

ARCHIVE="$OUT.tar.gz"
if tar -czf "$ARCHIVE" -C "$(dirname "$OUT")" "$(basename "$OUT")"; then
  echo "RESULT_ARCHIVE=$ARCHIVE"
else
  echo "FAIL: could not create result archive $ARCHIVE"
  exit 1
fi

if (( FAIL_COUNT > 0 )); then
  exit 1
fi
if (( BLOCKED_COUNT > 0 )); then
  exit 2
fi
exit 0
