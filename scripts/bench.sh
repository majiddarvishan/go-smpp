#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
OUT=${1:-"$ROOT/.bench"}
BENCHTIME=${BENCHTIME:-3s}
SESSIONS=${SMPP_BENCH_SESSIONS:-1}
PARALLELISM=${SMPP_BENCH_PARALLELISM:-16}
mkdir -p "$OUT"

cd "$ROOT"
{
  echo "timestamp_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "go_version=$(go version)"
  echo "goos=$(go env GOOS)"
  echo "goarch=$(go env GOARCH)"
  echo "gomaxprocs=${GOMAXPROCS:-default}"
  echo "sessions=$SESSIONS"
  echo "parallelism_multiplier=$PARALLELISM"
  uname -a
} > "$OUT/environment.txt"

# Codec-only baselines.
go test ./codec -run '^$' -bench 'Benchmark(DecodeHeader|FramerCompletePDU|ScanTLVs|EncodeSubmitSM|DecodeSubmitSM|EncodeDeliverSM|DecodeDeliverSM)$' -benchmem -benchtime="$BENCHTIME" \
  | tee "$OUT/codec.txt"

# Timer/window baselines, including high-outstanding deadline and liveness bookkeeping.
go test ./session -run '^$' -bench 'Benchmark(DeadlineHighOutstanding|LivenessActivityWithHighOutstanding|RequestWindowAcquireRelease|SessionMetricsSnapshot)' -benchmem -benchtime="$BENCHTIME" \
  | tee "$OUT/timers-window.txt"

# End-to-end in-memory, localhost TCP, and TLS-over-TCP request/response paths.
SMPP_BENCH_SESSIONS="$SESSIONS" SMPP_BENCH_PARALLELISM="$PARALLELISM" go test ./internal/perflab -run '^$' -bench 'Benchmark(InMemorySessionBidirectional|LocalhostTCPBidirectional|LocalhostTLSBidirectional)$' -benchmem -benchtime="$BENCHTIME" \
  | tee "$OUT/end-to-end.txt"

# One focused run captures CPU, heap, block/mutex contention, scheduler trace, and GC activity.
GODEBUG=gctrace=1 SMPP_BENCH_SESSIONS=1 SMPP_BENCH_PARALLELISM="$PARALLELISM" go test ./internal/perflab -run '^$' -bench 'BenchmarkInMemorySessionBidirectional/sessions_1$' -benchmem -benchtime="$BENCHTIME" \
  -cpuprofile "$OUT/cpu.pprof" \
  -memprofile "$OUT/heap.pprof" \
  -mutexprofile "$OUT/mutex.pprof" \
  -blockprofile "$OUT/block.pprof" \
  -trace "$OUT/trace.out" \
  > "$OUT/profile-benchmark.txt" 2> "$OUT/gc-trace.txt"

go tool pprof -top -nodecount=25 "$OUT/cpu.pprof" > "$OUT/cpu-top.txt" || true
go tool pprof -top -nodecount=25 "$OUT/heap.pprof" > "$OUT/heap-top.txt" || true
go tool pprof -top -nodecount=25 "$OUT/mutex.pprof" > "$OUT/mutex-top.txt" || true
go tool pprof -top -nodecount=25 "$OUT/block.pprof" > "$OUT/block-top.txt" || true
go tool trace -pprof=sched "$OUT/trace.out" > "$OUT/sched.pprof" || true
if [[ -s "$OUT/sched.pprof" ]]; then
  go tool pprof -top -nodecount=25 "$OUT/sched.pprof" > "$OUT/sched-top.txt" || true
fi

echo "benchmark artifacts written to $OUT"
