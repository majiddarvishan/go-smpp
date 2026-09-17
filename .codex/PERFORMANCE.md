# Performance Contract and Benchmark Strategy

Performance is a first-class requirement and must be measured throughout implementation, not only before release.

## Reference target

Reference platform:

- Linux/amd64
- 8 CPU cores
- 10 GB RAM

Throughput target:

> Sustain **100,000 aggregate bidirectional SMPP request PDUs per second**, where aggregate request rate is `requests sent + requests received`.

Examples that satisfy the request-count definition:

- 100k outbound request/s and 0 inbound request/s
- 50k outbound request/s and 50k inbound request/s
- 20k outbound request/s and 80k inbound request/s

Required SMPP responses are not counted as request throughput, but they are mandatory processing and must be present in end-to-end benchmarks. Therefore a 100k request/s scenario can require roughly another 100k response PDUs/s, and additional traffic such as enquire-link or receipts can raise total PDU rate further.

## Connection-count goal

The requirement is to meet the target with the **minimum practical number of SMPP sessions/connections**.

Benchmark order:

1. one session/connection
2. two sessions if one is insufficient
3. four sessions if two are insufficient
4. increase only when measurement requires it

Final benchmark reports must state the smallest session count that sustained the target under the documented scenario.

Do not promise that an arbitrary real SMSC can do 100k requests/s on one connection. Peer-side window limits, throttling, RTT and network capacity are external constraints.

## Why window/RTT matters

For a request/response workload, a useful lower-bound approximation is:

```text
required outstanding window ~= request_rate * response_RTT_seconds
```

Examples at 100k request/s:

```text
1 ms RTT   -> ~100 outstanding
10 ms RTT  -> ~1,000 outstanding
50 ms RTT  -> ~5,000 outstanding
100 ms RTT -> ~10,000 outstanding
```

This is a planning approximation, not an SMPP protocol limit. Benchmarks must measure real behavior.

## Timeout performance contract

Timeout processing is part of the hot path, not an administrative side feature.

The library must support:

- configurable per-request protocol response deadlines,
- Session Init timeout,
- Enquire Link scheduling and response timeout,
- inactivity timeout,
- caller context cancellation/deadlines.

The final high-throughput design must not require one independent `time.Timer` or goroutine per outstanding request. Deadline tracking must remain bounded by configured outstanding work.

Benchmark both normal and adverse cases:

- nearly all requests completing before timeout,
- sparse timeouts at steady load,
- large timeout storms,
- response and timeout occurring at the same boundary,
- session loss while many timeout records are active,
- late responses after expiry.

Measurements must include deadline-insert, completion/removal and expiry cost, plus memory retained per outstanding deadline.

## Concurrency/thread-safety performance contract

Concurrency correctness is mandatory and should not be achieved by serializing the whole session behind one coarse global lock if that prevents the throughput target.

Measure contention for:

- sequence allocation,
- pending request insert/complete/expire,
- session state reads/transitions,
- window acquire/release,
- TX serialization,
- timer/deadline updates,
- concurrent `Close`/timeout/session-loss paths.

Run dedicated correctness tests under `go test -race`. Race-detector throughput is not the production performance target; its purpose is concurrency verification.

## Benchmark layers

### 1. Primitive/codec microbenchmarks

Measure:

- header encode/decode
- C-Octet scanning
- TLV scanning and typed lookup
- `submit_sm` encode/decode
- `deliver_sm` encode/decode
- allocation count and bytes/op

Goal: identify codec regressions without involving networking.

### 2. Correlation/window/timer benchmarks

Measure:

- pending insert/complete/timeout throughput
- contention at realistic outstanding counts
- window acquisition/release cost
- out-of-order completion patterns
- deadline scheduling/removal cost
- timeout storms
- exactly-once completion under response/timeout/cancel races

### 3. In-memory session benchmark

Use in-memory/full-duplex transports to measure session machinery without kernel TCP cost.

### 4. Localhost TCP benchmark

Measure realistic framing, syscalls and scheduler behavior with a minimal peer simulator.

### 5. TLS benchmark

Run separately so TLS cost is visible rather than mixed with plain-TCP baseline.

### 6. Bidirectional benchmark

Simultaneously originate requests in both directions. This is the acceptance-relevant workload because the target explicitly includes requests sent and received at the same time.

## Required workload profiles

At minimum:

- outbound submit-heavy
- inbound submit-heavy in server mode
- balanced bidirectional request traffic
- outbound `deliver_sm` + inbound `submit_sm` in server TRX-style operation where applicable
- response arrival in order
- response arrival deliberately out of order
- low RTT / high RTT simulation
- small and large outstanding windows
- plain TCP and TLS
- concurrent callers sharing the same active session
- sparse response timeouts
- timeout storm
- Enquire Link on otherwise idle healthy sessions
- unresponsive peer causing Enquire Link failure
- Session Init timeout for unbound connections
- inactivity timeout behavior

## Allocation goals

The initial optimization target for common codec paths is 0–2 allocations/PDU where practical. This is a target, not a correctness criterion.

At 100k request/s plus responses, even small per-PDU allocation counts can produce large allocation rates. Every benchmark report should include:

