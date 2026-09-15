# Agent Instructions

This repository is a performance-sensitive SMPP implementation. Before changing code or plans, read these files in order:

1. `PLAN.md`
2. `.codex/PROJECT_CONTEXT.md`
3. `.codex/DECISIONS.md`
4. `.codex/ARCHITECTURE.md`
5. `.codex/PERFORMANCE.md`
6. `.codex/BACKLOG.md`
7. `.codex/SESSION.md`

## Mandatory working rules

- `PLAN.md` is the implementation progress source of truth. When a step is completed and verified, change its checkbox to `[x]` in the same commit.
- Do not start a later phase by bypassing architectural constraints from earlier phases unless the plan and decision log are explicitly updated.
- SMPP 3.4 is the first complete compatibility target; the core must remain SMPP 5.0-aware and must not become a 3.4-only design.
- Client/ESME and server/SMSC must share the same protocol/session core wherever protocol semantics are common.
- Public API is synchronous/context-aware, but the engine underneath must be asynchronous and pipelined.
- Every outbound request expecting a response must support a configurable protocol response timeout.
- Protocol response timeout begins after the request PDU is fully dispatched to the transport; local window/TX waiting is separately governed by caller context/deadline.
- Response, timeout, cancellation, fatal protocol failure and session loss may race, but a request must complete locally exactly once and release its window slot exactly once.
- Implement Session Init timeout, Enquire Link scheduling/response timeout and inactivity timeout as first-class session features.
- Do not create a goroutine per message/request or per request timeout.
- Do not use one independent `time.Timer` per pending request as the final high-throughput design; benchmark shared deadline structures.
- Public active runtime objects/APIs documented for concurrent use must be safe for multiple goroutines without caller-side serialization.
- Sequence generation, pending correlation, state transitions, window accounting, timers, `Close`, reconnect and session-loss handling must be data-race free.
- Run `go test -race` as concurrency code is introduced.
- Do not introduce unbounded queues or unbounded pending-request growth.
- Do not automatically resubmit requests after an ambiguous connection failure.
- The SMPP decoder must fail closed on unrecoverable structural/framing corruption. Invalid `command_length`, impossible field/TLV boundaries or any corruption that makes the next PDU boundary untrustworthy must terminate that connection/session.
- Never attempt heuristic byte-stream resynchronization after a fatal framing error. Do not scan for a plausible next SMPP header on the same connection.
- Every fatal malformed/framing PDU that causes connection termination must emit a structured diagnostic error log. Include safe metadata when available; do not dump credentials or full message bodies by default.
- In server mode, malformed input terminates only the offending connection/session, not the listener or unrelated sessions.
- Prefer Go standard library. Any external dependency requires a documented reason in `.codex/DECISIONS.md`.
- Do not use `unsafe` unless profiling proves a need and the decision is recorded/reviewed first.
- Codec and PDU packages must not depend on session/network packages.
- TLS belongs in the transport layer and must remain independent from SMPP PDU/session semantics.
- Unknown/vendor TLVs must be preservable; vendor-specific TLVs and commands must be extensible through registries.
- Avoid mutable global registry state in active hot paths; prefer explicit immutable/frozen registry snapshots where practical.
- Avoid per-PDU logging in hot paths. Mandatory fatal-protocol error logging is an exception; normal packet tracing remains opt-in.
- Performance optimizations must be profile-driven and backed by benchmarks.

## Performance contract

Primary reference target:

- 100,000 aggregate bidirectional request PDUs/s (`requests sent + requests received`).
- Required response encoding/decoding/correlation is additional processing and must be included in end-to-end tests.
- Response-timeout/deadline tracking is part of the benchmarked hot path.
- Reference machine: Linux/amd64, 8 CPU cores, 10 GB RAM.
- Optimize for the minimum practical number of SMPP sessions/connections. Benchmark one session first, then increase only when necessary.

Do not reinterpret the target as 100k total PDUs/s unless the project owner explicitly changes the requirement.

## Scope discipline

The first functional milestone focuses on bind/session management plus `submit_sm` and `deliver_sm` request/response paths. Other SMPP 3.4 operations remain required for 3.4 completeness and are tracked in `PLAN.md` and `.codex/BACKLOG.md`.

When handing work to another session/system, update `.codex/SESSION.md` with current branch/commit, completed work, tests/benchmarks run, known problems and the exact next task.
