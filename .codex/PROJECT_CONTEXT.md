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
- Client must support automatic reconnect/rebind.
- Never silently auto-resubmit an ambiguous request after connection loss.
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

## SMPP implementation facts that drive the architecture

- SMPP runs over a byte-stream transport; a single TCP read is not one PDU.
- Every PDU has a fixed 16-byte header and uses `command_length` for framing.
- SMPP is asynchronous: several requests may be outstanding and responses may arrive out of order.
- Request/response correlation is session-local and based on `sequence_number`.
- A reconnect establishes a new session; pending operations from a lost session cannot be correlated with the new session.
- TX, RX and TRX session modes must be supported.
- SMPP 5.0 adds useful flow-control information such as `congestion_state` and must fit into the same core.
- TLV optional parameters are the primary extension mechanism and unknown/vendor TLVs must not force a closed type system.

## Non-goals for the first implementation milestone

The following are intentionally not required before the initial submit/deliver path works correctly:

- Full SMPP 3.4 command coverage.
- SMPP 5.0 Cell Broadcast support.
- Adaptive congestion controller.
- Every GSM alphabet/segmentation feature.
- Vendor-specific behavior beyond proving the registry/extension mechanism.
- `unsafe`-based optimizations.

These items remain part of the overall roadmap.
