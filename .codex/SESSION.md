# Session Handoff

## Current state

Branch: `main`

Phase 0 through Phase 16 are complete and verified. Phase 16 adds the minimal SMPP peer simulator, codec/session/TCP/TLS benchmark laboratory, timeout/window/liveness overhead benchmarks, and CPU/heap/mutex/block/scheduler/GC profiling. Phase 17 performance acceptance and optimization is now in progress.

The core Phase 5–7 implementation began in commit `2dd79458bd4859ad4a834e79f53bbcfb4572622d` and was hardened by follow-up test/correctness commits through `01ec03bd3ed9cae50f0e905835c5789032fd5751` before the completion documentation updates.

Network transport is TCP. X.25 is out of scope. TLS is implemented as TLS-over-TCP using the Go standard library.

## Phase 5 completed — shared session state machine

The `session` package now contains a shared state machine for ESME and SMSC roles with:

- `Closed`, `Open`, `Outbound`, `Bound_TX`, `Bound_RX`, `Bound_TRX`, and `Unbound` states
- role-aware legal-operation validation
- bind transmitter/receiver/transceiver lifecycle
- unbind lifecycle
- atomic state reads plus serialized multi-step lifecycle transitions
- invalid-state response behavior
- race-safe/idempotent close behavior
- fatal decoder/framing failure -> structured diagnostic -> connection close -> state Closed exactly once

The codec remains independent of session state.

## Phase 6 completed — TCP and TLS-over-TCP transport

The `transport` package now provides:

- plain TCP dial/listen using `net.Dialer` and `net.ListenConfig`
- TLS client/server wrappers using `crypto/tls`
- TLS dial and listen helpers layered over TCP
- caller-supplied `net.Conn` compatibility
- `WriteFull` handling short writes so full-dispatch semantics are explicit
- tests for localhost TCP, fragmented stream reads, short writes, TLS handshake/data exchange, connection close, and fatal receive interrupting a blocked transmit

A fatal structural/framing PDU closes the offending connection. Closing the underlying `net.Conn` interrupts blocked RX/TX operations. No stream resynchronization is attempted.

## Phase 7 completed — asynchronous engine with synchronous public API

Each active session uses independent long-lived RX and TX paths. There is no goroutine-per-request/message architecture.

Implemented behavior includes:

- synchronous/context-aware public request calls over the asynchronous wire engine
- typed `BindTransmitter`, `BindReceiver`, `BindTransceiver`, `SubmitSM`, `DeliverSM`, `EnquireLink`, and `Unbind` helpers
- session-local outbound sequence generation in `1..0x7fffffff` with wrap back to 1
- inbound interoperability sequence acceptance through `0xffffffff`
- exact inbound sequence echo in responses
- bounded pending-request correlation
- out-of-order response completion
- concurrent bidirectional TRX operation
- inbound `deliver_sm` processing while an outbound `submit_sm` remains outstanding
- concurrent-safe `Session`, active `Client`, and `Server` lifecycle/request APIs
- exactly-once terminal completion primitive across response, caller cancellation, future timeout completion, fatal protocol failure, and session loss
- late/unmatched/duplicate responses cannot complete a different request
- owned copies for response values returned past the receive-frame lifetime

Phase 8 adds a configurable outbound request window (default 1024), context-cancellable blocking acquisition, non-blocking `TryRequest` admission with `ErrWindowFull`, exact-once slot release through pending-entry ownership, and lock-free window utilization/high-water/wait snapshots with an optional observer hook. `MaxPending` remains a defensive correlation-table ceiling and is automatically kept at least as large as the window.

Phase 9 adds configurable response deadlines that begin only after full PDU dispatch, a cancellable shared deadline heap with one reusable timer/goroutine per session, typed response/enquire timeouts, Session Init timeout, automatic Enquire Link after idle periods, and inactivity shutdown. Deadline cancellation is coupled to the exact-once pending terminal path so window capacity and timer state are released together.

Phase 10 adds opt-in automatic reconnect/rebind for dialed clients with bounded exponential backoff, persistent bind-profile copies, fresh sequence/pending state per replacement session, typed unavailable/loss metadata, retained fatal-protocol loss cause, and explicit Close cancellation. No ambiguous submit/deliver request is replayed.

Phase 11 completes SMSC/server mode with TCP/TLS listener construction, bind authentication and submit dispatch hooks, per-session isolation and sequence spaces, Session Init enforcement, configurable active-session limits, server-originated `deliver_sm`, and malformed/slow-client isolation. Outbound server requests use the same bounded window and response-timeout machinery as client requests.

