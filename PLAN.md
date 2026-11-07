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
- [x] Define TCP as the network transport; X.25 is out of scope. TLS, when enabled, is layered over TCP.
- [x] Define transport/TLS strategy.
- [x] Define extensible PDU/TLV registry and vendor-TLV support.
- [x] Define configurable request-response timeout semantics.
- [x] Require Enquire Link, inactivity timeout and Session Init timeout behavior.
- [x] Require concurrent/thread-safe public APIs and race-free internals.
- [x] Define fail-closed handling for unrecoverable malformed/framing PDUs and mandatory error logging.
- [x] Create repository planning/context files.

## Phase 1 — Module skeleton and protocol primitives

- [x] Initialize Go module as `github.com/majiddarvishan/go-smpp`.
- [x] Pin minimum supported Go version to Go 1.26. *(Superseded 2026-10-06: Go 1.25, see `docs/DECISIONS.md` D2.)*
- [x] Create package skeleton with dependency-direction tests/review.
- [x] Define `CommandID`, `CommandStatus`, `SequenceNumber`, TON, NPI, data-coding and session-state types.
- [x] Define SMPP 3.4 and 5.0 capability/profile types without duplicating the core.
- [x] Define protocol errors separately from transport/session/timeout errors.
- [x] Define typed timeout error metadata for command, sequence and timeout kind.
- [x] Define fatal framing/decode error categories separately from recoverable SMPP command/status errors.
- [x] Add unit tests for primitive encodings and constants.
- [x] Add baseline benchmarks for primitive encode/decode helpers.

## Phase 2 — Binary framing and codec foundation

- [x] Implement the fixed 16-byte SMPP header codec.
- [x] Implement stream framing using `command_length`; never assume one TCP read equals one PDU.
- [x] Implement integer, C-Octet String and Octet String helpers.
- [x] Implement TLV scanning/encoding with unknown-TLV preservation.
- [x] Support duplicate vendor/standard TLVs without forcing a `map[tag]value` representation.
- [x] Add configurable maximum PDU size and malformed-frame protection; initial default is 1 MiB.
- [x] Classify unrecoverable structural/framing violations as fatal to the current connection/session (for example invalid `command_length`, impossible field/TLV lengths, or decode state that cannot preserve frame boundaries).
- [x] Poison the framer on a fatal structural/framing/body-decode error, stop consuming that byte stream, and never attempt byte-stream resynchronization on the same connection.
- [x] Expose safe fatal-error metadata required by the later mandatory structured logger (`reason`, declared length, command and sequence when available) without including credentials/message payload.
- [x] Add fragmentation/coalescing tests for arbitrary TCP read boundaries.
- [x] Add malformed-length tests proving a corrupt frame cannot cause subsequent bytes to be interpreted as valid PDUs on the same connection.
- [x] Add codec fuzz tests/seeds.
- [x] Establish allocation and throughput baselines.
- [x] Verify zero-allocation common paths for fixed-header decode, complete-frame stream framing and TLV scanning in the current baseline.

> Integration note: fatal codec errors are now wired through the session/transport layer to mandatory structured error logging and connection close. Broader observability counters/hooks remain tracked in Phase 15.

## Phase 3 — Extensible registries

- [x] Implement central PDU command registry.
- [x] Implement central TLV registry.
- [x] Support vendor-specific TLV registration.
- [x] Support vendor-specific command registration without editing core switch statements throughout the codebase.
- [x] Define strict vs compatible handling for unknown/unsupported fields; compatibility mode must not override fatal framing-safety rules.
- [x] Make registry construction/mutation safe for concurrent callers and freeze registries into immutable read-only snapshots before session hot-path use.
- [x] Add registry concurrency and duplicate-registration tests.
- [x] Add registry lookup microbenchmarks and verify the frozen lookup path is allocation-free.

## Phase 4 — Essential SMPP 3.4 PDUs