- allocs/op
- bytes/op
- heap profile under sustained load
- GC frequency/pause contribution

Timeout/deadline bookkeeping must also report its per-request allocation behavior.

## Memory goals

Memory use must remain bounded by configuration and active workload. Track at least:

- receive buffers
- transmit buffers
- pending requests
- queued handler work
- timer/deadline records
- pooled objects/buffers

Avoid pools that retain arbitrarily large buffers indefinitely. Expired/cancelled/completed requests must not leave deadline or payload references retained indefinitely.

## CPU and contention goals

Profile:

- CPU samples by package/function
- mutex/block profile
- scheduler/goroutine count
- syscall contribution
- timer/deadline subsystem CPU
- GC CPU

Do not replace a simple correct implementation with a complex one until a profile demonstrates the bottleneck.

## Goroutine policy

The library must not create a goroutine per message/request. Benchmark runs should record goroutine count during sustained load and verify it remains tied to sessions/workers rather than message volume.

Timeout support must not introduce goroutine-per-deadline behavior.

## Logging policy

Performance tests run with per-PDU logs disabled. Optional tracing must be benchmarked separately and is not part of the base acceptance target.

## Acceptance criteria for the 100k milestone

All of the following must be true on the reference machine:

- sustained >= 100,000 aggregate bidirectional **request** PDUs/s for the defined run duration
- required request/response processing is enabled and successful
- session count is documented and is the smallest count found by the benchmark sequence
- no unbounded memory growth
- no unbounded goroutine growth
- configured response-timeout tracking is enabled and bounded
- timeout/cancel/response/session-loss races preserve exactly-once completion and window accounting
- concurrent public API use is race-free in dedicated race-detector tests
- error/timeout rate is within the benchmark's declared success threshold
- CPU, memory, GC and contention profiles are captured
- exact build version, Go version, kernel/environment and benchmark configuration are recorded

The sustained run duration and acceptable error threshold will be fixed before Phase 17 and added here.

## Performance regression policy

Once stable baselines exist, CI or scheduled benchmark reports should flag meaningful regressions in:

- ns/op
- allocs/op
- bytes/op
- throughput
- p50/p95/p99 response latency
- timeout scheduling/completion overhead
- mutex/block contention
- CPU usage
- memory at a fixed outstanding window

Exact regression thresholds are deferred until stable benchmark noise is measured.

## Phase 9 timer baseline

The first response-deadline implementation uses a cancellable min-heap with one reusable timer and one long-lived goroutine per session. It intentionally avoids a `time.Timer`/goroutine per request. Completed/cancelled entries are removed immediately rather than retained until their original expiry.

A local Linux/amd64 development benchmark on an AMD EPYC 9V74 (not the final 8-core/10-GB acceptance machine) measured the initial heap baseline at roughly 109 ns/op and 1 allocation for schedule+cancel; the timeout-storm benchmark exercises batched expiry. These values are implementation baselines, not Phase 17 acceptance results. Heap vs alternative shared deadline structures can be revisited only if end-to-end profiling shows timer bookkeeping is material.

## Phase 16 performance laboratory methodology

Phase 16 establishes a reproducible measurement harness before acceptance tuning. The benchmark implementation deliberately keeps the Phase 17 acceptance claim separate from development-run measurements.

The laboratory contains four layers:

1. **Codec-only**: fixed header, complete-frame framer, TLV scanning, and typed `submit_sm` / `deliver_sm` encode/decode with `-benchmem`.
2. **Session/timer primitives**: request-window acquire/release, 10,000-outstanding response-deadline schedule/cancel, liveness activity accounting while the deadline heap is populated, and lock-free metrics snapshots.
3. **Bidirectional end-to-end**: one ESME and one SMSC session exchange `submit_sm` / `submit_sm_resp` and `deliver_sm` / `deliver_sm_resp` concurrently over `net.Pipe`, localhost TCP, and TLS-over-TCP. Required response PDUs are included in the measured work but only request PDUs are reported in `request_pdu/s`.
4. **Profiles**: a focused one-session in-memory run captures CPU, heap, mutex, block, Go scheduler trace, and GC trace. `scripts/bench.sh` writes the raw profiles and human-readable `pprof -top` summaries to a chosen output directory.

The default end-to-end run uses one session. `SMPP_BENCH_SESSIONS` accepts a comma-separated list such as `1,2,4` or `1,2,4,8`; this permits the laboratory to add connection counts only when the preceding measurement is insufficient. `BENCHTIME` controls the per-case Go benchmark duration.

The benchmark script is intended to run unchanged on Go 1.26.x/Linux in CI or on dedicated benchmark hosts. CI-runner numbers are useful regression baselines but are **not** the Phase 17 reference-machine acceptance result.

The minimal `cmd/smpp-sim` executable is an SMSC-side high-throughput peer for external/local load generation. It uses the same server/session core, accepts SMPP 3.4 binds, responds to `submit_sm`, handles normal Enquire Link/Unbind lifecycle through the session core, supports TCP or TLS-over-TCP, and emits aggregate statistics rather than per-PDU logs.
