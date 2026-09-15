# Implementation Plan

This file is the source of truth for implementation progress. Every completed phase or step must be changed from `[ ]` to `[x]` in the same commit that completes it.

## Status legend

- `[ ]` not started / incomplete
- `[x]` completed and verified

## Phase 0 — Project bootstrap and architecture contract

- [x] Define project scope and protocol sources.
- [x] Define shared-core approach for client/server roles.
- [x] Define SMPP 3.4-first / SMPP 5.0-aware strategy.
- [x] Define initial hot-path scope: submit/deliver.
- [x] Define synchronous public API over asynchronous protocol core.
- [x] Define auto-reconnect policy with no hidden ambiguous auto-resubmit.
- [x] Define Linux/amd64 reference platform.
- [x] Define 8-core / 10-GB reference machine.
- [x] Define 100k aggregate bidirectional request-PDU/s target.
- [x] Define minimal-dependency and no-`unsafe` initial policy.
- [x] Define transport/TLS strategy.
- [x] Define extensible PDU/TLV registry and vendor-TLV support.
- [x] Define configurable request-response timeout semantics.
- [x] Require Enquire Link, inactivity timeout and Session Init timeout behavior.
- [x] Require concurrent/thread-safe public APIs and race-free internals.
- [x] Define fail-closed handling for unrecoverable malformed/framing PDUs and mandatory error logging.
- [x] Create repository planning/context files.

## Phase 1 — Module skeleton and protocol primitives

- [ ] Initialize Go module as `github.com/majiddarvishan/go-smpp`.
- [ ] Pin minimum supported Go version.
- [ ] Create package skeleton with dependency-direction tests/review.
- [ ] Define `CommandID`, `CommandStatus`, `SequenceNumber`, TON, NPI, data-coding and session-state types.
- [ ] Define SMPP 3.4 and 5.0 capability/profile types without duplicating the core.
- [ ] Define protocol errors separately from transport/session/timeout errors.
- [ ] Define typed timeout error metadata for command, sequence and timeout kind.
- [ ] Define fatal framing/decode error categories separately from recoverable SMPP command/status errors.
- [ ] Add unit tests for primitive encodings and constants.
- [ ] Add baseline benchmarks for primitive encode/decode helpers.

## Phase 2 — Binary framing and codec foundation

- [ ] Implement the fixed 16-byte SMPP header codec.
- [ ] Implement stream framing using `command_length`; never assume one TCP read equals one PDU.
- [ ] Implement integer, C-Octet String and Octet String helpers.
- [ ] Implement TLV scanning/encoding with unknown-TLV preservation.
- [ ] Support duplicate vendor/standard TLVs without forcing a `map[tag]value` representation.
- [ ] Add configurable maximum PDU size and malformed-frame protection.
- [ ] Classify unrecoverable structural/framing violations as fatal to the current connection/session (for example invalid `command_length`, impossible field/TLV lengths, or decode state that cannot preserve frame boundaries).
- [ ] On a fatal structural/framing error, stop consuming that byte stream, emit a structured error log, close the transport, fail the session, and never attempt byte-stream resynchronization on the same connection.
- [ ] Ensure fatal-protocol logging includes useful safe metadata (reason, peer/session/state, declared length and command/sequence when available) without dumping credentials or message payload by default.
- [ ] Add fragmentation/coalescing tests for arbitrary TCP read boundaries.
- [ ] Add malformed-length tests proving a corrupt frame cannot cause subsequent bytes to be interpreted as valid PDUs on the same connection.
- [ ] Add codec fuzz tests.
- [ ] Establish allocation and throughput baselines.
- [ ] Target common-path decode/encode at 0–2 allocations/PDU where practical, verified by benchmark.

## Phase 3 — Extensible registries