- [x] Implement `bind_transmitter` / response.
- [x] Implement `bind_receiver` / response.
- [x] Implement `bind_transceiver` / response.
- [x] Implement `unbind` / response.
- [x] Implement `enquire_link` / response.
- [x] Implement `generic_nack`.
- [x] Implement `submit_sm` / `submit_sm_resp`.
- [x] Implement `deliver_sm` / `deliver_sm_resp`.
- [x] Preserve optional TLVs on supported PDUs.
- [x] Preserve inbound request sequence numbers exactly in responses, including interoperability values through `0xffffffff`.
- [x] Add specification-driven encode/decode vectors.

## Phase 5 — Shared session state machine

- [x] Implement shared session states: Closed, Open, Outbound, Bound_TX, Bound_RX, Bound_TRX, Unbound.
- [x] Implement legal-operation validation by state and peer role.
- [x] Implement client and server role semantics on the same session core.
- [x] Keep codec independent from session state.
- [x] Implement clean bind/unbind lifecycle.
- [x] Make fatal decoder/framing errors transition the owning session to failure/closed exactly once.
- [x] Make state transitions race-free under concurrent send/receive/close/timeout activity.
- [x] Add state-transition and invalid-state tests.

## Phase 6 — TCP transport and optional TLS-over-TCP

- [x] Define transport around `net.Conn` semantics.
- [x] Implement plain TCP dial/listen using the standard library.
- [x] Implement optional TLS client/server adapters over TCP using `crypto/tls`.
- [x] Keep TLS configuration outside SMPP PDU/session packages.
- [x] Support caller-supplied `net.Conn` compatible transports that preserve TCP stream semantics.
- [x] Do not implement X.25 transport.
- [x] Ensure protocol-fatal close interrupts blocked RX/TX operations and is idempotent/concurrent-safe.
- [x] Add partial-read, partial-write and connection-close tests.

## Phase 7 — Asynchronous engine with synchronous public API

- [x] Implement independent long-lived RX and TX paths.
- [x] Implement session-local outbound sequence-number generation in `0x00000001..0x7fffffff`.
- [x] Accept inbound non-zero sequence values through `0xffffffff` without rejecting otherwise valid PDUs.
- [x] Implement out-of-order response correlation.
- [x] Implement bounded pending-request tracking.
- [x] Expose synchronous/context-aware public submit APIs without goroutine-per-message architecture.
- [x] Ensure inbound `deliver_sm` can be processed while outbound submit requests are outstanding.
- [x] Guarantee public `Client`, `Server`, `Session` and request APIs documented as concurrent-safe can be called from multiple goroutines at the same time.
- [x] Ensure request completion is exactly-once when response, timeout, cancellation, fatal protocol error and session loss race each other.
- [x] Add race tests and high-concurrency correlation tests.
- [x] Run `go test -race` for concurrent session/client/server scenarios.

> Phase 7 provides the exactly-once terminal completion primitive used by future protocol timeouts. The actual response-deadline scheduler and liveness timers remain Phase 9 work.

## Phase 8 — Windowing and backpressure

- [x] Implement configurable maximum outstanding-request window.
- [x] Implement blocking/context-cancellable acquisition for synchronous API calls.
- [x] Implement explicit overload/backpressure errors rather than unbounded queues.
- [x] Ensure timeout/cancellation/session-loss paths always release window capacity exactly once.
- [x] Record window utilization metrics/hooks.
- [x] Benchmark different window sizes against RTT profiles.
- [x] Avoid hard-coding the historical SMPP 3.4 recommendation of 10 outstanding requests.

## Phase 9 — Efficient timer and liveness subsystem

