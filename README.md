# go-smpp

A high-performance SMPP stack for Go, designed for both ESME/client and SMSC/server use cases.

## Project goals

- Shared protocol core for client and server roles.
- SMPP 3.4 complete support first, with an architecture that is SMPP 5.0-aware from day one.
- Initial production hot path: `submit_sm` / `submit_sm_resp` and `deliver_sm` / `deliver_sm_resp`.
- Synchronous public API backed by an asynchronous, pipelined protocol engine.
- Configurable per-request response timeout after a request is sent.
- SMPP Session Init timeout, Enquire Link liveness handling and inactivity timeout.
- Thread-safe/concurrent-safe active client, server and session APIs for documented concurrent operations.
- Automatic reconnect without hidden automatic resubmission of ambiguous requests.
- Extensible PDU/TLV registry, including vendor-specific TLVs.
- Encoding support in separate packages within this repository.
- Minimal external dependencies; prefer the Go standard library.
- Primary target: Linux/amd64.
- Minimum supported Go version: 1.26.
- No `unsafe` in the initial implementation.

## Network transport

The SMPP network transport is **TCP**. X.25 is intentionally out of scope.

Plain TCP is the baseline transport. TLS is optional and, when enabled, is layered over TCP using the Go standard library `crypto/tls`. The SMPP codec/session layers remain independent from TLS configuration.

## Timeout and liveness model

Outbound SMPP requests that expect responses have a configurable protocol response timeout. The timeout starts after the request PDU has been fully dispatched to the active transport; time spent waiting for local window/TX capacity is controlled separately by the caller context/deadline.

A request may complete by response, timeout, cancellation or session loss, but local completion and window release must happen exactly once. Late responses after timeout are treated as late/unmatched and must not complete another request.

The session layer also provides configurable Session Init timeout, Enquire Link scheduling/response handling and inactivity timeout.

## Concurrency model

The wire protocol core is asynchronous and bidirectional even though the public request API is synchronous. Active runtime objects are designed to be safely shared by multiple goroutines where documented. Sequence allocation, pending correlation, session state, window accounting, timers, close and reconnect paths must be data-race free. The implementation must not use a goroutine per message/request.

## Malformed PDU policy

If an inbound PDU has a fatal structural/framing error that makes the TCP stream boundary untrustworthy, the library logs the failure and closes that connection/session. It does not attempt byte-stream resynchronization. Recoverable SMPP semantic/status errors remain separate and may receive the appropriate protocol response.

## Performance target

The reference target is **100,000 SMPP request PDUs per second aggregate, bidirectionally** (requests sent + requests received), while using the minimum practical number of TCP connections/sessions. Mandatory SMPP responses are additional work and are included in end-to-end benchmark load.

Reference machine:

- CPU: 8 cores
- RAM: 10 GB
- OS/arch: Linux/amd64

Performance work starts with a single-session benchmark and scales to the smallest session count required to sustain the target. No fixed claim is made that every peer/network can achieve the target on one session; RTT, peer window limits, throttling, and network conditions are part of the benchmark contract.

## Protocol scope

The design is based on:

- SMPP v3.4, Issue 1.2, 12-Oct-1999
- SMPP v5.0, 19-Feb-2003

SMPP 3.4 is the first complete compatibility target. SMPP 5.0 features are added on the same core rather than as a separate stack.

## Current implementation status

Phase 1 is complete. The repository now contains the Go module/package skeleton, protocol primitive types/constants, SMPP 3.4/5.0 profiles, typed timeout/fatal-protocol error categories, dependency-direction tests, unit tests, primitive benchmarks, and Go 1.26 CI. Binary PDU framing/codec implementation starts in Phase 2.

## Planning and project context

- [`PLAN.md`](PLAN.md) — phased implementation plan and progress checklist.
- [`AGENTS.md`](AGENTS.md) — entry point for coding agents.
- [`.codex/PROJECT_CONTEXT.md`](.codex/PROJECT_CONTEXT.md) — project goals and constraints.
- [`.codex/DECISIONS.md`](.codex/DECISIONS.md) — architectural decisions.
- [`.codex/ARCHITECTURE.md`](.codex/ARCHITECTURE.md) — target architecture and package boundaries.
- [`.codex/PERFORMANCE.md`](.codex/PERFORMANCE.md) — performance contract and benchmark strategy.
- [`.codex/BACKLOG.md`](.codex/BACKLOG.md) — deferred protocol/features backlog.
- [`.codex/SESSION.md`](.codex/SESSION.md) — handoff/current-state notes.
