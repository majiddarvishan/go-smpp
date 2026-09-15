# Project Context

## Project

Repository: `github.com/majiddarvishan/go-smpp`

Purpose: build a high-performance SMPP stack in Go that can be used both as an ESME/client and an SMSC/server.

Current implementation baseline: Go 1.26, Linux/amd64, TCP transport.

## Protocol references

Primary specifications supplied by the project owner:

- Short Message Peer-to-Peer Protocol Specification v3.4, Issue 1.2, 12-Oct-1999.
- Short Message Peer-to-Peer Protocol Specification v5.0, 19-February-2003.

The specifications are protocol references, not source-code dependencies.

## Product requirements

- Implement both client/ESME and server/SMSC behavior.
- Use one shared protocol/session core for both sides wherever semantics are common.
- Complete SMPP 3.4 support first.
- Keep the architecture SMPP 5.0-aware from the beginning so 5.0 is an extension, not a rewrite.
- Initial functional focus is `submit_sm`/`submit_sm_resp` and `deliver_sm`/`deliver_sm_resp`, plus the session-management PDUs needed to operate them.
- Other 3.4 commands remain required and must stay in backlog/plan until implemented.
- Network transport is TCP. X.25 is out of scope.
- Optional TLS, when used, runs over TCP and stays in the transport layer.
- Provide encoding/message functionality as separate packages in the same repository.
- GSM 03.38/GSM 7-bit encode/decode, extension-table handling, and septet packing/unpacking are required.
- Unicode message support must distinguish strict UCS-2/BMP from UTF-16BE with surrogate pairs; emoji support uses the latter when the configured peer/carrier capability permits it.
- Public API should be synchronous/context-aware.
- Underlying protocol engine must still be asynchronous, pipelined and capable of out-of-order response correlation.
- Every outbound request that expects a response must support a configurable response timeout.
- The protocol response timeout starts after the request PDU has been fully dispatched to the transport; local queue/window waiting is governed separately by caller context/deadline.
- A timed-out request must be completed exactly once, removed from pending correlation and release its window slot exactly once.
- Late responses after timeout must be treated as late/unmatched and must not complete another request.
- Session Init timeout is required.
- Enquire Link scheduling and response-timeout handling are required.
- Inactivity timeout is required.
- Client must support automatic reconnect/rebind.
- Never silently auto-resubmit an ambiguous request after connection loss.
- Public active runtime objects and request APIs must be safe for concurrent use by multiple goroutines where documented.
- Internal sequence allocation, session state, pending correlation, window accounting, timeout/cancel/close/reconnect paths must be data-race free.
- Locally generated sequence numbers stay in `0x00000001..0x7fffffff`.
- Received sequence numbers accept the interoperability range `0x00000001..0xffffffff`; responses must preserve the received sequence number exactly.
- Fatal structural/framing corruption terminates the offending TCP connection/session after structured diagnostic logging; no byte-stream resynchronization is attempted.
- A server must isolate malformed input to the offending connection; the listener and unrelated sessions continue normally.
- Minimize external dependencies; prefer the standard library.
- Minimum supported Go version is 1.26.
- Initial implementation must not use `unsafe`.
- Extensible registry is required for vendor-specific TLVs and future/custom commands.

## Performance requirement

The project owner's `100k` requirement means:

> Sustain 100,000 aggregate SMPP **request PDUs per second**, counting requests sent and requests received at the same time, using the minimum practical number of TCP connections/sessions.

Responses are not counted as request throughput, but response processing is mandatory SMPP work and therefore must be included in end-to-end performance tests.

Reference machine:

- Linux/amd64
- 8 CPU cores
- 10 GB RAM

The project benchmarks one session first, then increases connection count only if measurement requires it.

## SMPP implementation facts that drive the architecture

- SMPP is processed as a TCP byte stream; a single TCP read is not one PDU.
- Every PDU has a fixed 16-byte header and uses `command_length` for framing.
- The framer handles fragmented PDUs and multiple coalesced PDUs in one TCP read.
- The framer can emit complete PDUs already present in an input buffer without copying; fragmented PDUs use bounded buffering.
- The initial default maximum PDU size is 1 MiB and is configurable.
- Framing trust is connection-scoped: if a structural decode error makes alignment unsafe, the framer is poisoned and the connection is discarded rather than heuristically resynchronized.
- SMPP is asynchronous: several requests may be outstanding and responses may arrive out of order.
- Request/response correlation is session-local and based on `sequence_number`.
- A reconnect establishes a new session; pending operations from a lost session cannot be correlated with the new session.
- TX, RX and TRX session modes must be supported.
- SMPP sessions require request-response timeout, Session Init, Enquire Link and inactivity handling.
- SMPP 5.0 adds capabilities such as `congestion_state` that must fit into the same core.
- TLV optional parameters are the primary extension mechanism and unknown/vendor TLVs must not force a closed type system.
- Supported SMPP 3.4 PDU decoders preserve ordered optional TLVs, including duplicates and unknown/vendor tags.
- `short_message` and `message_payload` are mutually exclusive carriers of message user data for the supported submit/deliver path.
- Active sessions use independent long-lived RX and TX paths over `net.Conn`; there is no goroutine per request/message.
- Bounded pending correlation supports out-of-order response completion and exact-once terminal ownership.
- A successful full `WriteFull` call is the transport boundary after which future protocol response-timeout accounting may start.

## Concurrency correctness rules

- Do not require application-level serialization around an active session for normal concurrent sends.
- A response and timeout racing for the same request must have one winner only.
- Timeout, context cancellation, fatal decoder failure, close and session loss must not double-release a window slot or double-notify a caller.
- `Close` must be safe to call concurrently and must not race with reconnect into reviving a deliberately closed client.
- Registries should avoid mutable global hot-path state; prefer immutable/frozen session-visible snapshots.
- `go test -race` is part of normal development for session/client/server concurrency tests.

## Current implementation state

Phase 0 through Phase 7 are complete and verified. The shared state machine, TCP/TLS-over-TCP transport boundary, long-lived RX/TX engine, synchronous/context-aware public request API, bounded pending correlation, out-of-order completion, high inbound sequence interoperability, fatal-protocol structured logging/connection close, and concurrent client/server/session lifecycle are implemented.

Phase 8 is next and will add the real configurable SMPP outstanding-request window/backpressure policy. The existing `MaxPending` and TX queue bounds are defensive safety bounds, not a substitute for Phase 8. The actual response timeout scheduler, Session Init timer, Enquire Link scheduling/timeout, and inactivity timer remain Phase 9 work. Auto-reconnect remains Phase 10. Full SMSC/server policy remains Phase 11. GSM 7-bit and Unicode/emoji encoding remains Phase 12.
