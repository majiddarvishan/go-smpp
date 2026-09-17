#!/usr/bin/env bash
set -euo pipefail
git diff --check --cached
go test ./...
go test -race ./...
go vet ./...
BENCHTIME=200ms SMPP_BENCH_SESSIONS=1,2,4,8 ./scripts/bench.sh /tmp/phase16-bench
printf '\n=== CPU profile top ===\n'
cat /tmp/phase16-bench/cpu-top.txt || true
printf '\n=== Heap profile top ===\n'
cat /tmp/phase16-bench/heap-top.txt || true
printf '\n=== Mutex profile top ===\n'
cat /tmp/phase16-bench/mutex-top.txt || true
printf '\n=== Block profile top ===\n'
cat /tmp/phase16-bench/block-top.txt || true
printf '\n=== Scheduler profile top ===\n'
cat /tmp/phase16-bench/sched-top.txt || true
printf '\n=== GC trace tail ===\n'
tail -n 20 /tmp/phase16-bench/gc-trace.txt || true