- [ ] Implement central PDU command registry.
- [ ] Implement central TLV registry.
- [ ] Support vendor-specific TLV registration.
- [ ] Support vendor-specific command registration without editing core switch statements throughout the codebase.
- [ ] Define strict vs compatible handling for unknown/unsupported fields; compatibility mode must not override fatal framing-safety rules.
- [ ] Make registry construction/mutation safe for concurrent callers or freeze registries into immutable read-only snapshots before session hot-path use.
- [ ] Add registry concurrency and duplicate-registration tests.

## Phase 4 — Essential SMPP 3.4 PDUs

- [ ] Implement `bind_transmitter` / response.
- [ ] Implement `bind_receiver` / response.
- [ ] Implement `bind_transceiver` / response.
- [ ] Implement `unbind` / response.
- [ ] Implement `enquire_link` / response.
- [ ] Implement `generic_nack`.
- [ ] Implement `submit_sm` / `submit_sm_resp`.
- [ ] Implement `deliver_sm` / `deliver_sm_resp`.
- [ ] Preserve optional TLVs on supported PDUs.
- [ ] Add specification-driven encode/decode vectors.

## Phase 5 — Shared session state machine

- [ ] Implement shared session states: Closed, Open, Outbound, Bound_TX, Bound_RX, Bound_TRX, Unbound.
- [ ] Implement legal-operation validation by state and peer role.
- [ ] Implement client and server role semantics on the same session core.
- [ ] Keep codec independent from session state.
- [ ] Implement clean bind/unbind lifecycle.
- [ ] Make fatal decoder/framing errors transition the owning session to failure/closed exactly once.
- [ ] Make state transitions race-free under concurrent send/receive/close/timeout activity.
- [ ] Add state-transition and invalid-state tests.

## Phase 6 — Transport layer and TLS

- [ ] Define transport around `net.Conn` semantics.
- [ ] Implement plain TCP dial/listen using the standard library.
- [ ] Implement TLS client/server adapters using `crypto/tls`.
- [ ] Keep TLS configuration outside SMPP PDU/session packages.
- [ ] Support caller-supplied `net.Conn` compatible transports.
- [ ] Ensure protocol-fatal close interrupts blocked RX/TX operations and is idempotent/concurrent-safe.
- [ ] Add partial-read, partial-write and connection-close tests.

## Phase 7 — Asynchronous engine with synchronous public API

- [ ] Implement independent long-lived RX and TX paths.
- [ ] Implement session-local sequence-number generation.
- [ ] Implement out-of-order response correlation.
- [ ] Implement bounded pending-request tracking.
- [ ] Expose synchronous/context-aware public submit APIs without goroutine-per-message architecture.
- [ ] Ensure inbound `deliver_sm` can be processed while outbound submit requests are outstanding.
- [ ] Guarantee public `Client`, `Server`, `Session` and request APIs documented as concurrent-safe can be called from multiple goroutines at the same time.
- [ ] Ensure request completion is exactly-once when response, timeout, cancellation, fatal protocol error and session loss race each other.
- [ ] Add race tests and high-concurrency correlation tests.
- [ ] Run `go test -race` for concurrent session/client/server scenarios.

## Phase 8 — Windowing and backpressure

- [ ] Implement configurable maximum outstanding-request window.
- [ ] Implement blocking/context-cancellable acquisition for synchronous API calls.
- [ ] Implement explicit overload/backpressure errors rather than unbounded queues.
- [ ] Ensure timeout/cancellation/session-loss paths always release window capacity exactly once.
- [ ] Record window utilization metrics/hooks.
- [ ] Benchmark different window sizes against RTT profiles.
- [ ] Avoid hard-coding the historical SMPP 3.4 recommendation of 10 outstanding requests.

## Phase 9 — Efficient timer and liveness subsystem

