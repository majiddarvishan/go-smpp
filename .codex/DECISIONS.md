# Architectural Decisions

This file records decisions that should not be silently changed during implementation. If a decision changes, append/update the relevant entry and explain why.

## D-001 — Shared protocol core

**Decision:** Client/ESME and server/SMSC share protocol types, codec, session state machinery, sequencing, timers and flow-control infrastructure where semantics are common.

**Reason:** Duplicated client/server stacks would diverge and make protocol correctness/performance work harder.

## D-002 — SMPP 3.4 first, SMPP 5.0-aware architecture

**Decision:** SMPP 3.4 is the first complete compatibility target. SMPP 5.0 features are implemented as capabilities/extensions on the same core.

**Reason:** 5.0 is an evolution of 3.4 and should not require a second codec/session stack.

## D-003 — Initial functional scope

**Decision:** First production path is bind/session management plus `submit_sm` and `deliver_sm` request/response flows.

**Reason:** This covers the critical bidirectional messaging path while allowing performance architecture to be validated early.

## D-004 — Synchronous public API, asynchronous engine

**Decision:** Public request APIs are synchronous/context-aware. Internally, the engine is asynchronous and supports multiple outstanding requests with out-of-order responses.

**Reason:** A synchronous application-facing API is simple to use, while a synchronous wire engine would waste RTT and fail the throughput target.

## D-005 — No goroutine per message

**Decision:** Long-lived goroutines may be used for session components, but the design must not create a goroutine for every PDU/request.

**Reason:** At high request rates and non-trivial RTT, outstanding request counts can be large; goroutine-per-message creates avoidable scheduler/memory pressure.

## D-006 — Bounded concurrency and memory

**Decision:** Pending tables, queues, windows and buffers must have explicit limits/backpressure. No unbounded producer queue is allowed in the core.

## D-007 — Reconnect without hidden resubmit

**Decision:** Client reconnect/rebind is automatic and configurable. Pending requests from the lost session fail. The library never automatically resubmits an operation whose remote outcome is ambiguous.

**Reason:** Hidden resubmission can produce duplicate SMS delivery.

## D-008 — Minimal dependencies

**Decision:** Prefer Go standard library. External dependencies require a concrete benefit and must be documented here before introduction.

## D-009 — No unsafe initially

**Decision:** Initial implementation uses no `unsafe`.

**Reason:** Correctness and maintainability come first. A future use of `unsafe` requires profiling evidence, benchmark benefit, targeted tests and an explicit decision change.

## D-010 — Transport/TLS boundary

**Decision:** The network transport is TCP. X.25 is out of scope. The protocol/session core works on a `net.Conn`-like stream boundary. Plain TCP uses `net`; optional TLS is layered over TCP using `crypto/tls`. Caller-supplied compatible stream connections may be allowed where they preserve the same semantics.

**Reason:** TCP is the project transport requirement. Keeping TLS outside PDU/session semantics preserves a clean boundary and avoids an unnecessary runtime dependency.

## D-011 — Extensible registry

**Decision:** PDU command decoding and TLV interpretation use centralized extensible registries. Vendor-specific TLVs are first-class extension points. The design must not require editing scattered switch statements to add vendor extensions.

## D-012 — TLV representation

**Decision:** Do not make `map[uint16][]byte` the only internal TLV representation.

**Reason:** It loses duplicate tags/order and tends to allocate aggressively. The codec should permit ordered/borrowed representations while still offering convenient lookup APIs.

## D-013 — Stream-oriented decoder

**Decision:** Framing is based on `command_length`. Code must handle partial PDUs and several PDUs in one TCP read.

## D-014 — Performance target definition

**Decision:** `100k` means 100,000 aggregate bidirectional **request** PDUs/s: requests sent plus requests received. Required responses are extra PDUs and are included in end-to-end processing load.

Reference hardware: Linux/amd64, 8 cores, 10 GB RAM.

## D-015 — Minimum connection count

**Decision:** Optimize and benchmark one SMPP session first. Increase TCP session count only as required by measured RTT/window/peer/CPU constraints. Acceptance reporting must state the smallest session count that reaches the target.

## D-016 — Timer strategy

**Decision:** Do not use one independent `time.Timer` per outstanding request in the final high-throughput design. Compare efficient shared deadline structures using benchmarks.

## D-017 — Logging/observability

**Decision:** Per-PDU logging is not part of the default hot path. Prefer metrics/counters/event hooks; packet tracing is explicitly enabled and isolated from normal performance.