- [x] Implement a configurable per-request response timeout for outbound SMPP requests.
- [x] Start the protocol response timeout when the request PDU has been fully dispatched to the transport; time spent waiting for window/TX capacity remains governed by the caller context/deadline.
- [x] On response timeout, atomically remove/expire the pending request, release its window slot and return a typed timeout error to the synchronous caller.
- [x] Treat a response arriving after its request expired as a late/unmatched response; it must never complete an unrelated request.
- [x] Implement response deadlines without one independent `time.Timer` per request in the final high-throughput design.
- [x] Implement configurable Session Init timeout for connection-to-bind/session-establishment lifecycle.
- [x] Implement configurable Enquire Link interval/scheduling after SMPP inactivity.
- [x] Correlate `enquire_link` / `enquire_link_resp` through the same safe request/response machinery and apply a response timeout.
- [x] Implement configurable inactivity timeout based on session activity and define deterministic close/unbind behavior on expiry.
- [x] Ensure liveness timers are reset/update-safe under simultaneous RX and TX traffic.
- [x] Evaluate deadline buckets, heap batching, and/or timer wheel by benchmark.
- [x] Add boundary-race tests where response and timeout occur at nearly the same instant.
- [x] Add timeout-storm benchmark and memory-bound tests.

## Phase 10 — Client auto-reconnect

- [x] Implement configurable reconnect policy/backoff.
- [x] Re-bind automatically after reconnect.
- [x] Fail pending requests from the lost session deterministically.
- [x] Never silently auto-resubmit requests whose delivery state is ambiguous.
- [x] Treat fatal malformed-PDU closure like transport/session loss for reconnect policy, while preserving the protocol-failure reason in diagnostics.
- [x] Expose enough error metadata for application-level resubmission decisions.
- [x] Ensure reconnect, close and timeout transitions are race-free.
- [x] Add reconnect-during-full-window tests.

## Phase 11 — Server/SMSC mode

- [x] Implement TCP/TLS listener lifecycle.
- [x] Implement bind authentication hook/interface.
- [x] Implement per-session state and sequence spaces.
- [x] Enforce Session Init timeout for accepted connections that do not establish a valid SMPP session in time.
- [x] Implement inbound `submit_sm` dispatch and synchronous response path.
- [x] Implement outbound `deliver_sm` from server to bound RX/TRX sessions.
- [x] Apply outbound request response-timeout behavior to server-originated requests such as `deliver_sm`.
- [x] Close only the offending connection/session on a fatal malformed PDU; keep the listener and unrelated sessions healthy.
- [x] Add configurable connection/session limits.
- [x] Add slow-client and malicious-frame protection tests.

## Phase 12 — Message and encoding packages

- [x] Create encoding packages separate from protocol/session core.
- [x] Implement GSM 03.38/GSM 7-bit default alphabet encode/decode.
- [x] Implement GSM 7-bit extension-table characters and septet packing/unpacking.
- [x] Implement strict UCS-2/BMP encoding and decoding helpers.
- [x] Implement UTF-16BE Unicode encoding with surrogate-pair support for supplementary-plane characters such as emoji.
- [x] Provide a message-encoding selection helper that can prefer GSM 7-bit when representable and fall back to Unicode when required/configured.
- [x] Keep strict UCS-2 and UTF-16BE-with-surrogates behavior distinguishable so applications can match peer/carrier capabilities instead of silently emitting unsupported emoji.
- [x] Support binary payloads.
- [x] Implement UDH-based multipart segmentation/reassembly with limits calculated from encoded septets/code units, not Go rune count.
- [x] Implement SAR-TLV multipart support.
- [x] Keep message-content decoding optional on the protocol hot path.
- [x] Add conformance and boundary tests for GSM 7-bit, extension characters, Unicode BMP text, surrogate pairs/emoji, and multipart boundaries.

## Phase 13 — SMPP 3.4 completeness

- [x] Implement `data_sm` / response.
- [x] Implement `submit_multi` / response.
- [x] Implement `query_sm` / response.
- [x] Implement `cancel_sm` / response.
- [x] Implement `replace_sm` / response.
- [x] Implement `alert_notification`.
- [x] Implement `outbind` semantics.
- [x] Complete all SMPP 3.4 standard TLVs.
- [x] Complete SMPP 3.4 command-status coverage.
- [x] Run a full 3.4 conformance matrix.