Phase 12 adds GSM 03.38 default/extension alphabet conversion, septet packing/unpacking, strict UCS-2, UTF-16BE surrogate-pair emoji support, explicit text encoding selection, binary payload helpers, UDH multipart segmentation/reassembly metadata, and SAR-TLV helpers. Segmentation counts septets/code units rather than Go runes and never splits a UTF-16 surrogate pair.

Phase 13 completes the SMPP 3.4 command surface with `data_sm`, `submit_multi`, `query_sm`, `cancel_sm`, `replace_sm`, `alert_notification`, and `outbind`; registers all 44 SMPP 3.4 standard TLV tag identifiers; and verifies the complete named 3.4 command-status set. `outbind` and `alert_notification` are modeled as one-way primitives, never consume a pending/window slot, and never receive a synthetic response. Outbind moves both sides through `Outbound` and permits the ESME to originate `bind_receiver`. `replace_sm` is intentionally Bound_TX-only per the 3.4 operation matrix. Typed client/session/server convenience methods were added for the newly completed operations.

Phase 14 extends that same core rather than introducing a parallel SMPP 5.0 stack. `Config.Profile` selects the standard registry/profile, bind negotiation records `PeerCapabilities`, and an ESME can use v5-only operations only after 5.0 was mutually negotiated. SMSC sessions automatically add `sc_interface_version` to successful bind responses for 3.4/5.0 peers, but do not send it to pre-3.4 peers. A local 3.4 profile cannot advertise 5.0, and a local 5.0 profile that intentionally binds as 3.4 is capped at 3.4 capabilities.

The v5 registry adds six Cell Broadcast command IDs and 20 v5 TLV tags on top of the complete 3.4 registry, including `congestion_state`, broadcast/billing fields, number portability, and endpoint network/node identification. `broadcast_sm`, `query_broadcast_sm`, and `cancel_broadcast_sm` have typed session/client APIs. The codec accepts `congestion_state` on normal, header-only, and non-zero-status responses; error responses may carry TLVs without reintroducing their omitted standard body. `FlowController` receives validated 0..100 congestion feedback on the RX path and is intentionally separate from the hard request-window bound.

## Phase 15 completed — observability without hot-path logging

Each `Session` now exposes a lock-free `Metrics()` snapshot for sent/received requests and responses, protocol/liveness timeouts, Enquire Link activity, decode/fatal failures, response RTT samples, congestion feedback, and the bounded request-window snapshot. The client exposes reconnect and failed-reconnect counters without changing reconnect semantics.

`Observer` provides optional typed session events and `PacketTracer` provides optional direction/header/length traces. Both are nil by default and use no background worker/queue. Raw PDU tracing is a separate `TraceRawPDU` opt-in because complete wire frames may contain bind credentials or SMS content. Normal traffic still produces no per-PDU `slog` records. Fatal structural/framing failures continue to emit the mandatory safe structured diagnostic before/while closing the offending TCP session, regardless of whether optional observability hooks are configured.

RTT measurement is tied to the full-dispatch boundary. A very fast peer can return a response before the TX goroutine records its post-write timestamp; that race is preserved as a valid zero lower-bound RTT sample instead of dropping the sample. Congestion metrics are updated from negotiated SMPP 5.0 response feedback even if no adaptive `FlowController` is configured.

## Phase 16 completed — performance laboratory

Phase 16 added a reproducible benchmark stack and verified it on Go 1.26.8/Linux amd64. The end-to-end workload exchanges `submit_sm/submit_sm_resp` and `deliver_sm/deliver_sm_resp` concurrently, counts only request PDUs in `request_pdu/s`, and still processes every required response.

GitHub Actions run `35279555454` completed successfully with `go test ./...`, `go test -race ./...`, `go vet ./...`, codec/session microbenchmarks, in-memory bidirectional benchmarks, localhost TCP/TLS benchmarks, and profiling.

Development-run baselines on the hosted AMD EPYC runner were approximately 163k request-PDU/s for one in-memory session, 42.6k for one localhost TCP session, 56.3k for two TCP sessions, 60.0k for four, and 60.2k for eight. TLS measured approximately 39.7k, 52.0k, 55.4k, and 55.5k request-PDU/s for 1/2/4/8 sessions respectively. These numbers are diagnostics only and are not the Phase 17 8-core/10-GB acceptance result.

