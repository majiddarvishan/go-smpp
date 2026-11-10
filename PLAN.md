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
- [x] Run the probe against the two local SMPP 3.4 simulator components, review the sanitized archive, and add only the two approved `simulator-peer` fixtures (`simulator-smsc-peer-001`, `simulator-esme-peer-002`) to the manifest; keep them distinct from deployed-peer evidence.
- [ ] Add reviewed, anonymized deployed-peer exchanges and build the representative-peer strict/compatible behavior matrix from observed behavior.
- [x] Improve the opt-in probe's ESME-originated `submit_sm` observation workflow with a longer configurable window, an explicit just-in-time prompt, and sanitized Unbind request/response diagnostics; keep it build-tagged and outside the public API. Final code commit `ca5dfad5df1afdd9d80f8d742634149b59a6fd7e` passed Go 1.25.x/1.27.x CI in [run 42386492884](https://github.com/majiddarvishan/go-smpp/actions/runs/42386492884).
- [x] Re-run the ESME phase using the operator-controlled ESME CLI: after `OBSERVE`, issue exactly one outbound `submit_sm` within the configured window; verify `submit_sm_resp` and characterize `unbind_resp` behavior. The 2026-11-08 archive shows TRX bind, enquire_link, deliver_sm_resp, ESME-originated submit_sm and submit_sm_resp all observed successfully; the operator confirmed this ESME simulator does not support Unbind.
- [x] Make the test-only bind gate observe the actual successful `bind_transceiver_resp` write rather than polling transient session state; preserve a safe, allowlisted session-close cause when the response is not written. Cover the response/close race and peer EOF with regression tests. Final head `0cf19df7fe3dd145a07b839745a3be5bd4870358` passed full CI on Go 1.25.x and Go 1.27.x in [run 42393189702](https://github.com/majiddarvishan/go-smpp/actions/runs/42393189702).
- [x] Respect a known ESME peer that does not support Unbind: default to an explicit `UNSUPPORTED` event without sending the request, close the local session, and provide a strict opt-in for capable peers. Add regression tests; do not alter the library Unbind implementation. Final head `4657d5e6c239c3abfd54fcf13e5804cb1310a018` passed full CI on Go 1.25.x and Go 1.27.x in [run 42474275074](https://github.com/majiddarvishan/go-smpp/actions/runs/42474275074).

**INT-01 framework verification — 2026-11-07**

- Implementation commits: task registration `62a4a5441f8389decc0ed6fedf623808873fc367`, fixture harness/docs `9bd79115444fd5408cb4d1a371a8f9e61a0934ce`, gofmt correction `9fc5ed44830365f8c9b24f5e1c2f3a4c9c261b40`.
- GitHub Actions [run 42375378458](https://github.com/majiddarvishan/go-smpp/actions/runs/42375378458) passed both Go 1.25.x and Go 1.27.x, including gofmt, lint (Go 1.27.x), unit, race, coverage, fuzz smoke, protocol/codec benchmarks, Phase 17 development performance/resource smoke, and no-unsafe.
- The manifest initially contained five constructed synthetic SMPP 3.4 PDUs; two reviewed `simulator-peer` fixtures from the local run were subsequently added. No deployed real-peer or vendor interoperability is claimed.
- The fixture harness is test-only; it adds no public API or runtime dependency. Bind/outbind frames are rejected, and real-peer entries require reviewed sanitization metadata.

**INT-01E verification — 2026-11-07**

- Final probe/sanitizer commit `05a6924563872e9a108b194e021dee70ec0e6c35` passed [GitHub Actions run 42380908792](https://github.com/majiddarvishan/go-smpp/actions/runs/42380908792) on Go 1.25.x and Go 1.27.x. This includes gofmt, lint, unit, race, fuzz, coverage, benchmark/performance checks, the opt-in sanitizer tests, and CLI compile validation.
- The hosted-runner Phase 17 60-second soak is not the dedicated 60-second reference acceptance on the reference environment; PERF-01 remains open.
- The first live simulator run is recorded below; the sanitized archive was reviewed and two simulator fixtures were added. The ESME-originated submit action still needs a deliberate trigger, and both simulators' missing `unbind_resp` behavior remains an open compatibility finding.

**Local simulator evidence — 2026-11-07**

- Reviewed sanitized archive: `go-smpp-interop.qgB9QT.tar.gz`; no passwords, bind frames, raw PDU files, original addresses, IDs or message content were committed.
- SMSC simulator: TRX bind, enquire_link, submit_sm (`registered_delivery=0x03`), submit_sm_resp and parsed `DELIVRD` receipt all PASS. Unbind request was sent; no `unbind_resp` was observed within the 8-second operation deadline.
- ESME simulator: TRX bind, enquire_link, synthetic deliver_sm and deliver_sm_resp all PASS. No outbound ESME submit_sm was observed during the original 5-second window. Unbind request was sent; no `unbind_resp` was observed within the operation deadline.
- Added reviewed fixtures `simulator-smsc-peer-001` and `simulator-esme-peer-002` to `testdata/interop/manifest.json`. Both remain explicitly `source=simulator-peer`; they do not establish deployed-peer/vendor compatibility.
- Probe follow-up: default ESME submit observation window increased to 15 seconds and is configurable through `SMPP_INTEROP_SUBMIT_WAIT`; a just-in-time prompt and safe Unbind request/response diagnostics were added for both client and server roles. Final code commit `ca5dfad5df1afdd9d80f8d742634149b59a6fd7e` passed Go 1.25.x/1.27.x CI in [run 42386492884](https://github.com/majiddarvishan/go-smpp/actions/runs/42386492884).
- SMPP 3.4 permits Unbind from either role and expects the matching `unbind_resp`; the missing response is retained as an observed simulator compatibility gap, not normalized away.



**ESME CLI-driven simulator run — 2026-11-08**

- Reviewed sanitized archive `go-smpp-interop.avJydg.tar.gz`. It contains the report, run log and sanitized manifest candidate only; no password or raw PDU files.
- The live run passed `listen`, TRX `bind_transceiver` (SMPP 3.4), `enquire_link`, synthetic `deliver_sm_resp`, and the operator-triggered ESME `submit_sm`. The candidate contains the matching successful `submit_sm_resp` with the same sequence number.
- The probe sent server-initiated Unbind and observed no `unbind_resp` within 8 seconds. The operator then confirmed the ESME simulator does not support Unbind. This is a peer test-harness capability limitation, not a go-smpp library failure; do not infer Unbind success.
- Probe follow-up adds an explicit `UNSUPPORTED` event and skips server-initiated Unbind by default in the ESME/server phase, closing the local session afterward. Strict Unbind exchange is opt-in via `SMPP_INTEROP_ATTEMPT_ESME_UNBIND=true` or `-attempt-esme-unbind=true` for peers that support it. The library's Unbind semantics remain unchanged. Regression tests cover default skip, strict opt-in success, and strict timeout failure. Full CI passed on Go 1.25.x and Go 1.27.x at final source head `4657d5e6c239c3abfd54fcf13e5804cb1310a018` in [run 42474275074](https://github.com/majiddarvishan/go-smpp/actions/runs/42474275074).

**Follow-up ESME CLI run analysis — 2026-11-07**

- Reviewed sanitized archive `go-smpp-interop.MLqt5S.tar.gz`. The run lasted about 0.55 seconds and contains only `run.log`, `report.json`, and an empty candidate manifest; no raw PDU or credential material was persisted.
- The probe received a TRX bind attempt and the configured authenticator accepted it: a rejected bind would have produced the explicit SMPP `command_status` and allowlisted reason, not the generic `error_type=*`.
- The previous probe then reported `session closed before reaching Bound_TRX` through a generic `*errors.errorString`. Its 10 ms state polling could miss a short-lived Bound_TRX state if the ESME closed immediately after receiving `bind_transceiver_resp`; the old report also discarded `Session.Err()`, so this archive cannot distinguish peer EOF/reset from another terminal cause.
- `frames_dropped_or_excluded=1` is expected here: the inbound bind frame is deliberately excluded by the credential-frame sanitizer. `frames=0` does not mean no bind request arrived.
- No `OBSERVE` prompt was reached, so no CLI `submit_sm` command could be issued. The ESME simulator is operator-controlled; the correct order is READY → start ESME → successful bind/delivery handshake → OBSERVE → issue one CLI `submit_sm`.
- Probe follow-up replaces transient state polling with the session's post-write `EventResponseSent` observation for `bind_transceiver_resp`, prioritizes an already-observed response over a simultaneous close, and retains a safe session-close cause (EOF/reset/fatal protocol classification). The CLI prompt and instructions are clarified. Regression tests cover the response/close race and peer EOF; full CI passed on Go 1.25.x and Go 1.27.x in [run 42393189702](https://github.com/majiddarvishan/go-smpp/actions/runs/42393189702) at final head `0cf19df7fe3dd145a07b839745a3be5bd4870358`.

**ESME session-continuity investigation — 2026-11-08**

- Reviewed sanitized archive `go-smpp-interop.g7u649.tar.gz`. TRX bind passed; `enquire_link` failed with `*session.LossError` whose cause was `*net.OpError` on write; `deliver_sm` failed with `server.ErrUnknownSession` (reported as `*errors.errorString`); a later ESME `submit_sm` and `submit_sm_resp` were captured.
- `ErrUnknownSession` establishes that the session passed to `Server.DeliverSM` was no longer owned by the server. The prior submit handler did not retain session identity, so the later submit may have arrived on a replacement connection and must not be counted as success for the original bind. Report `sanitized_frames_captured=1` also differed from the later candidate's two frames: the old code took the report snapshot before deferred server shutdown, allowing late trace callbacks from another active session.
- Probe-only follow-up on the current branch: bind the submit observation to its `*session.Session`; report PASS only on the original bind session, otherwise FAIL with `submit_received_on_replacement_session`; add `session_health_after_enquire_link` including state, Done status, server ownership and sanitized close cause/network error; mark DeliverSM BLOCKED when the original session is already closed; close the server and join all active sessions before final evidence snapshot; suppress mixed-session fixture candidates.
- Added regression coverage for same-session versus replacement-session Submit, peer EOF cause retention, and safe broken-pipe classification. No public library/session behavior has been changed. Root cause of the initial write error (local versus peer/OS transport condition) remains to be established from the next run's close-cause classification.
- INT-01L remains open pending one repeat live ESME-mode run. Do not use the `g7u649` or `WgVM1N` runs to claim same-connection Submit success.

**Per-socket TCP lifecycle trace follow-up — 2026-11-08**

- Reviewed `go-smpp-interop.WgVM1N.tar.gz`. It contains only `run.log`, `report.json`, and an empty candidate manifest; no PCAP/TCP trace or raw PDU data. The report records a read-side `ECONNRESET`, the original Session as Closed and no longer owned by the server, and a later Submit on a replacement session. Three sanitized frames were retained and four were excluded; the exact wire ordering was not present in the archive.
- A read-side `ECONNRESET` establishes that the local TCP stack reported a reset while reading. It does not identify whether the remote application, its OS, or another network component initiated the reset. The Session then closes its socket while handling that terminal read error; that consequence must not be mistaken for the initiating action.
- The build-tagged interop probe now wraps each accepted socket with a metadata-only lifecycle tracer. It assigns a connection ID, records direction/command ID/status/sequence/length for complete SMPP frames, records read/write error class, and records local `Close` order plus any prior I/O error. No PDU payload, credentials, addresses, or raw TCP bytes are persisted.
- Bind response correlation now uses the exact accepted socket rather than a shared Observer channel. Submit observations include that socket's connection ID and must still match the original Session. This distinguishes an initial connection from automatic reconnect/replacement.
- The reset classification reason is now `tcp_reset_received`, not `peer_reset_connection`, to avoid attributing the sender to the peer application without evidence. Public library/session behavior is unchanged. Regression tests cover fragmented bind response header parsing, per-connection correlation, and reset-before-local-close ordering.
- Full CI passed at source head `2a53a445a92e67264b02058131136c176b6e477e` on Go 1.25.x and Go 1.27.x in [run 42478439742](https://github.com/majiddarvishan/go-smpp/actions/runs/42478439742), including lint/coverage, opt-in interop tests, race, fuzz, benchmark and performance/resource smoke gates. Follow-up commit `29c6d813adcbfc743cde10fafc74b340aea6b50b` additionally waits for the Server.Serve accept loop to exit before taking the final trace snapshot; Go 1.25.x passed its full matrix and Go 1.27.x Lint was still running at the time of this note. Live ESME rerun remains pending.

**Scope constraints:** test-only tooling and repository testdata; no public API, runtime dependency, product-specific behavior, raw PCAP persistence, bind/outbind credential frames, or unsanitized real traffic. Keep INT-01 open until deployed-peer evidence is added and reviewed.


**ESME EOF before Bind Response — 2026-11-08**

- Reviewed the uploaded `go-smpp-interop.wvS2vr.tar.gz` (report window `12:11:03.149Z`–`12:11:05.330Z`). It contains only `run.log`, `report.json` and an empty candidate manifest; no PCAP or raw TCP data.
- Connection 1 was accepted, then the complete 45-byte `bind_transceiver` request (`command_id=0x00000009`, sequence `1006632970`) was read. The authenticator accepted TRX credentials. Before any successful outbound `bind_transceiver_resp` was observed, the server's next read returned EOF (`peer_closed_connection`). Only after that read error did go-smpp locally close the session (`prior_io_error=read:peer_closed_connection`). This is an orderly peer stream close, not the previous TCP-reset case; the trace does not explain why the ESME process closed.
- Connection 2 was accepted immediately after connection 1 closed and was locally closed 37 microseconds later. The old trace did not identify the close caller. New test-only trace state marks local closes occurring after probe shutdown begins, so the next run can distinguish runner cleanup from an independent socket failure.
- The final `tcp_write_error` on connection 1 reported `local_connection_closed` after its `tcp_local_close`; it is a consequence of the session already being closed, not the initiating failure. New trace metadata records the attempted SMPP header on failed writes only (command ID/status/sequence/length), allowing confirmation that the pending write was `bind_transceiver_resp` without retaining payload bytes.
- No submit, delivery or DLR exchange occurred; `sanitized_frames_captured=0`, one bind frame was excluded, and the candidate manifest is empty. Do not count this as a successful simulator exchange.
- The probe uses distinct decimal-only IDs: `12345678901234567890` for the test `submit_sm_resp` and `9876543210987654321` for the unrelated synthetic DLR. SMPP wire encoding remains a NUL-terminated C-Octet String. The generic library does not generate these IDs (`server.SubmitResult.MessageID` is supplied by the application), and the generic DLR parser continues preserving peer IDs as opaque strings; no global numeric-only parser restriction is added.
- Added regression coverage for attempted Bind Response header metadata on a failed write, marking local closes during probe shutdown, and numeric-only MessageID validation for the `submit_sm_resp` and synthetic DLR paths. Source/test head `3cb8ff9f59ba392b7fde2f54b9ee941c8b04505e` passed the full Go 1.25.x/1.27.x CI matrix in [run 42481521309](https://github.com/majiddarvishan/go-smpp/actions/runs/42481521309), including gofmt, lint, unit, opt-in interop, coverage, race, fuzz, benchmark, performance/resource smoke and no-unsafe gates. The subsequent successful ESME repeat and current full CI status are recorded in the 2026-11-08 successful-repeat section below.
- Next evidence needed: one repeat ESME run with the new trace, plus ESME simulator's own log for the bind attempt. If the reason remains opaque, run a FIN/RST-only loopback capture on the same host to confirm the remote endpoint's close sequence; do not capture payload because bind requests contain credentials. INT-01L remains open. PR #8 remains Draft/open; no merge or release.



## Successful ESME/server repeat — 2026-11-08

- Reviewed `go-smpp-interop.2KhVNK.tar.gz`. It contains a complete SMPP 3.4 exchange on one TCP connection: TRX Bind/BindResp, EnquireLink/EnquireLinkResp, DeliverSM/DeliverSMResp, and ESME-originated SubmitSM/SubmitSMResp. All PDU trace events use `connection_id=1`; the Submit event confirms `session_match=true`. The candidate excludes both Bind frames and retains the six safe non-bind frames.
- Correct command IDs: `0x00000004` is `submit_sm`; `0x80000004` is `submit_sm_resp`. The last trace pair is Submit, not Unbind. No Unbind PDU/response is present in this archive.
- The `unbind=UNSUPPORTED` event is the separate server-initiated Unbind test (SMSC → ESME). The known ESME simulator does not support this operation, so it is skipped; the runner then closes its local session. Report metadata now makes the initiator, peer role, operation kind and request direction explicit. The library's Unbind implementation is unchanged.
- Reviewed and added a separate manifest fixture `simulator-esme-peer-002-submit`, evidence `INT-01-PEER-002-002`, with six sanitized frames. The previous peer-002 fixture remains unchanged because it records a different run and a prior strict Unbind attempt.
- The loopback capture's local time is UTC+03:30. FIN from `127.0.0.1:4000` to client port `46662` at `16:43:51.816897`, followed by peer FIN at `16:43:51.816946`, aligns with the report's local Close at `13:13:51.816872Z`. The active session shut down gracefully; it did not RST. Other RST/ACK packets target different ephemeral ports and occurred before listener startup, immediately after shutdown, or after probe exit. This is consistent with refused connection attempts while no listener is accepting; because SYNs were excluded by the tcpdump filter and trace omits remote ports, the initiating local process cannot be identified.
- The earlier `wvS2vr` peer EOF before Bind Response was not reproduced. That trace showed the ESME peer closed first and go-smpp closed only after EOF; the later write error was secondary. The successful repeat demonstrates the probe and peer can complete the handshake and message exchange. No public Session/Transport change is justified.
- Probe report metadata now clarifies Unbind direction; tests cover the new fields. CI has a small diagnostic improvement to print `gofmt -d` when formatting fails. Latest branch head `dea096d71e6acd31fb192a050d90fad1124a1a74` passed the full Go 1.25.x/1.27.x CI matrix in [run 42485670483](https://github.com/majiddarvishan/go-smpp/actions/runs/42485670483). INT-01L is complete; deployed-peer evidence and the reference-host performance gate remain open. PR #8 remains Draft/open and unmerged.


### INT-01L reopened after recurring ESME peer EOF — 2026-11-08

The follow-up archive `go-smpp-interop.nmFaln.tar.gz` reproduces the earlier peer-close-before-BindResp symptom after the successful `2KhVNK` exchange. The report contains no PCAP or ESME-side logs. On connection 1, the 45-byte Bind was read at `13:41:15.971049832Z`; peer EOF arrived at `13:41:15.971342852Z` (293 µs later). The authenticator accepted the Bind. No BindResp was written; the local session closed after the peer EOF, and the later BindResp write failed with `local_connection_closed`. Connection 2 was accepted only during probe shutdown and carried no observed PDU; its initiator is unknown.

This makes the symptom intermittent rather than resolved. The evidence does not justify changing public Session/Transport behavior. INT-01L is reopened and requires ESME-side logs for the exact bind attempt, or a FIN/RST-only loopback packet summary if simulator logs are unavailable. Never capture SMPP payloads or credentials. PR #8 remains Draft/open; no merge or release.


### Follow-up FIN/RST-only capture — 2026-11-08 (18:01 local)

- The supplied capture was filtered to FIN/RST only and includes no SYN or ordinary ACK packets; no simultaneous probe `report.json` or ESME console log was provided. Therefore ephemeral ports cannot be mapped to probe `connection_id` values or to a specific Bind attempt.
- At `18:01:40.274839`, client-side `127.0.0.1:46680` sent FIN to server `127.0.0.1:4000`. At `18:01:40.275994`, the server sent FIN back to `46680`. This tuple is consistent with a graceful close initiated by the client side.
- A distinct tuple `4000 → 46686` shows server FIN at `18:01:40.275480` followed by server RST at `18:01:40.275770`; without the earlier handshake and ACK sequence, the reason is undetermined.
- Other server-originated RSTs target different ephemeral ports at `18:01:31`, `18:01:34`, and `18:01:37`. They may be connection attempts/refusals or stale traffic; the present filter cannot establish which process initiated them.
- This capture supports that at least one client-side TCP endpoint initiated a graceful close, but does not yet prove that it is the same socket/session as the reported Bind EOF. No code change is justified.
- Next capture: include only TCP SYN/FIN/RST flags (no payload), and run it concurrently with one ESME probe while saving the sanitized probe report. Correlate timestamps and client ephemeral ports. Ask for ESME console diagnostics around the same attempt if available.


### Bind authentication failure and peer decoder compatibility (2026-11-10 review of 2026-11-08 evidence)

The `oagXgd` ESME interoperability run reported:

- `bind_transceiver` was received and rejected with `command_status=0x0000000e` (`StatusInvalidPassword`), reason `password_mismatch`; this is an actual authentication rejection, not a timeout or a missing BindResp.
- The server **successfully wrote** `bind_transceiver_resp` (`0x80000009`) with the same sequence `2915041288`, error status `0x0000000e`, and `command_length=16`. The separate ESME simulator printed `(MPP/SmppBindResp.cpp,191) - DECODE - BindResp, (packetPtr - packet.c_str()) != command_length`.
- The concurrent header-only loopback capture confirms the TCP connection to `127.0.0.1:4000` succeeded on client port `52560` at `18:10:49.334922` local, followed by a server FIN at `18:10:49.349865` and client FIN at `18:10:49.350057`. Earlier SYN→RST pairs occur when no listener responds to the connection attempt. The packet filter includes no SMPP payload.
- **Conformance:** SMPP 3.4 Issue 1.2 sections 4.1.2 and 4.1.4 explicitly omit the whole Bind Response body when `command_status` is nonzero. Section 4.1.6 defines the same response-family body shape for transceiver; unlike the two preceding sections it does not repeat that explicit note. The shared library follows the protocol's error-response body-omission convention: nonzero status produces a header-only 16-byte Bind Response; successful responses carry a NUL-terminated `system_id` (17+ bytes). Specification: https://smpp.org/SMPP_v3_4_Issue1_2.pdf.
- The ESME parser's offset-mismatch diagnostic is **consistent with** incorrectly requiring a `system_id` terminator in a failed BindResp, but the actual `SmppBindResp.cpp` source has not been inspected; treat this as a peer-decoder compatibility hypothesis, not a proven code defect.
- Do **not** append a nonstandard NUL to the library's failed Bind Responses or bypass/relax authentication to make this peer pass. Fix the local credential mismatch without sharing any secret. The ESME parser should handle a header-only nonzero-status BindResp rather than trying to parse successful-bind fields.
- Codec golden-wire regression tests now cover all three SMPP 3.4 BindResp variants, exact 16-byte invalid-password framing and success C-Octet String bodies; a separate real TCP/server test checks rejection and success framing through the authenticator. No public library behavior changed. CI and an operator retest with locally matched credentials remain to be completed.
- A prior unrelated CI run on `c7e2479` failed only at Go 1.25's `FuzzParseDeliveryReceipt` smoke with `context deadline exceeded`; unit and race tests passed there. The new focused test's initial CI also failed because its synthetic passwords exceeded the SMPP 3.4 field limit; corrected to within 8 octets in `e0ec9ff`. Do not mislabel these test-fixture mistakes as production defects.


### BindResp wire audit verification — 2026-11-10

- Final source head `f5dc82fdf1230c794229f016d248aca0790b3193` passed both Go 1.25.x and Go 1.27.x in [GitHub Actions run 42784198156](https://github.com/majiddarvishan/go-smpp/actions/runs/42784198156). Full matrix passed: gofmt, lint (Go 1.27), unit tests, opt-in interop tests, coverage floor, race tests, fuzz smoke, protocol/codec benchmarks, Phase 17 performance smoke, sustained resource-bound smoke, and no-unsafe.
- The first version of the server regression used synthetic passwords exceeding SMPP 3.4's field limit; they were corrected. It then assumed successful BindResp length was exactly system_id+header, but the real server legitimately returned sc_interface_version TLV, so the success assertion was corrected to accept the TLV while checking system_id. Full CI passed after both fixes.
- INT-01M's source audit and regression tests are complete; the peer's own BindResp decoder needs a local follow-up when the ESME sends credentials matching those supplied privately at the probe prompt. A failed bind still must be reported as an authentication error; the library must not silently pad its header-only error response.
- Earlier Go 1.25 fuzz smoke failure (`context deadline exceeded`) at the old `c7e2479` head did not recur in the fully passing final matrix; no production code or CI thresholds were changed on the basis of that isolated event. PR #8 remains Draft/open and unmerged.


### Successful repeat with an empty precursor TCP connection — 2026-11-10 (`CpcJZ3`)

- Reviewed user-provided sanitized archive `go-smpp-interop.CpcJZ3.tar.gz` (`report.json`, `run.log`, `manifest-candidate.json`); run window `08:49:48.101316757Z`–`08:49:59.057931413Z`. The verdict is **PASS**, `OVERALL failures=0 blocked=0 candidates=1`; six non-Bind frames were captured, two Bind frames excluded for privacy. There is no raw PDU/PCAP in the archive.
- TCP connection 1 was accepted at `08:49:49.880562930Z`, received **no SMPP PDU**, then returned `peer_closed_connection` at `08:49:49.948583312Z` (approximately 68 ms later); the library's local Close followed. The process/purpose of that short-lived connection is **not established**. It is distinct from earlier failures where the peer closed **after** sending Bind.
- TCP connection 2 was accepted at `08:49:49.949844124Z`; `bind_transceiver` (45 bytes, sequence `3745513482`) was read and `bind_transceiver_resp` (37 bytes, `command_status=0`, same sequence) was written. The session remained `Bound_TRX`, `server_active_sessions=1`, `server_owns_session=true`, and `session_match=true`.
- On connection 2, `enquire_link` and `enquire_link_resp` matched sequence `1`; `deliver_sm` and `deliver_sm_resp` matched sequence `2` with status zero; the manually issued ESME `submit_sm` and the library's `submit_sm_resp` matched sequence `3292528651` with response status zero. This also confirms round-trip preservation of the peer's high-bit inbound sequence numbers on both BindResp and SubmitSMResp.
- The probe's `unbind=UNSUPPORTED` means **SMSC-initiated** Unbind was intentionally not attempted for this ESME simulator. The completed SubmitSM response was followed by a local Close by the probe; no peer Unbind exchange was recorded.
- The candidate manifest's six sanitized frames were checked for declared length, command ID, status, and sequence consistency; all checks passed. The candidate has `sanitization.reviewed=false` and reuses ID `simulator-esme-peer-002` / evidence `INT-01-PEER-002-001`, which **already belongs to an existing reviewed fixture from another run**. Do not overwrite/re-import it. The existing `simulator-esme-peer-002-submit` fixture already covers this exact six-command exchange; this independent run is recorded as further evidence, not a redundant fixture.
- Compared with earlier `oagXgd`, a valid successful 37-byte BindResp shows that matching credentials make the normal exchange succeed. This does **not** prove how the peer decoder handles a nonzero-status, 16-byte BindResp; keep those questions separate. Earlier intermittent post-Bind EOF remains open as INT-01L, while the local ESME interoperability success path is verified. Do not modify public codec, Session, authentication or Transport on the basis of this passing run.
- PR #8 remains Draft/open. No merge, tag, or release. Reference-host 60-second performance acceptance and deployed-peer evidence are still pending.


### Pre-Bind TCP disconnect recovery regression — 2026-11-10

- Reviewed `go-smpp-interop.CpcJZ3.tar.gz`: first TCP connection closed after ~68 ms without a PDU; second connection completed Bind, EnquireLink, DeliverSM, ESME SubmitSM and responses with `session_match=true`. Report PASS/zero failures.
- The sanitized candidate has six structurally valid frames, excludes both credential-bearing Bind frames, and is `reviewed=false` under an already-used `simulator-esme-peer-002` fixture ID/evidence ref. Its semantic commands duplicate existing reviewed `simulator-esme-peer-002-submit`; do not import as a conflicting duplicate. High-bit Submit sequence `3292528651` and Bind sequence `3745513482` are preserved in responses.
- Added generic `server/prebind_peer_close_test.go`: server `MaxSessions=1`; five consecutive connections accepted then closed without any PDU, each slot reclaimed; a subsequent TRX Bind succeeds. No production code changed.
- Full Go 1.25.x/1.27.x CI for source commit `5a1d2500a362fc0d405d5aed8d02d2e2550001ea` passed in [run 42788172657](https://github.com/majiddarvishan/go-smpp/actions/runs/42788172657) attempt 2. Initial Go 1.25 fuzz smoke failed with `context deadline exceeded` on `FuzzFramerChunked` during a 2s fuzz run; unit/race passed. A rerun of only the failed job succeeded. No thresholds or fuzz algorithms were changed.
- INT-01N complete. INT-01L remains open because the earlier occasional **post-Bind** peer EOF is a distinct unresolved event. Local simulator interoperability is verified; deployed-peer evidence and 60-second reference-host performance acceptance remain outstanding. PR #8 stays Draft/open; no release/tag.