## Phase 14 — SMPP 5.0 extensions

- [x] Add SMPP 5.0 capability negotiation on the shared core.
- [x] Implement `congestion_state` TLV support.
- [x] Implement adaptive flow-controller extension points.
- [x] Add SMPP 5.0 error/status additions.
- [x] Add SMPP 5.0 number-portability and endpoint-identification TLVs.
- [x] Implement Cell Broadcast commands and related TLVs.
- [x] Add SMPP 5.0 compatibility/conformance tests.

## Phase 15 — Observability without hot-path logging

- [x] Define zero/low-overhead counters and event hooks.
- [x] Expose requests sent/received, responses, response timeouts, session-init timeouts, inactivity expirations, enquire-link activity, outstanding window, RTT, reconnects and decode failures.
- [x] Emit mandatory structured error logs for fatal malformed/framing PDUs before/while terminating the offending session; these rare error logs are distinct from disabled-by-default per-PDU tracing.
- [x] Expose congestion-state data when available.
- [x] Provide optional packet tracing outside the default hot path.
- [x] Keep per-PDU logging disabled by default.

## Phase 16 — Performance simulator and benchmark laboratory

- [x] Build a minimal high-throughput SMPP peer simulator.
- [x] Add codec-only benchmarks.
- [x] Add in-memory/loopback session benchmarks.
- [x] Add localhost TCP benchmarks.
- [x] Add TLS-over-TCP benchmarks separately from plain TCP.
- [x] Add single-session bidirectional benchmark first.
- [x] Add 2-, 4-, and higher-session benchmarks only as needed to find the minimum session count meeting target.
- [x] Add request-timeout and liveness-timer overhead benchmarks under high outstanding counts.
- [x] Measure CPU, allocations, heap, GC, mutex contention and scheduler behavior.
- [x] Persist benchmark methodology/results in `.codex/PERFORMANCE.md` or dedicated reports.

## Phase 17 — Performance acceptance and optimization

> Historical reference acceptance passed on the documented `smpp-reference` 8-core/10-GB Linux/amd64 runner. A fresh 60-second run on the combined current HEAD after later library additions remains a separate P1 release-regression gate.

- [x] Sustain 100,000 aggregate bidirectional **request** PDUs/s on the reference machine.
- [x] Include required SMPP response processing in the end-to-end load.
- [x] Determine and document the minimum practical TCP connection/session count for the benchmark scenario.
- [x] Verify bounded memory under sustained load.
- [x] Verify no goroutine-per-message growth pattern.
- [x] Verify configured timeout tracking remains bounded and does not become a throughput bottleneck.
- [x] Profile before every significant optimization.
- [x] Keep implementation free of `unsafe` unless a later measured bottleneck justifies a separately reviewed decision.

**Reference acceptance evidence:** [docs/PERFORMANCE.md](docs/PERFORMANCE.md) records the 2026-10-06 Linux/amd64 reference run at 713,932 request-PDU/s for 60 seconds with one session. The report documents a local acceptance-script edit caveat; recorded parameters match current defaults, and no library source changed between the measured commit and the subsequent Go directive/docs-only commits. A new combined-HEAD run after later library additions remains a separate pre-release regression task.

## Phase 18 — Reliability, fuzzing and chaos

- [x] Test fragmented and coalesced TCP streams end to end.
- [x] Test out-of-order responses.
- [x] Test malformed, oversized and truncated PDUs end to end.
- [x] Test invalid `command_length`, impossible mandatory-field lengths, invalid TLV lengths and corrupted frames; assert the offending connection closes and no byte-stream resynchronization is attempted.
- [x] Verify each fatal structural/framing rejection emits the required diagnostic log without exposing sensitive message/authentication content by default.
- [x] Test duplicate/unexpected sequence numbers.
- [x] Test interoperability sequence numbers above `0x7fffffff` through `0xffffffff` on inbound requests.
- [x] Test disconnect during bind, idle state and full outstanding window.
- [x] Test delayed responses, late responses and timeout storms.
- [x] Test Session Init timeout, Enquire Link timeout/liveness and inactivity timeout scenarios.
- [x] Test simultaneous response-vs-timeout, timeout-vs-close and reconnect-vs-close races.
- [x] Test slow peers and application handlers.
- [x] Run `go test -race` scenarios.
- [x] Run sustained soak tests with concurrent API callers.