## D-018 — Optimization policy

**Decision:** Optimize after measurement. Every non-obvious performance optimization must be supported by a benchmark/profile showing the bottleneck and benefit.

## D-019 — Request response timeout semantics

**Decision:** Every outbound SMPP request that expects a response supports a configurable protocol response timeout. The response timeout starts after the request PDU has been fully dispatched to the active transport. Time spent waiting for window capacity or local TX admission is controlled separately by the caller context/deadline.

On expiry, the pending request is completed exactly once with a typed response-timeout error, removed from correlation state, and its window capacity is released exactly once. A later response for the expired request is treated as late/unmatched and must not complete a different request.

## D-020 — SMPP liveness timers

**Decision:** The session engine must support configurable Session Init timeout, Enquire Link scheduling/response timeout and inactivity timeout for both relevant client and server session lifecycles.

## D-021 — Concurrency/thread-safety contract

**Decision:** Public mutable types that represent active runtime objects (`Client`, `Server`, `Session`, and documented request/send APIs) must be safe for concurrent use by multiple goroutines. Internal state transitions, pending correlation, sequence allocation, window accounting, timeout completion, close and reconnect must be data-race free.

Registries/configuration should avoid hot-path mutable global state. Prefer explicit registry instances that are either concurrency-safe during construction or frozen into immutable snapshots before sessions use them.

## D-022 — Exactly-once local request completion

**Decision:** For each locally originated request, only one terminal event may win: matching response, caller cancellation/deadline, protocol response timeout, or session/transport loss. All losing paths must observe the completed state and must not double-release window capacity, double-notify the caller or mutate reused request state.

## D-023 — Fail closed on unrecoverable structural/framing corruption

**Decision:** If an inbound PDU is structurally malformed in a way that makes byte-stream alignment or trustworthy decoding unsafe, the current transport/session is considered corrupted and must be closed. The decoder must not attempt heuristic byte-stream resynchronization. Only the offending connection/session is terminated; a server listener and unrelated sessions remain alive.

Recoverable protocol/application errors where the frame boundary is intact remain distinct and may use the appropriate SMPP response/status behavior rather than forcing connection closure.

## D-024 — Mandatory diagnostic logging for fatal protocol corruption

**Decision:** Every connection termination caused by an unrecoverable malformed/framing PDU must emit a structured error log through the library's logging/diagnostic facility. Credentials, message payloads and other sensitive body content are not dumped by default.

## D-025 — Minimum Go version

**Decision:** The minimum supported Go version is **Go 1.26**. The module is `github.com/majiddarvishan/go-smpp` and the `go.mod` directive is pinned accordingly.

**Reason:** The project is new, performance-sensitive, and targets Linux/amd64. Supporting an actively supported modern Go baseline avoids carrying compatibility cost for older toolchains while leaving Go 1.27 usable by consumers.

## D-026 — Sequence-number receive interoperability

**Decision:** Locally generated sequence numbers remain in the SMPP-defined request range `0x00000001..0x7fffffff`. Inbound PDUs accept any non-zero uint32 sequence number through `0xffffffff`. A response to an inbound request must preserve the received value exactly, including values above `0x7fffffff`.

**Reason:** SMPP 3.4 documents `0x7fffffff` as the upper range, but deployed peers are known to transmit values in the upper uint32 half. Rejecting them would cause avoidable interoperability failures while accepting them does not affect framing or response correlation.

## D-027 — Codec maximum PDU bound

**Decision:** The codec/framer has a configurable maximum PDU size. The initial default is **1 MiB**, allocated lazily rather than reserved per connection. A declared `command_length` above the configured bound is a fatal framing error for that TCP connection.

**Reason:** SMPP framing uses a 32-bit length, so an explicit operational bound is required to prevent unbounded memory commitment from malformed or hostile peers. One MiB leaves substantial room above ordinary SMPP message payloads and vendor TLVs while retaining a defensive ceiling.

## Open decisions

The following remain to be decided in later phases and recorded here:

- Exact public package naming/API conventions after the first API sketch.
- Default window size and backpressure behavior.
- Default per-request response timeout.
- Default Session Init timeout.
- Default Enquire Link interval and response timeout.
- Default inactivity timeout and exact graceful-close policy.
- Default reconnect/backoff policy.
- Exact logging interface/default logger behavior for mandatory fatal-protocol diagnostics.
- Borrowed-vs-owned PDU exposure rules at the public boundary.
