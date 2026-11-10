#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
MODE=accept
OUT=${1:-"$ROOT/.bench/phase17-acceptance"}
if [[ "${1:-}" == "--preflight" ]]; then
  MODE=preflight
  OUT=""
fi
DURATION=${SMPP_ACCEPTANCE_DURATION:-60s}
MIN_RPS=${SMPP_ACCEPTANCE_MIN_RPS:-100000}
CALLERS=${SMPP_ACCEPTANCE_CALLERS:-128}
SESSION_COUNTS=${SMPP_ACCEPTANCE_SESSIONS:-1,2,4}
TX_BATCH=${SMPP_BENCH_TX_BATCH:-32}

cd "$ROOT"

if [[ "$(uname -s)" != "Linux" || "$(uname -m)" != "x86_64" ]]; then
  echo "Phase 17 reference acceptance requires Linux/amd64" >&2
  exit 2
fi
if ! command -v go >/dev/null 2>&1; then
  echo "Phase 17 reference acceptance requires Go 1.27.x on PATH" >&2
  exit 2
fi
GO_VERSION=$(go env GOVERSION)
if [[ "$GO_VERSION" != go1.27* ]]; then
  echo "Phase 17 acceptance requires Go 1.27.x; found $GO_VERSION" >&2
  exit 2
fi

CORES=$(nproc)
MEM_KB=$(awk '/MemTotal:/ {print $2}' /proc/meminfo)
MIN_MEM_KB=$((10 * 1024 * 1024))
if (( CORES < 8 )); then
  echo "Phase 17 reference acceptance requires at least 8 logical CPUs; found $CORES" >&2
  exit 2
fi
if (( MEM_KB < MIN_MEM_KB )); then
  echo "Phase 17 reference acceptance requires at least 10 GiB RAM; found $((MEM_KB / 1024)) MiB" >&2
  exit 2
fi

# A previous reference result was obtained with local modifications, so the
# tested source could not be reproduced from its recorded commit alone.
# Refuse dirty source rather than treating that result as release evidence.
if [[ -n "$(git status --porcelain --untracked-files=normal)" ]]; then
  echo "Phase 17 reference acceptance requires a clean Git working tree. Commit or isolate local changes before the run." >&2
  exit 2
fi
SOURCE_COMMIT=$(git rev-parse HEAD)

export GOMAXPROCS=${GOMAXPROCS:-8}
export SMPP_BENCH_PARALLELISM=${SMPP_BENCH_PARALLELISM:-16}
export SMPP_BENCH_TX_BATCH="$TX_BATCH"

if [[ "$MODE" == "preflight" ]]; then
  printf 'PHASE17_PREFLIGHT=PASS commit=%s go=%s linux_amd64=true logical_cpus=%s mem_total_kib=%s duration=%s min_request_pdu_s=%s callers=%s session_candidates=%s tx_batch=%s gomaxprocs=%s\n' \
    "$SOURCE_COMMIT" "$GO_VERSION" "$CORES" "$MEM_KB" "$DURATION" "$MIN_RPS" "$CALLERS" "$SESSION_COUNTS" "$TX_BATCH" "$GOMAXPROCS"
  exit 0
fi

mkdir -p "$OUT"

{
  echo "timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "commit=$SOURCE_COMMIT"
  echo "source_clean=true"
  echo "go_version=$GO_VERSION"
  echo "kernel=$(uname -srmo)"
  echo "host_cpus=$CORES"
  echo "gomaxprocs=$GOMAXPROCS"
  echo "mem_total_kib=$MEM_KB"
  echo "duration=$DURATION"
  echo "min_request_pdu_s=$MIN_RPS"
  echo "callers=$CALLERS"
  echo "session_candidates=$SESSION_COUNTS"
  echo "tx_batch=$TX_BATCH"
} > "$OUT/environment.txt"

echo "Running race verification before the production-speed acceptance pass..."
go test -race ./session ./client ./server | tee "$OUT/race.txt"

IFS=',' read -r -a COUNTS <<< "$SESSION_COUNTS"
WINNER=""
for count in "${COUNTS[@]}"; do
  count=$(echo "$count" | xargs)
  [[ -z "$count" ]] && continue
  echo "=== Phase 17 sustained acceptance: sessions=$count ==="
  LOG="$OUT/acceptance-sessions-$count.txt"
  if SMPP_ACCEPTANCE=1       SMPP_ACCEPTANCE_DURATION="$DURATION"       SMPP_ACCEPTANCE_SESSION_COUNT="$count"       SMPP_ACCEPTANCE_CALLERS="$CALLERS"       SMPP_ACCEPTANCE_MIN_RPS="$MIN_RPS"       go test ./internal/perflab -run '^TestPhase17ReferenceAcceptance$' -count=1 -v 2>&1 | tee "$LOG"; then
    WINNER="$count"
    break
  fi
done

if [[ -z "$WINNER" ]]; then
  echo "No tested session count met the Phase 17 acceptance contract." >&2
  exit 1
fi

echo "$WINNER" > "$OUT/minimum-session-count.txt"
echo "Minimum tested passing session count: $WINNER"

echo "Capturing reference-host profiles for the passing configuration..."
SMPP_BENCH_SESSIONS="$WINNER" SMPP_BENCH_PARALLELISM="$SMPP_BENCH_PARALLELISM" SMPP_BENCH_TX_BATCH="$TX_BATCH" go test ./internal/perflab -run '^$'   -bench='BenchmarkLocalhostTCPBidirectional$'   -benchmem -benchtime=10s   -cpuprofile="$OUT/cpu.pprof"   -memprofile="$OUT/heap.pprof"   -mutexprofile="$OUT/mutex.pprof"   -blockprofile="$OUT/block.pprof"   | tee "$OUT/profile-benchmark.txt"

go tool pprof -top -nodecount=30 "$OUT/cpu.pprof" > "$OUT/cpu-top.txt"
go tool pprof -top -nodecount=30 "$OUT/heap.pprof" > "$OUT/heap-top.txt"
go tool pprof -top -nodecount=30 "$OUT/mutex.pprof" > "$OUT/mutex-top.txt"
go tool pprof -top -nodecount=30 "$OUT/block.pprof" > "$OUT/block-top.txt"

echo "Phase 17 reference artifacts written to $OUT"