## Phase 19 — Release readiness

- [x] Freeze and document public API compatibility policy.
- [x] Document concurrency guarantees for every public mutable type.
- [x] Document timeout/liveness configuration and exact semantics.
- [x] Document fatal malformed-PDU connection-close and diagnostic logging policy.
- [x] Add examples for ESME client and SMSC/server.
- [x] Complete package documentation.
- [x] Document interoperability quirks and vendor-extension APIs.
- [x] Document GSM 7-bit and Unicode/emoji interoperability behavior and peer-capability caveats.
- [x] Publish reproducible performance results for the reference machine.
- [ ] Tag the first production-ready release.

## Phase 20 — Optional generic delivery-receipt parsing

> This additive message-layer helper preserves the SMPP 3.4 wire/session core and existing API. It defines protocol parsing only; application correlation and delivery policy remain outside the library.

- [x] **DLR-01:** Publish a pure `message`-package receipt parser for `protocol.DeliverSM`, with explicit no-receipt handling.
- [x] **DLR-02:** Apply text-first `id:`, TLV-fallback identifier precedence; independently apply TLV-first `message_state`, text `stat:` fallback; preserve conflict evidence and opaque IDs.
- [x] **DLR-03:** Reject malformed/ambiguous receipt TLVs and invalid states; remain panic-free and avoid guessing or vendor-only normalization.
- [x] **DLR-04:** Add receipt examples, mismatch/duplicate/malformed/ordinary message fixtures and tests.
- [x] **DLR-05:** Update additive public API snapshot and exhaustive no-panic coverage.
- [x] **DLR-06:** Document limitations and synchronize `.codex` handoff/backlog.
- [x] **DLR-07:** Verify Go 1.25/1.27 CI (format, unit, race, API, fuzz/coverage) and merge only after evidence.

