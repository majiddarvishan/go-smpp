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
- [x] Create repository planning/context files.

## Phase 1 — Module skeleton and protocol primitives

- [ ] Initialize Go module as `github.com/majiddarvishan/go-smpp`.
- [ ] Pin minimum supported Go version.
- [ ] Create package skeleton with dependency-direction tests/review.
- [ ] Define `CommandID`, `CommandStatus`, `SequenceNumber`, TON, NPI, data-coding and session-state types.
- [ ] Define SMPP 3.4 and 5.0 capability/profile types without duplicating the core.
- [ ] Define protocol errors separately from transport/session errors.
- [ ] Add unit tests for primitive encodings and constants.
- [ ] Add baseline benchmarks for primitive encode/decode helpers.

## Phase 2 — Binary framing and codec foundation

- [ ] Implement the fixed 16-byte SMPP header codec.
- [ ] Implement stream framing using `command_length`; never assume one TCP read equals one PDU.
- [ ] Implement integer, C-Octet String and Octet String helpers.
- [ ] Implement TLV scanning/encoding with unknown-TLV preservation.
- [ ] Support duplicate vendor/standard TLVs without forcing a `map[tag]value` representation.
- [ ] Add configurable maximum PDU size and malformed-frame protection.
- [ ] Add fragmentation/coalescing tests for arbitrary TCP read boundaries.
- [ ] Add codec fuzz tests.
- [ ] Establish allocation and throughput baselines.
- [ ] Target common-path decode/encode at 0–2 allocations/PDU where practical, verified by benchmark.

## Phase 3 — Extensible registries

- [ ] Implement central PDU command registry.
- [ ] Implement central TLV registry.
- [ ] Support vendor-specific TLV registration.
- [ ] Support vendor-specific command registration without editing core switch statements throughout the codebase.
- [ ] Define strict vs compatible handling for unknown/unsupported fields.
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
- [ ] Add state-transition and invalid-state tests.

## Phase 6 — Transport layer and TLS

- [ ] Define transport around `net.Conn` semantics.
- [ ] Implement plain TCP dial/listen using the standard library.
- [ ] Implement TLS client/server adapters using `crypto/tls`.
- [ ] Keep TLS configuration outside SMPP PDU/session packages.
- [ ] Support caller-supplied `net.Conn` compatible transports.
- [ ] Add partial-read, partial-write and connection-close tests.

## Phase 7 — Asynchronous engine with synchronous public API

- [ ] Implement independent long-lived RX and TX paths.
- [ ] Implement session-local sequence-number generation.
- [ ] Implement out-of-order response correlation.
- [ ] Implement bounded pending-request tracking.
- [ ] Expose synchronous/context-aware public submit APIs without goroutine-per-message architecture.
- [ ] Ensure inbound `deliver_sm` can be processed while outbound submit requests are outstanding.
- [ ] Add race tests and high-concurrency correlation tests.

## Phase 8 — Windowing and backpressure

- [ ] Implement configurable maximum outstanding-request window.
- [ ] Implement blocking/context-cancellable acquisition for synchronous API calls.
- [ ] Implement explicit overload/backpressure errors rather than unbounded queues.
- [ ] Record window utilization metrics/hooks.
- [ ] Benchmark different window sizes against RTT profiles.
- [ ] Avoid hard-coding the historical SMPP 3.4 recommendation of 10 outstanding requests.

## Phase 9 — Efficient timer subsystem

- [ ] Implement response deadlines without one `time.Timer` per request.
- [ ] Implement Session Init timeout.
- [ ] Implement Enquire Link scheduling.
- [ ] Implement inactivity handling.
- [ ] Evaluate deadline buckets, heap batching, and/or timer wheel by benchmark.
- [ ] Add timeout-storm benchmark and memory-bound tests.

## Phase 10 — Client auto-reconnect

- [ ] Implement configurable reconnect policy/backoff.
- [ ] Re-bind automatically after reconnect.
- [ ] Fail pending requests from the lost session deterministically.
- [ ] Never silently auto-resubmit requests whose delivery state is ambiguous.
- [ ] Expose enough error metadata for application-level resubmission decisions.
- [ ] Add reconnect-during-full-window tests.

## Phase 11 — Server/SMSC mode

- [ ] Implement TCP/TLS listener lifecycle.
- [ ] Implement bind authentication hook/interface.
- [ ] Implement per-session state and sequence spaces.
- [ ] Implement inbound `submit_sm` dispatch and synchronous response path.
- [ ] Implement outbound `deliver_sm` from server to bound RX/TRX sessions.
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
- [ ] Expose requests sent/received, responses, timeouts, outstanding window, RTT, reconnects and decode failures.
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
- [ ] Measure CPU, allocations, heap, GC, mutex contention and scheduler behavior.
- [ ] Persist benchmark methodology/results in `.codex/PERFORMANCE.md` or dedicated reports.

## Phase 17 — Performance acceptance and optimization

- [ ] Sustain 100,000 aggregate bidirectional **request** PDUs/s on the reference machine.
- [ ] Include required SMPP response processing in the end-to-end load.
- [ ] Determine and document the minimum practical connection/session count for the benchmark scenario.
- [ ] Verify bounded memory under sustained load.
- [ ] Verify no goroutine-per-message growth pattern.
- [ ] Profile before every significant optimization.
- [ ] Keep implementation free of `unsafe` unless a later measured bottleneck justifies a separately reviewed decision.

## Phase 18 — Reliability, fuzzing and chaos

- [ ] Test fragmented and coalesced TCP streams.
- [ ] Test out-of-order responses.
- [ ] Test malformed, oversized and truncated PDUs.
- [ ] Test duplicate/unexpected sequence numbers.
- [ ] Test disconnect during bind, idle state and full outstanding window.
- [ ] Test delayed responses and timeout storms.
- [ ] Test slow peers and application handlers.
- [ ] Run `go test -race` scenarios.
- [ ] Run sustained soak tests.

## Phase 19 — Release readiness

- [ ] Freeze and document public API compatibility policy.
- [ ] Add examples for ESME client and SMSC/server.
- [ ] Complete package documentation.
- [ ] Document interoperability quirks and vendor-extension APIs.
- [ ] Publish reproducible performance results for the reference machine.
- [ ] Tag the first production-ready release.

## Backlog policy

Items intentionally deferred from the first submit/deliver milestone are tracked in `.codex/BACKLOG.md`. Moving an item out of backlog requires adding it to the appropriate phase here; completion is still recorded only by changing its checkbox to `[x]`.
