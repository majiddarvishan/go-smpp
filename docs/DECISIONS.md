# Project decisions

Short records of choices that were considered and deliberately settled, so that
they are documented rather than implicit and a later contributor can see what
was weighed before reopening one. `docs/API_COMPATIBILITY.md` says an
incompatible API correction must be "recorded in project decisions" before a
release tag; this is the file for that. Newest last.

Each record has: status, context, decision, why, what it costs, and when to
revisit it.

---

## D1 — Typed request/response wrappers on `session.Session` stay explicit

- **Status:** accepted, 2026-10-05 (review finding C5, Task 4.5).
- **Applies to:** `session/operations.go`.

### Context

`Session` exposes 17 exported operation methods. They fall into groups that
are not all the same shape:

| Group | Methods | Shape |
| --- | --- | --- |
| Request, typed response | `SubmitSM`, `DeliverSM`, `DataSM`, `SubmitMulti`, `QuerySM`, `BroadcastSM`, `QueryBroadcastSM` | `Request`, then `responseError`, then assert the body type, else `ErrUnexpectedPDU` with a command-specific message. About 14 lines each, identical apart from the command ID, the two types and the message text. |
| Request, typed response, shared helper | `BindTransmitter`, `BindReceiver`, `BindTransceiver` | One line each, delegating to a single unexported `bind` that already holds the shape once. |
| Request, status only | `CancelSM`, `ReplaceSM`, `CancelBroadcastSM`, `EnquireLink`, `Unbind` | `Request`, then `responseError`; no response body is returned. |
| One-way | `AlertNotification`, `Outbind` | `SendOneWay`; no response exists. |

A generic helper such as
`func request[Req, Resp any](ctx, s *Session, id protocol.CommandID, req Req) (Resp, error)`
would cover only the first group, saving on the order of 60 to 80 lines.

### Decision

Keep the wrappers explicit. Do not introduce a generic request helper.

### Why

1. **The exported surface does not shrink.** Every wrapper is an exported
   method and a godoc entry, and the API is about to be frozen
   (`docs/API_COMPATIBILITY.md`). A generic helper would be unexported, so the
   seven methods, their signatures and their documentation all remain; the
   saving is internal only. (This is the strongest argument *for* the helper,
   and it is a modest one.)
2. **Each method states its own contract in one place.** The command ID, the
   request type, the response type and the error text sit together, and a
   search for `func (s *Session) SubmitSM` lands on the whole story. With a
   helper, the same information is spread across a call and a type
   instantiation on the most-read path in the package.
3. **The shape is not guaranteed to stay uniform.** Only a subset of the
   operations share it today, and the SMPP 5.0 family already breaks the
   one-response-type assumption: the codec can return `protocol.OptionalResponse`
   instead of a command's primary response type (see the types discovered by
   `session/body_coverage_test.go`). A helper with a single `Resp` type
   parameter cannot express "this or that body" without extra machinery, and
   would then be bypassed for exactly the commands that differ, leaving two
   styles to maintain instead of one.
4. **The duplication is mechanical and visible.** There is no logic in it to
   get subtly wrong: no locking, no state, no retries. Those live in
   `Request`, `SendOneWay` and the engine behind them, which are not duplicated.

What is *not* a reason: the project's stdlib-only policy (`AGENTS.md`). That
policy is about runtime dependencies; generics are a language feature and add
none, so it does not argue either way here.

### What it costs

About seven near-identical function bodies. A defect in the shape (for
example, how a wrong body type is reported) has to be fixed in each. Nothing
enforces that they stay in step beyond review.

### Revisit when

- a defect has to be fixed identically in more than one of these wrappers; or
- more than about ten operations share the typed-response shape; or
- the response-body variants above are handled in one place that a helper
  could reuse.

If revisited, prefer an unexported generic helper that keeps every exported
method and its documentation unchanged, and add a test that drives all of the
wrappers through the same success, error-status and wrong-body cases first, so
the change is provably behaviour-preserving.