**Implementation verification:** [PR #7](https://github.com/majiddarvishan/go-smpp/pull/7), [CI run 42305732414](https://github.com/majiddarvishan/go-smpp/actions/runs/42305732414) passed Go 1.25/1.27 matrices (format, unit, API/no-panic, lint, race, coverage/fuzz/performance smoke). DLR-07 was closed after [PR #7](https://github.com/majiddarvishan/go-smpp/pull/7) merged as `e4606a40961d62cf8eb917f28120385282e89b29`; final PR-head CI [42306339442](https://github.com/majiddarvishan/go-smpp/actions/runs/42306339442) passed both Go 1.25/1.27 matrices.

## Phase 21 — Product-neutral library audit and generic protocol completeness

> This repository is a reusable SMPP library, not an application integration. No downstream product may be imported, named, or used to define library APIs. Task details were registered in `.codex/TASKS.md` before implementation.

- [x] Remove consumer/product-specific material from README, docs, `.codex` and other maintained handoff files; retain only protocol-level facts and generic development-tool descriptions.
- [x] Record the product-neutral boundary in `AGENTS.md`, `.codex/DECISIONS.md`, and `.codex/ARCHITECTURE.md`.
- [x] Extend generic DLR parsing with all standard text receipt fields (`id`, `sub`, `dlvrd`, `submit date`, `done date`, `stat`, `err`, `text`) while preserving raw values.
- [x] Parse standard `deliver_sm` receipt TLVs (`receipted_message_id`, `message_state`, optional `network_error_code`) with strict length/duplicate handling. Tolerate response-oriented `delivery_failure_reason` and `additional_status_info_text` as peer-supplied diagnostics without treating either alone as receipt evidence.
- [x] Support IA5 and Latin-1 DLR text; explicitly report unsupported encodings rather than treating them as an ordinary non-receipt or guessing.
- [x] Add a typed accessor for the standard `protocol.MessageState` without changing existing public field types.
- [x] Add generic SMPP 3.4 `registered_delivery` parse/encode helpers with lossless reserved-bit round trips. Document the `0b11` SMSC receipt value as standards-reserved and expose success-only meaning only as an explicitly peer-specific alias.
- [x] Reconcile the historical backlog against implemented source/tests and correct stale Phase 12–14/17 status.
- [x] Align current status/handoff docs with the published reference acceptance result and remaining release gates.
- [x] Add comprehensive table-driven, malformed-input, round-trip, API snapshot and no-panic tests.
- [x] Pass the complete Go 1.25.x/1.27.x CI matrix and confirm final-head checks are green. PR #8 remains Draft pending maintainer review; no merge was performed.

**Phase 21 verification — 2026-11-07**

- Local `scripts/phase21-validation.sh` archive on commit `dff70d0968332e9fa78f4a4c80af744d00ab429a`: **37 PASS, 0 FAIL, 0 BLOCKED**, covering Go 1.25.0 and Go 1.27.0. The untracked `.bench/` directory was preserved.
- Follow-up correctness fix `ce14ae06a3d9096937c5c54a99a93aa8f695ece7` prevents response-only TLVs from identifying a `deliver_sm` receipt and adds regression coverage.
- Implementation-head GitHub Actions run [42369084385](https://github.com/majiddarvishan/go-smpp/actions/runs/42369084385) passed both Go 1.25.x and Go 1.27.x jobs, including gofmt, unit, race, fuzz (including DLR fuzz), benchmarks, 2-second resource/performance smoke and no-unsafe; Go 1.27.x also passed lint and coverage. The subsequent documentation-only PR-head run [42369351000](https://github.com/majiddarvishan/go-smpp/actions/runs/42369351000) also passed both jobs.
- The 2-second CI smoke is not the 60-second current-HEAD reference acceptance. Keep that P1 release gate open.

## P1 — Safe observability and packet capture (OBS-01)

- [x] Audit all library-owned packet tracing, decode-failure, panic, callback-error and fatal-protocol diagnostic paths for credential, address, message-body and arbitrary TLV leakage.
- [x] Keep raw PDU capture disabled unless both a tracer and the explicit raw-capture option are configured; preserve metadata-only tracing by default.
- [x] Ensure caller-controlled panic values, callback/decoder error messages and protocol `Reason` text are not copied into default logs, Observer events or error strings. Preserve programmatic fields and error identity without formatting untrusted values.
- [x] Document that every raw PDU may contain credentials, addresses, message content, identifiers or vendor TLVs; the generic library deliberately provides no unsafe universal redaction.
- [x] Document short-lived, isolated, access-restricted local capture; drop bind/outbind frames, sanitize all other frames with a peer-aware allowlist, prefer synthetic data, and never forward unsanitized raw frames to shared/third-party telemetry.
- [x] Add regression tests proving secret markers in authenticator panics, one-way handler errors, custom decoder errors and fatal decoder Reason are absent from logs/events/error strings.
- [x] Verify Go 1.25.x and 1.27.x format, unit, race, fuzz, lint, coverage and CI before closing OBS-01.

**OBS-01 verification — 2026-11-07**

- Source/test commits: `82c06bddafc482c6e336c10c84f5d904d12c50c3`, `b3ed89c58b28c38d0ce7e0d43b1bf89a0c8a0a59`, and final test correction `5e3f4678b1516c5eb4c5f84fd0af48d44ffe49a5`.
- GitHub Actions [run 42371052716](https://github.com/majiddarvishan/go-smpp/actions/runs/42371052716) passed both Go 1.25.x and Go 1.27.x: gofmt, unit, race, fuzz smoke, protocol/codec benchmarks, Phase 17 performance/resource smoke and no-unsafe; Go 1.27.x also passed lint and coverage. The final documentation/status PR head `64af94935c5c07af1c7995813c3b1a228c0c244d` passed both jobs in [run 42371554356](https://github.com/majiddarvishan/go-smpp/actions/runs/42371554356).
- Raw capture remains a separate explicit opt-in; no built-in PCAP/file persistence or generic vendor-TLV redactor was added. Metadata-only tracing remains the safe default.
- The 2-second CI performance smoke is not the 60-second reference acceptance; PERF-01 remains open.

## Backlog policy

Items intentionally deferred from the first submit/deliver milestone are tracked in `.codex/BACKLOG.md`. Moving an item out of backlog requires adding it to the appropriate phase here; completion is still recorded only by changing its checkbox to `[x]`.

## P1 — Interoperability fixture framework (INT-01)

- [x] Define a versioned, product-neutral fixture manifest distinguishing constructed synthetic examples from anonymized real-peer evidence.
- [x] Validate provenance, opaque peer aliases, SMPP profile, direction, expected decode outcome, and required sanitization attestations for real-peer fixtures.
- [x] Add deterministic offline stream replay through the existing framer/codec; assert command/header/body expectations and exact wire round-trip where explicitly declared.
- [x] Include only clearly labeled synthetic fixtures until reviewed peer exchanges are available. Never infer vendor behavior or claim deployed-peer interoperability from synthetic data.
- [x] Add an opt-in, product-neutral local integration probe for separate SMPP 3.4 simulator components in both roles: go-smpp ESME to external SMSC, and go-smpp SMSC to external ESME. Execute sequentially with one connection per simulator; read credentials only from hidden prompts/environment; keep all raw frames in memory and export only allowlisted, sanitized frames plus safe metadata. The probe is build-tagged and adds no public API or runtime dependency.
- [ ] Run the probe, review and add approved `simulator-peer` fixture candidates to the offline manifest; keep them distinct from `real-peer-anonymized` deployed-peer evidence.
- [ ] Add reviewed, anonymized deployed-peer exchanges and build the representative-peer strict/compatible behavior matrix from observed behavior.

**INT-01 framework verification — 2026-11-07**

- Implementation commits: task registration `62a4a5441f8389decc0ed6fedf623808873fc367`, fixture harness/docs `9bd79115444fd5408cb4d1a371a8f9e61a0934ce`, gofmt correction `9fc5ed44830365f8c9b24f5e1c2f3a4c9c261b40`.
- GitHub Actions [run 42375378458](https://github.com/majiddarvishan/go-smpp/actions/runs/42375378458) passed both Go 1.25.x and Go 1.27.x, including gofmt, lint (Go 1.27.x), unit, race, coverage, fuzz smoke, protocol/codec benchmarks, Phase 17 development performance/resource smoke, and no-unsafe.
- The manifest currently contains five constructed synthetic SMPP 3.4 PDUs. No real peer or vendor interoperability is claimed.
- The fixture harness is test-only; it adds no public API or runtime dependency. Bind/outbind frames are rejected, and real-peer entries require reviewed sanitization metadata.

**INT-01E verification — 2026-11-07**

- Final probe/sanitizer commit `05a6924563872e9a108b194e021dee70ec0e6c35` passed [GitHub Actions run 42380908792](https://github.com/majiddarvishan/go-smpp/actions/runs/42380908792) on Go 1.25.x and Go 1.27.x. This includes gofmt, lint, unit, race, fuzz, coverage, benchmark/performance checks, the opt-in sanitizer tests, and CLI compile validation.
- The hosted-runner Phase 17 60-second soak is not the dedicated 60-second reference acceptance on the reference environment; PERF-01 remains open.
- No live simulator session has been executed yet; the next step is the user's local run and review of the sanitized archive.

**Scope constraints:** test-only tooling and repository testdata; no public API, runtime dependency, product-specific behavior, raw PCAP persistence, bind/outbind credential frames, or unsanitized real traffic. Keep INT-01 open until simulator candidates are reviewed and deployed-peer evidence is added and reviewed.
