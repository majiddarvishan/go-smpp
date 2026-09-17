# Session Handoff

## Current state

Branch: `main`

Phase 0 through Phase 13 are complete and verified locally. Phase 14 is next.

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

## Exact next task

Start **Phase 14 — SMPP 5.0 extensions** from `PLAN.md`. Keep one shared core, negotiate peer capabilities per session, add v5-only TLVs/statuses and Cell Broadcast operations without weakening SMPP 3.4 compatibility or fatal-framing safety.
