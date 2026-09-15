# Session Handoff

## Current state

Repository bootstrap/planning is complete. No Go implementation has started yet.

Current requirements have been captured in `PLAN.md`, `AGENTS.md` and the `.codex` documents.

## Confirmed requirements

- Both ESME/client and SMSC/server are required.
- One shared core for protocol/session logic.
- SMPP 3.4 complete first; architecture is SMPP 5.0-aware from the beginning.
- First functional path is bind/session + submit/deliver.
- Other 3.4 operations are deferred but mandatory later.
- Encoding/message functionality lives in separate packages in the same repository.
- Public API is synchronous/context-aware.
- Underlying engine is asynchronous and pipelined.
- Every outbound request expecting a response has a configurable response timeout.
- Response timeout starts after the request PDU is fully dispatched to the active transport.
- Window/TX admission waiting is controlled separately by caller context/deadline.
- Timeout completion must remove pending correlation and release the window exactly once.
- Late responses after timeout must not complete an unrelated request.
- Session Init timeout is required.
- Enquire Link scheduling plus response-timeout handling is required.
- Inactivity timeout is required.
- Client auto-reconnect/rebind is required.
- No hidden auto-resubmit of ambiguous requests.
- Active public runtime objects/APIs must be safe for concurrent use by multiple goroutines where documented.
- Internal state, correlation, window accounting, timers, close and reconnect paths must be data-race free.
- Minimal dependencies; standard library preferred.
- Linux/amd64 primary target.
- No `unsafe` initially.
- Plain TCP and TLS supported through transport abstraction.
- Extensible registries for standard/vendor PDUs and TLVs.
- Performance target is 100k aggregate bidirectional request PDUs/s on 8 cores / 10 GB RAM, with minimum practical connection count.

## Next task

Start **Phase 1 — Module skeleton and protocol primitives** from `PLAN.md`.

Before coding Phase 1:

1. Decide/pin the minimum supported Go version.
2. Sketch the public API names sufficiently to avoid package naming conflicts.
3. Define timeout error categories/types early enough that later session code does not collapse protocol response timeout into generic `context.DeadlineExceeded`.
4. Initialize the module as `github.com/majiddarvishan/go-smpp`.
5. Create only the package directories needed for Phase 1; avoid speculative package sprawl.
6. Add primitive unit tests and microbenchmarks with the first code.
7. Mark each completed Phase 1 checklist item `[x]` in `PLAN.md` in the same commit.

## Implementation cautions

- Do not implement codec/session logic in Phase 1 beyond what is needed to define/test primitive types.
- Do not introduce third-party dependencies without updating `.codex/DECISIONS.md`.
- Do not use a goroutine-per-message or goroutine-per-timeout model.
- Do not use one independent `time.Timer` per pending request as the final high-throughput timeout architecture.
- Do not treat one TCP read as one SMPP PDU.
- Do not hard-code a window of 10.
- Do not add automatic resubmission to reconnect logic later.
- Do not count response PDUs toward the stated 100k request-PDU/s target, although they must be processed during benchmarks.
- Do not make correctness depend on callers serializing access to a session.
- Any response/timeout/cancel/session-loss race must result in exactly one local completion and one window release.
- Run race-detector tests as concurrency code is introduced, not only at release time.

## Handoff update rule

At the end of every meaningful implementation session, replace/update this file with:

- branch and latest commit
- phases/steps completed
- files changed
- commands/tests/benchmarks executed and results
- unresolved failures or performance concerns
- exact next task

This file should remain concise enough to resume work quickly on another machine/session.