- [ ] Implement a configurable per-request response timeout for outbound SMPP requests.
- [ ] Start the protocol response timeout when the request PDU has been fully dispatched to the transport; time spent waiting for window/TX capacity remains governed by the caller context/deadline.
- [ ] On response timeout, atomically remove/expire the pending request, release its window slot and return a typed timeout error to the synchronous caller.
- [ ] Treat a response arriving after its request expired as a late/unmatched response; it must never complete an unrelated request.
- [ ] Implement response deadlines without one independent `time.Timer` per request in the final high-throughput design.
- [ ] Implement configurable Session Init timeout for connection-to-bind/session-establishment lifecycle.
- [ ] Implement configurable Enquire Link interval/scheduling after SMPP inactivity.
- [ ] Correlate `enquire_link` / `enquire_link_resp` through the same safe request/response machinery and apply a response timeout.
- [ ] Implement configurable inactivity timeout based on session activity and define deterministic close/unbind behavior on expiry.
- [ ] Ensure liveness timers are reset/update-safe under simultaneous RX and TX traffic.
- [ ] Evaluate deadline buckets, heap batching, and/or timer wheel by benchmark.
- [ ] Add boundary-race tests where response and timeout occur at nearly the same instant.
- [ ] Add timeout-storm benchmark and memory-bound tests.

## Phase 10 — Client auto-reconnect

- [ ] Implement configurable reconnect policy/backoff.
- [ ] Re-bind automatically after reconnect.
- [ ] Fail pending requests from the lost session deterministically.
- [ ] Never silently auto-resubmit requests whose delivery state is ambiguous.
- [ ] Treat fatal malformed-PDU closure like transport/session loss for reconnect policy, while preserving the protocol-failure reason in diagnostics.
- [ ] Expose enough error metadata for application-level resubmission decisions.
- [ ] Ensure reconnect, close and timeout transitions are race-free.
- [ ] Add reconnect-during-full-window tests.

## Phase 11 — Server/SMSC mode

- [ ] Implement TCP/TLS listener lifecycle.
- [ ] Implement bind authentication hook/interface.
- [ ] Implement per-session state and sequence spaces.
- [ ] Enforce Session Init timeout for accepted connections that do not establish a valid SMPP session in time.
- [ ] Implement inbound `submit_sm` dispatch and synchronous response path.
- [ ] Implement outbound `deliver_sm` from server to bound RX/TRX sessions.
- [ ] Apply outbound request response-timeout behavior to server-originated requests such as `deliver_sm`.
- [ ] Close only the offending connection/session on a fatal malformed PDU; keep the listener and unrelated sessions healthy.
- [ ] Add configurable connection/session limits.
- [ ] Add slow-client and malicious-frame protection tests.

## Phase 12 — Message and encoding packages

- [ ] Create encoding packages separate from protocol/session core.
- [ ] Implement GSM 7-bit support.
- [ ] Implement UCS-2 support.
- [ ] Support binary payloads.
- [ ] Implement UDH-based multipart segmentation/reassembly.
- [ ] Implement SAR-TLV multipart support.
- [ ] Keep message-content decoding optional on the protocol hot path.
- [ ] Add conformance and boundary tests.

## Phase 13 — SMPP 3.4 completeness

- [ ] Implement `data_sm` / response.
- [ ] Implement `submit_multi` / response.
- [ ] Implement `query_sm` / response.
- [ ] Implement `cancel_sm` / response.
- [ ] Implement `replace_sm` / response.
- [ ] Implement `alert_notification`.
- [ ] Implement `outbind` semantics.
- [ ] Complete all SMPP 3.4 standard TLVs.
- [ ] Complete SMPP 3.4 command-status coverage.
- [ ] Run a full 3.4 conformance matrix.

## Phase 14 — SMPP 5.0 extensions

- [ ] Add SMPP 5.0 capability negotiation on the shared core.
- [ ] Implement `congestion_state` TLV support.
- [ ] Implement adaptive flow-controller extension points.
- [ ] Add SMPP 5.0 error/status additions.
- [ ] Add SMPP 5.0 number-portability and endpoint-identification TLVs.
- [ ] Implement Cell Broadcast commands and related TLVs.
- [ ] Add SMPP 5.0 compatibility/conformance tests.

