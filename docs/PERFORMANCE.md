# Performance evidence

## Status

**The Phase 17 reference-machine acceptance has been run and passed** (maintainer's run, reported 2026-10-06,
`scripts/acceptance.sh`, which refuses to run unless the machine meets the contract). The result and resource
bounds are recorded below, taken from that run's console output.

**Still to fill in** from the run's `environment.txt`: the commit, Go version, kernel, memory and run date.
`docs/RELEASE_CHECKLIST.md` requires the exact environment and commit to be published before the first
production tag, so those cells are marked _not recorded_ rather than guessed. Keep the raw artifacts from
`.bench/phase17-acceptance` alongside this table.

## Result

| | |
| --- | --- |
| Verdict | **PASS** |
| Minimum passing session count | **1** (the first of 1, 2, 4 to pass; the others were not needed) |
| Sustained request PDUs/s | **713,932**, about 7.1 times the 100,000 requirement |
| Duration | 60 s |
| Requests / responses | 42,836,172 / 42,836,172 (equal) |
| Callers | 128 |
| Goroutines | baseline 8, maximum 136 (callers + 8) |
| Peak heap | 3 MiB (limit 1024) |
| Retained heap growth after GC | 0 MiB (limit 64) |
| Pending requests, maximum | 64 (window 64) |
| Race run (`session`, `client`, `server`) | ok; these three packages were reported as `(cached)`, i.e. an identical earlier passing run |
| CPU | 12th Gen Intel Core i5-12400 (the benchmark's `cpu:` line); `GOMAXPROCS` 8 |
| Diagnostic benchmark, same host | `BenchmarkLocalhostTCPBidirectional/sessions_1-8`: 765,084 request PDUs/s, 1,307 ns/op, 897 B/op, 9 allocs/op, tx batch 32 |

What this measures, so it is not over-read: one session over loopback TCP with required responses enabled and a
trivial handler. It shows the library's own per-PDU cost and its bounded memory under sustained load. It says
nothing about a real network, a real SMSC's processing time, or many sessions with different peers.

## The acceptance contract

The contract is encoded in `scripts/acceptance.sh` and `internal/perflab/acceptance_test.go`
(`TestPhase17ReferenceAcceptance`); this section only restates it.

Machine, checked by the script, which exits with status 2 if it is not met:

- Linux/amd64, at least 8 logical CPUs, at least 10 GiB of RAM, Go 1.26.x.
- `GOMAXPROCS=8`, `SMPP_BENCH_PARALLELISM=16`, `SMPP_BENCH_TX_BATCH=32` unless overridden.

Pass criteria, checked by the test over a sustained run (60 s by default):

| Criterion | Limit |
| --- | --- |
| Throughput, request PDUs completed per second, required responses enabled | at least 100,000 |
| Requests sent equals responses received | exact |
| Goroutines | at most baseline + callers + 32 (a fixed-worker bound) |
| Peak heap | at most 1024 MiB |
| Retained heap growth after a forced GC | at most 64 MiB |
| Pending requests and window in use | never above the configured window |

Session counts 1, 2 and 4 are tried in order and the first that passes is recorded as the minimum passing
session count; the script then captures CPU, heap, mutex and block profiles for that configuration.

## Running it

On the reference host:

```sh
scripts/acceptance.sh                      # writes artifacts under .bench/phase17-acceptance
scripts/acceptance.sh /path/to/outdir      # or somewhere else
```

Or, once CI is wired up, dispatch the `SMPP reference performance acceptance` workflow on a self-hosted
runner labelled `smpp-reference` (`.github/workflows/reference-acceptance.yml`).

Artifacts written to the output directory:

| File | Contents |
| --- | --- |
| `environment.txt` | timestamp, commit, Go version, kernel, CPUs, memory, and every parameter used |
| `race.txt` | `go test -race ./session ./client ./server` |
| `acceptance-sessions-N.txt` | the test log for each session count tried; contains the `PHASE17_RESULT` line |
| `minimum-session-count.txt` | the first passing session count |
| `profile-benchmark.txt`, `*.pprof`, `*-top.txt` | profiles of the passing configuration |

## Remaining fields

Copy these from `environment.txt` in the artifact directory.

| Field | Value |
| --- | --- |
| Commit | _not recorded_ |
| Date (UTC) | _not recorded_ |
| Go version | _not recorded_ |
| Kernel | _not recorded_ |
| Memory | _not recorded_ |

Once they are filled in, tick the reference-evidence items in `docs/RELEASE_CHECKLIST.md`.
