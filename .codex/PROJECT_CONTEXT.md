# Project Context

## Project

Repository: `github.com/majiddarvishan/go-smpp`

Purpose: build a high-performance SMPP stack in Go that can be used both as an ESME/client and an SMSC/server.

This document captures stable project requirements so work can continue on another machine/session without re-explaining the project.

## Protocol references

Primary specifications supplied by the project owner:

- Short Message Peer-to-Peer Protocol Specification v3.4, Issue 1.2, 12-Oct-1999.
- Short Message Peer-to-Peer Protocol Specification v5.0, 19-Feb-2003.

The specifications are protocol references, not source-code dependencies. Do not copy large portions of specification text into the repository.

## Product requirements

- Implement both client/ESME and server/SMSC behavior.
- Use one shared protocol/session core for both sides wherever semantics are common.
- Complete SMPP 3.4 support first.
- Keep the architecture SMPP 5.0-aware from the beginning so 5.0 is an extension, not a rewrite.
- Initial functional focus is `submit_sm`/`submit_sm_resp` and `deliver_sm`/`deliver_sm_resp`, plus the session-management PDUs needed to operate them.
- Other 3.4 commands remain required and must stay in backlog/plan until implemented.
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
- If an inbound PDU has an unrecoverable structural/framing error that can make subsequent TCP bytes unsafe to interpret (for example an invalid `command_length` or impossible field/TLV length), close that connection/session immediately after recording the failure. Do not attempt byte-stream resynchronization on the same connection.
- Fatal malformed-PDU closure must be logged with safe structured diagnostic metadata. Do not dump credentials or full message payload by default.
- A server must isolate this failure to the offending connection; the listener and unrelated sessions continue normally.
- Minimize external dependencies; prefer the standard library.
- Initial target is Linux/amd64.
- Initial implementation must not use `unsafe`.
- Transport must support plain TCP and TLS.
- Extensible registry is required for vendor-specific TLVs and future/custom commands.

## Performance requirement

The project owner's `100k` requirement means:

> Sustain 100,000 aggregate SMPP **request PDUs per second**, counting requests sent and requests received at the same time, using the minimum practical number of SMPP connections/sessions.

Responses are not counted as request throughput, but response processing is mandatory SMPP work and therefore must be included in end-to-end performance tests.

Reference machine:

- Linux/amd64
- 8 CPU cores
- 10 GB RAM

The project should benchmark one session first. If peer/window/RTT/network constraints make one session insufficient, increase session count only as needed and record the smallest count that meets the target.

Timeout/deadline tracking is part of the hot path and must be benchmarked at realistic outstanding-window sizes. The final design must not rely on one independent `time.Timer` per outstanding request if that prevents the performance target.

## SMPP implementation facts that drive the architecture

- SMPP runs over a byte-stream transport; a single TCP read is not one PDU.
- Every PDU has a fixed 16-byte header and uses `command_length` for framing.
- Framing trust is connection-scoped: if a structural decode error makes alignment unsafe, the connection is discarded rather than heuristically resynchronized.
- SMPP is asynchronous: several requests may be outstanding and responses may arrive out of order.
- Request/response correlation is session-local and based on `sequence_number`.
- A reconnect establishes a new session; pending operations from a lost session cannot be correlated with the new session.
- TX, RX and TRX session modes must be supported.
- SMPP sessions require liveness/session timers such as request response timeout, Session Init, Enquire Link and inactivity handling.
- SMPP 5.0 adds useful flow-control information such as `congestion_state` and must fit into the same core.
- TLV optional parameters are the primary extension mechanism and unknown/vendor TLVs must not force a closed type system.

## Concurrency correctness rules

- Do not require application-level serialization around an active session for normal concurrent sends.
- A response and timeout racing for the same request must have one winner only.
- Timeout, context cancellation, fatal decoder failure, close and session loss must not double-release a window slot or double-notify a caller.
- `Close` must be safe to call concurrently and must not race with reconnect into reviving a deliberately closed client.
- Registries should avoid mutable global hot-path state; prefer immutable/frozen session-visible snapshots.
- `go test -race` is part of normal development for session/client/server concurrency tests.

## Fatal malformed-PDU policy

Structural errors are treated more severely than ordinary SMPP semantic/status errors. If the decoder cannot safely trust the frame boundary, it must stop using that byte stream. Typical examples are invalid `command_length`, body/mandatory-field lengths that cannot fit in the declared frame, TLV lengths that overrun the PDU, or another corruption that would require guessing the next PDU boundary.

Required behavior:

1. classify the failure as a fatal protocol/framing error,
2. emit a structured error log with safe metadata when available,
3. stop reading/decoding additional PDUs from that connection,
4. close the transport/session exactly once,
5. fail pending requests according to normal session-loss rules,
6. do not attempt to locate a plausible next SMPP header in the remaining byte stream.

Recoverable protocol/application errors where framing remains trustworthy may still be handled with SMPP response status or `generic_nack` as appropriate.

## Non-goals for the first implementation milestone

The following are intentionally not required before the initial submit/deliver path works correctly:

- Full SMPP 3.4 command coverage.
- SMPP 5.0 Cell Broadcast support.
- Adaptive congestion controller.
- Every GSM alphabet/segmentation feature.
- Vendor-specific behavior beyond proving the registry/extension mechanism.
- `unsafe`-based optimizations.

These items remain part of the overall roadmap.
