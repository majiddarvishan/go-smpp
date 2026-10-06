# Performance evidence

## Status

**The Phase 17 reference-machine result has not been recorded yet.** No throughput or resource-bound figure
in this file is a measurement, and none should be quoted as one until the table below is filled in from a
run on the reference machine. Task 6.4 stays open until then.

What exists today is development-machine validation (the README mentions more than 100k request PDUs/s on
a single localhost session). That is diagnostic only. It is not the acceptance result, because it was not
taken on the documented reference hardware, and this repository's own acceptance script refuses to run
anywhere else (see below).

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

## Recording the result

Copy the values from the `PHASE17_RESULT` line of the passing `acceptance-sessions-N.txt` and from
`environment.txt`, then replace the placeholders below. Keep the raw artifacts too (an attached archive or a
release asset is fine); a table with no source files behind it is only a claim.

| Field | Value |
| --- | --- |
| Commit | _not recorded_ |
| Date (UTC) | _not recorded_ |
| Go version | _not recorded_ |
| Kernel | _not recorded_ |
| CPUs / memory | _not recorded_ |
| Minimum passing session count | _not recorded_ |
| Sustained request PDUs/s (`request_pdu_s`) | _not recorded_ |
| Duration | _not recorded_ |
| Callers | _not recorded_ |
| Goroutines, baseline and max | _not recorded_ |
| Peak heap, MiB (`heap_peak_mib`) | _not recorded_ |
| Retained heap growth, MiB (`heap_retained_growth_mib`) | _not recorded_ |
| Pending max / window max | _not recorded_ |
| Race run | _not recorded_ |

Once the table is filled in, update the README paragraph that lists the reference result as a remaining
blocker, and tick the corresponding line in `docs/RELEASE_CHECKLIST.md`.
