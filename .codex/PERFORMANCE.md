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

## Phase 16 verified CI development baseline

GitHub Actions run `35279555454` executed the Phase 16 laboratory on Go 1.26.8/Linux amd64 using a hosted AMD EPYC runner. The run passed the normal and race test suites, then recorded these diagnostic baselines:

| Path | Sessions | request-PDU/s | allocs/op | bytes/op |
| --- | ---: | ---: | ---: | ---: |
| in-memory `net.Pipe` | 1 | ~163,057 | 16 | ~1,165 |
| in-memory `net.Pipe` | 2 | ~267,359 | 16 | ~1,159 |
| in-memory `net.Pipe` | 4 | ~309,018 | 16 | ~1,151 |
| localhost TCP | 1 | ~42,600 | 16 | ~1,176 |
| localhost TCP | 2 | ~56,261 | 16 | ~1,176 |
| localhost TCP | 4 | ~60,047 | 16 | ~1,177 |
| localhost TCP | 8 | ~60,214 | 16 | ~1,177 |
| localhost TLS | 1 | ~39,674 | 17 | ~1,208 |
| localhost TLS | 2 | ~52,030 | 18 | ~1,216 |
| localhost TLS | 4 | ~55,420 | 18 | ~1,219 |
| localhost TLS | 8 | ~55,499 | 18 | ~1,219 |

Codec baselines remained inexpensive relative to the full session path: complete-frame framing was allocation-free, TLV scanning was allocation-free, and typed submit/deliver decode was one allocation per operation in the recorded run.

The CPU/scheduler profiles show substantial time in Go runtime select/channel scheduling, while the heap profile attributes visible allocation volume to `Session.request`, short-message encoding, response ownership copies, deadline scheduling, and decoded submit/deliver bodies. This is the measured starting point for Phase 17. The hosted runner is not the reference 8-core/10-GB acceptance environment, so these figures must not be presented as the production acceptance result.

## Phase 17 optimization order

The first optimization pass should preserve protocol semantics and target measured costs in this order:

1. reduce per-PDU TX syscall/scheduler overhead using opportunistic bounded batching/coalescing without delaying a packet merely to form a batch;
2. reduce avoidable request-path allocations while preserving exactly-once response/timeout/cancel/session-loss completion;
3. re-profile pending/deadline/window contention before replacing the current simple bounded structures;
4. re-run 1, 2, 4, and only then higher session counts;
5. perform the final sustained acceptance run on the documented 8-core/10-GB Linux/amd64 host.

Every optimization remains subject to `go test -race`, bounded-memory requirements, fail-closed framing behavior, and the no-hidden-resubmit reconnect rule.

### Phase 17 experiment: opportunistic TX batching

The first Phase 17 experiment added bounded, no-wait TX coalescing and measured it immediately on the same hosted CI class. With batching enabled by default at 32 queued PDUs, localhost TCP throughput regressed from the Phase 16 baseline (for example one session from ~42.6k to ~39.2k request-PDU/s, four sessions from ~60.0k to ~53.4k), and TLS also regressed. In-memory throughput improved slightly in some cases, but the network result did not justify making batching the default.

The batching implementation is therefore retained only as an explicit tuning option and the default is one PDU per transport write. This preserves a reproducible opt-in experiment without imposing a measured regression on normal sessions. The next optimization target is allocation pressure in the hot submit/deliver encode/request path.

## Phase 17 measured TX batching experiment

A same-run localhost TCP sweep on the hosted AMD EPYC 9V74 runner measured the scatter/gather TX path with one SMPP session, 64 concurrent callers, and required SMPP responses enabled:

| TX batch | request-PDU/s | allocs/op | bytes/op |
| ---: | ---: | ---: | ---: |
| 1 | ~67,795 | 11 | ~977 |
| 2 | ~108,144 | 12 | ~999 |
| 4 | ~146,731 | 11 | ~988 |
| 8 | ~167,931 | 11 | ~984 |
| 16 | ~168,177 | 11 | ~983 |
| 32 | ~182,512 | 11 | ~983 |

This experiment validates opportunistic scatter/gather batching as a material TCP optimization on that development runner. Based on this measured sweep, the session default is now 32 queued PDUs, bounded by `TXBatchBytes`; setting `TXBatchItems=1` disables coalescing for peers/workloads where that is preferable. The TX loop does not delay an isolated packet just to fill a batch. Performance-lab defaults now follow the production session default so ordinary smoke/profile runs exercise the measured fast path.

The experiment is still not the Phase 17 acceptance result: the required acceptance host is Linux/amd64 with 8 CPU cores and 10 GB RAM, and sustained memory/goroutine/timeout bounds must also be verified there.

## Phase 17 reference acceptance runner

`scripts/acceptance.sh` is the authoritative final-acceptance entry point. It refuses to run the acceptance claim unless the host provides at least 8 logical CPUs, at least 10 GiB RAM, and Go 1.26.x; it runs the production workload with `GOMAXPROCS=8` by default.

The sustained test starts at one TCP session, uses fixed concurrent load-generator workers, samples heap/goroutine/outstanding-work bounds, requires every request to receive its SMPP response, and rejects unexpected traffic errors. The script then tries 2 and 4 sessions only if the preceding count fails the configured throughput target. After the first passing count it captures CPU, heap, mutex, and block profiles and records the exact environment and commit.

Default final settings are a 60-second sustained window, 100,000 request-PDU/s minimum, 128 fixed callers, and the measured TX batch of 32. These defaults may be overridden for diagnostics, but a production acceptance report must state any override explicitly.
