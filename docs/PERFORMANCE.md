# Performance evidence

## Status

**Historical reference-machine acceptance PASS (2026-10-06):** the result below comes from the maintainer's older `scripts/acceptance.sh` run. **A newer 2026-11-10 clean-candidate acceptance is now PASS and documented first below.** The 2026-10-06 run remains historical and does not verify the candidate by itself. The historical result, resource bounds and environment are documented below.

**Reference toolchain: Go 1.27.x.** The run used go1.27.0. The maintainer confirmed that he had modified `scripts/acceptance.sh` locally for the run, and
said he does not want Go 1.26 as the reference, so the edit was presumably the toolchain check, which then required Go 1.26.x. The
committed script now requires `go1.27*`, so it matches the run. Two things this does not settle: the local edit itself was
not seen, so that nothing besides the version check differed (thresholds, duration, session candidates) is an inference, though
the parameters in `environment.txt` match the script's defaults; and the recorded commit predates the script change, so a reader
reproducing the run should use the current script.

## Current-candidate reference acceptance — 2026-11-10

**PASS on clean candidate `ebf28340fbe7a49b9ab6a8a0a9a34d7ba924dee9`**, based on the maintainer's terminal output from `scripts/acceptance.sh --preflight` and `scripts/acceptance.sh .bench/phase17-reference`. This is the required 60-second reference-host run, not the older historical result and not a hosted-CI smoke.

| Measurement | Current candidate |
| --- | --- |
| Preflight | PASS, exact commit above; Linux/amd64; go1.27.0 |
| Reference host | 12 logical CPUs, 32,649,508 KiB RAM, `GOMAXPROCS=8` |
| Configuration | 60 s, 128 callers, TX batch 32, session candidates 1 / 2 / 4, minimum 100,000 request PDUs/s |
| Race gate | PASS for `session`, `client`, `server` |
| Sustained acceptance | PASS, 683,465 request PDUs/s (6.83x target) |
| Minimum tested passing sessions | **1**; 2 and 4 not required |
| Timed completed count | 41,008,141 |
| Requests / responses | 41,008,269 / 41,008,269 (equal) |
| Goroutines | baseline 8; maximum 136 |
| Heap | peak 3 MiB; retained growth 0 MiB |
| Pending / window maximum | 64 / 64 |
| Diagnostic benchmark | 723,710 request PDUs/s, 1,382 ns/op, 896 B/op, 9 allocs/op, sessions=1 |
| Output directory | `.bench/phase17-reference` (ignored by Git) |

The timed `completed` count is sampled at the 60-second deadline, before workers finish their current requests, whereas aggregate `requests` and `responses` counters are read after the workers join (`internal/perflab/acceptance_test.go`). The extra 128 matched request/response pairs in this result are therefore consistent with the in-flight drain, **not** a response-loss finding. The reference acceptance rate uses only the timed completed count.

The runner reported `Phase 17 reference artifacts written to .bench/phase17-reference`. The performance result and clean-tree preflight are supported by the operator's output; **the generated `environment.txt`, `minimum-session-count.txt`, race log and profiles have not been independently inspected or archived in this review**. Inspect those local artifacts and record exact kernel/date/profile provenance before release tagging. No production implementation change is required from this acceptance result.

## Historical result — 2026-10-06

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
| Diagnostic benchmark, same host | `BenchmarkLocalhostTCPBidirectional/sessions_1-8`: 765,084 request PDUs/s, 1,307 ns/op, 897 B/op, 9 allocs/op, tx batch 32 |

What this measures, so it is not over-read: one session over loopback TCP with required responses enabled and a
trivial handler. It shows the library's own per-PDU cost and its bounded memory under sustained load. It says
nothing about a real network, a real SMSC's processing time, or many sessions with different peers.

## The acceptance contract

The contract is encoded in `scripts/acceptance.sh` and `internal/perflab/acceptance_test.go`
(`TestPhase17ReferenceAcceptance`); this section only restates it.

Machine, checked by the script, which exits with status 2 if it is not met:

- Linux/amd64, at least 8 logical CPUs, at least 10 GiB of RAM, Go 1.27.x.
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

On the Linux/amd64 reference host, check out the exact candidate commit with a clean Git working tree:

```sh
scripts/acceptance.sh --preflight          # read-only; no benchmark or files
scripts/acceptance.sh .bench/phase17-reference
```

The read-only preflight checks Linux/amd64, Go 1.27.x, 8+ logical CPUs, 10+ GiB memory, and a clean Git tree; it emits `PHASE17_PREFLIGHT=PASS` plus the commit and selected parameters or exits 2. The full run rechecks these constraints, records `source_clean=true` and the exact commit in `environment.txt`, and writes the 60-second performance evidence to the ignored `.bench/` directory. Diagnostic runs below 60 seconds do **not** satisfy release acceptance.

The optional `SMPP reference performance acceptance` GitHub Actions workflow supports manual dispatch against the candidate branch when a self-hosted runner labeled `smpp-reference` is registered and online. Hosted-runner smoke is not equivalent. PR #8 does not need to be merged before testing the candidate; if integration changes executable source, rerun the reference gate on that integrated commit.

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

## Historical environment — 2026-10-06

From `environment.txt` of the run.

| Field | Value |
| --- | --- |
| Commit | `c0f699c09bdc572b7e5e500ea8f2ac2c53f58135` (the working tree may have differed: see the deviation above) |
| Date (UTC) | 2026-10-06T19:59:40Z |
| Go version | go1.27.0 |
| Kernel | Linux 6.8.0-139-generic x86_64 GNU/Linux |
| CPUs | 12 logical (12th Gen Intel Core i5-12400); `GOMAXPROCS` 8 |
| Memory | 32,649,520 KiB (about 31.1 GiB) |
| Parameters | duration 60 s, 128 callers, session candidates 1, 2, 4, tx batch 32, minimum 100,000 request PDUs/s |

The recorded commit predates two later commits, `3b9e685` (`go.mod` to `go 1.25`) and documentation-only changes; no
library source changed between them.
