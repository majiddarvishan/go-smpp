# Project Context

## Project

Repository: `github.com/majiddarvishan/go-smpp`

Purpose: build a high-performance SMPP stack in Go that can be used both as an ESME/client and an SMSC/server.

Current implementation baseline: Go 1.26, Linux/amd64, TCP transport.

## Protocol references

Primary specifications supplied by the project owner:

- Short Message Peer-to-Peer Protocol Specification v3.4, Issue 1.2, 12-Oct-1999.
- Short Message Peer-to-Peer Protocol Specification v5.0, 19-Feb-2003.

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
- Framing trust is connection-scoped: if a structural decode error makes alignment unsafe, the connection is discarded rather than heuristically resynchronized.
- SMPP is asynchronous: several requests may be outstanding and responses may arrive out of order.
- Request/response correlation is session-local and based on `sequence_number`.
- A reconnect establishes a new session; pending operations from a lost session cannot be correlated with the new session.
- TX, RX and TRX session modes must be supported.
- SMPP sessions require request-response timeout, Session Init, Enquire Link and inactivity handling.
- SMPP 5.0 adds capabilities such as `congestion_state` that must fit into the same core.
- TLV optional parameters are the primary extension mechanism and unknown/vendor TLVs must not force a closed type system.

## Concurrency correctness rules

- Do not require application-level serialization around an active session for normal concurrent sends.
- A response and timeout racing for the same request must have one winner only.
- Timeout, context cancellation, fatal decoder failure, close and session loss must not double-release a window slot or double-notify a caller.
- `Close` must be safe to call concurrently and must not race with reconnect into reviving a deliberately closed client.
- Registries should avoid mutable global hot-path state; prefer immutable/frozen session-visible snapshots.
- `go test -race` is part of normal development for session/client/server concurrency tests.

## Current implementation state

Phase 1 is complete: module/package skeleton, protocol primitive constants/types, 3.4/5.0 profiles, typed timeout and fatal protocol error categories, dependency-direction test, unit tests, primitive benchmarks and Go 1.26 CI are present. Phase 2 begins the binary framing/codec implementation.