The first profile shows the main optimization candidates are session scheduling/channel coordination and per-request allocation pressure rather than codec framing: the in-memory path already exceeds the target on one session, while localhost TCP plateaus well below it on this runner. The heap profile also highlights `Session.request`, short-message encoding/decoding, owned response copies, and deadline records as measurable allocation sources.

## Validation performed

GitHub Actions run `34986312997` validated the Phase 5–7 code on Go 1.26.x / Linux amd64 and completed successfully:

```text
go test ./...
go test -race ./...
go test -run '^$' -bench=. -benchmem ./protocol ./codec
```

Coverage added for these phases includes:

- state transitions and invalid-state commands
- 128 concurrent requests with responses returned in reverse order
- concurrent bidirectional submit/deliver traffic
- `deliver_sm` while `submit_sm` is still pending
- receive-side sequence values through `0xffffffff`
- fragmented TCP reads and short writes
- TLS-over-TCP handshake/data exchange
- fatal malformed PDU logging + close
- fatal receive closing a connection while TX is blocked
- response/timeout/cancellation/session-loss exactly-once completion races
- concurrent client/server/session close behavior
- outbound sequence wrap

The protocol/codec benchmarks remain microbenchmarks; no end-to-end 100k request-PDU/s performance claim has been made yet.

The Phase 15 apply gate runs on Go 1.26.x/Linux and requires all of the following before the phase commit can be pushed to `main`:

```text
git diff --check --cached
go test ./...
go test -race ./...
go vet ./...
```

Phase 15 adds tests for observability counters/events, opt-in packet tracing and raw-trace gating, timeout/liveness counters, congestion metrics, reconnect metrics, fatal decode counters/logging, and ordinary-traffic no-log behavior.

## Important requirements to preserve

- 100k aggregate bidirectional request-PDU/s target on 8 cores / 10 GB RAM with minimum practical TCP session count.
- locally generated sequence numbers: `1..0x7fffffff`.
- inbound sequence interoperability: accept `1..0xffffffff` and preserve exact value in responses.
- synchronous/context-aware public API over an asynchronous pipelined engine.
- thread-safe active runtime APIs; no goroutine-per-message/request/timeout model.
- all queues/pending structures must remain bounded.
- configurable request-response timeout; Session Init, Enquire Link and inactivity handling remain required.
- auto-reconnect/rebind is implemented without hidden auto-resubmit of ambiguous requests.
- fatal malformed/framing PDU => mandatory structured error log + close offending connection; no stream resynchronization.
- GSM 03.38/GSM 7-bit, strict UCS-2, and UTF-16BE surrogate-pair/emoji support are implemented and must remain separate from the protocol hot path.
- SMPP 3.4 complete first, architecture SMPP 5.0-aware.
- no `unsafe` initially; minimal runtime dependencies.

## Phase 17 progress — profile-guided TCP optimization

The first Phase 17 profile showed the single-session localhost TCP path dominated by transport write syscalls. Opportunistic TX batching was therefore implemented and then upgraded to `net.Buffers` scatter/gather for plain `*net.TCPConn` sessions. A same-run batch sweep on a hosted AMD EPYC 9V74 runner improved one-session localhost TCP from roughly 67.8k request-PDU/s at batch 1 to roughly 182.5k request-PDU/s at batch 32 while processing all required SMPP responses. This is a development-run result, not the 8-core/10-GB acceptance result.

The response-deadline record is now embedded in `pendingRequest`, the dispatch/receive timestamps are reused, caller parallelism is configurable in the benchmark lab, and the deadline microbenchmark has reached zero allocations in recent CI runs. An attempted outbound frame `sync.Pool` optimization was reverted after CI showed it increased bytes/op and allocations/op; Phase 17 remains profile-driven rather than keeping regressions.



The Phase 17 short sustained CI checkpoint now verifies all mandatory SMPP responses, fixed-worker goroutine bounds, bounded pending/window state, and zero retained heap growth after GC in the sampled run. `PLAN.md` marks those non-reference-specific checks complete. Final 100k/minimum-session/sustained-memory acceptance remains open until `scripts/acceptance.sh` runs on the actual 8-core/10-GB reference environment.

## Exact next task

Continue **Phase 17 — Performance acceptance and optimization** from `PLAN.md`. Re-profile the one-session localhost TCP path with the measured batch-32 scatter/gather fast path, then target the remaining request/response allocation and synchronization costs. Keep the 100k acceptance checkbox open until the documented Linux/amd64 8-core / 10-GB reference environment sustains the target with bounded memory/timeouts and the minimum practical session count.