## Phase 15 — Observability without hot-path logging

- [ ] Define zero/low-overhead counters and event hooks.
- [ ] Expose requests sent/received, responses, response timeouts, session-init timeouts, inactivity expirations, enquire-link activity, outstanding window, RTT, reconnects and decode failures.
- [ ] Emit mandatory structured error logs for fatal malformed/framing PDUs before/while terminating the offending session; these rare error logs are distinct from disabled-by-default per-PDU tracing.
- [ ] Expose congestion-state data when available.
- [ ] Provide optional packet tracing outside the default hot path.
- [ ] Keep per-PDU logging disabled by default.

## Phase 16 — Performance simulator and benchmark laboratory

- [ ] Build a minimal high-throughput SMPP peer simulator.
- [ ] Add codec-only benchmarks.
- [ ] Add in-memory/loopback session benchmarks.
- [ ] Add localhost TCP benchmarks.
- [ ] Add TLS benchmarks separately from plain TCP.
- [ ] Add single-session bidirectional benchmark first.
- [ ] Add 2-, 4-, and higher-session benchmarks only as needed to find the minimum session count meeting target.
- [ ] Add request-timeout and liveness-timer overhead benchmarks under high outstanding counts.
- [ ] Measure CPU, allocations, heap, GC, mutex contention and scheduler behavior.
- [ ] Persist benchmark methodology/results in `.codex/PERFORMANCE.md` or dedicated reports.

## Phase 17 — Performance acceptance and optimization

- [ ] Sustain 100,000 aggregate bidirectional **request** PDUs/s on the reference machine.
- [ ] Include required SMPP response processing in the end-to-end load.
- [ ] Determine and document the minimum practical connection/session count for the benchmark scenario.
- [ ] Verify bounded memory under sustained load.
- [ ] Verify no goroutine-per-message growth pattern.
- [ ] Verify configured timeout tracking remains bounded and does not become a throughput bottleneck.
- [ ] Profile before every significant optimization.
- [ ] Keep implementation free of `unsafe` unless a later measured bottleneck justifies a separately reviewed decision.

## Phase 18 — Reliability, fuzzing and chaos

- [ ] Test fragmented and coalesced TCP streams.
- [ ] Test out-of-order responses.
- [ ] Test malformed, oversized and truncated PDUs.
- [ ] Test invalid `command_length`, impossible mandatory-field lengths, invalid TLV lengths and corrupted frames; assert the offending connection closes and no byte-stream resynchronization is attempted.
- [ ] Verify each fatal structural/framing rejection emits the required diagnostic log without exposing sensitive message/authentication content by default.
- [ ] Test duplicate/unexpected sequence numbers.
- [ ] Test disconnect during bind, idle state and full outstanding window.
- [ ] Test delayed responses, late responses and timeout storms.
- [ ] Test Session Init timeout, Enquire Link timeout/liveness and inactivity timeout scenarios.
- [ ] Test simultaneous response-vs-timeout, timeout-vs-close and reconnect-vs-close races.
- [ ] Test slow peers and application handlers.
- [ ] Run `go test -race` scenarios.
- [ ] Run sustained soak tests with concurrent API callers.

## Phase 19 — Release readiness

- [ ] Freeze and document public API compatibility policy.
- [ ] Document concurrency guarantees for every public mutable type.
- [ ] Document timeout/liveness configuration and exact semantics.
- [ ] Document fatal malformed-PDU connection-close and diagnostic logging policy.
- [ ] Add examples for ESME client and SMSC/server.
- [ ] Complete package documentation.
- [ ] Document interoperability quirks and vendor-extension APIs.
- [ ] Publish reproducible performance results for the reference machine.
- [ ] Tag the first production-ready release.

## Backlog policy

Items intentionally deferred from the first submit/deliver milestone are tracked in `.codex/BACKLOG.md`. Moving an item out of backlog requires adding it to the appropriate phase here; completion is still recorded only by changing its checkbox to `[x]`.
