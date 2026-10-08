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

---

## D2 — Declared minimum Go version

- **Status:** **decided and applied 2026-10-06.** The maintainer's decision is a floor of **Go 1.25**, with newer releases through **Go 1.27** supported, and on his explicit instruction `go.mod` now says `go 1.25`. (Review finding on `go 1.26.0`, Task 6.6.) It was applied in a scratch-verified but not directly compiled state: the environment used for this record has no Go 1.25 toolchain, so the first build of the real `go.mod` is the maintainer's; see "What was and was not verified after applying" below.
- **Applies to:** the `go` directive in `go.mod`, and the sentences that quote it (README, release checklist).

### Context

`go.mod` says `go 1.26.0`. A toolchain older than that either downloads a newer one (`GOTOOLCHAIN=auto`) or refuses to build ("requires go >= 1.26.0"), which on an air-gapped build host is a hard stop. The question the finding asks is whether any Go 1.26 feature is actually needed.

### Evidence

The whole module was built, vetted and tested, with the `go` directive in a scratch copy set to each value below and `GOTOOLCHAIN=local` so no toolchain was fetched. "Tests" means `go test -count=1 ./...` over all 10 packages, including the root package's architecture, API-contract and supply-chain tests.

| Toolchain | `go` line | Build | Vet | Tests |
| --- | --- | --- | --- | --- |
| 1.21.9 | 1.21 | ok | ok | ok |
| 1.22.2 | 1.22 | ok | ok | ok |
| 1.23.1 | 1.23 | ok | ok | ok |
| 1.24.13 | 1.24 | ok | ok | ok |
| 1.24.13 | 1.21 | ok | ok | ok |
| 1.24.13 | 1.22 | ok | ok | ok |

The race detector (`session`, `server`, `client`, `transport`) is also clean on 1.21.9 / line 1.21 and on 1.24.13 / line 1.21. The maintainer has separately run the full suite on the real Go 1.26 toolchain.

What this shows:

- **No Go 1.26 feature is load-bearing, and nothing newer than Go 1.21 is.** A 1.21 compiler accepts every package, test file, example and command in the module.
- **1.21 is the floor, and it is a hard one**: `log/slog`, used by `session` and `cmd/smpp-sim`, first shipped in Go 1.21. Go 1.20 was not tested (no toolchain available) and cannot work as the code stands.
- **The code does not depend on newer language semantics.** The `go` line selects language and runtime defaults: per-iteration loop variables arrive with a 1.22 line and the current timer-channel semantics with a 1.23 line. Rows 5 and 6 compile with a modern toolchain under the old semantics, and pass. So the library works under both old and new semantics, which matters because a consumer's own `go.mod` decides which apply to their binary.

### Decision

Declare **Go 1.25** as the minimum and support every release above it, which now includes 1.27. What backs this, and what does not:

- 1.21, 1.22, 1.23 and 1.24 were each tested directly (table above). The maintainer ran the suite on a Go 1.26 toolchain. The reference acceptance run's `environment.txt` records **go1.27.0**, and that run includes a race-detector pass of `session`, `client` and `server`, so 1.27 is confirmed on the maintainer's machine. Go 1.25 and 1.26 were not available in the environment used for this record, so **1.25 is bracketed by a passing 1.24 and a passing 1.27, not tested on its own**. `GOTOOLCHAIN=go1.25.0 go test -count=1 ./...` settles it.
- Go 1.25 is the maintainer's chosen floor, higher than the verified 1.21; it is not a limit of the code. One thing to weigh: if Go 1.27 is already released, upstream supports only 1.26 and 1.27, so a 1.25 floor means promising a toolchain that has just left upstream support. That is a legitimate choice, but it is the point of the trade-off above.
- **Supporting 1.27 means testing it.** CI should run a matrix of the floor and the newest release (`1.25.x` and `1.27.x`) once the CI stage opens, otherwise "supports 1.27" is a hope.
- The reference-acceptance toolchain is **Go 1.27.x**: `scripts/acceptance.sh` now requires `go1.27*` (changed on the maintainer's decision that 1.26 is not wanted as the reference), matching the recorded run on go1.27.0. It is a separate requirement from the module's minimum.

### Options

1. **Keep `go 1.26.0`.** No change, no new test burden. Cost: excludes any operator who cannot run a current toolchain.
2. **Lower to `go 1.21`**, the verified floor. Widest adoption. Cost: you now promise a toolchain that upstream stopped supporting long ago, so those users also run a standard library (`crypto/tls`, `crypto/x509`, `net`) without current security fixes. That is their choice, but it is part of what the library is then deployed on.
3. **Lower to an intermediate value** (for example `go 1.22`): the same evidence supports it. It narrows the promise to something less ancient but is otherwise arbitrary; the principled choices are 1 and 2.

### Recommendation

Option 2 **only together with a CI job that builds and tests on the floor**, plus one on the newest release. A declared minimum nobody tests is a claim that rots, and today CI installs only Go 1.26. If you do not want to maintain an old-toolchain job, choose option 1; that is a legitimate answer, not a failure.

### If you apply it

```sh
sed -i 's/^go 1.26.0$/go 1.25/' go.mod
```

and in the same commit:

- README: the sentence "(`go.mod` requires Go 1.26.0)" in the supply-chain section (change it to 1.25).
- `docs/RELEASE_CHECKLIST.md`: the minimum-Go-version item.
- CI (held until the CI stage): a matrix of the floor and the newest release; for example `go-version: ['1.25.x', '1.27.x']`.

Things that do **not** change, checked: `.github/workflows/ci.yml` and `reference-acceptance.yml` install Go with a literal `1.26.x`, not `go-version-file`, so lowering `go.mod` does not change which toolchain they use; and `scripts/acceptance.sh` requires a 1.26.x *toolchain* regardless of the `go` line, which is right, since the reference result should be taken on the current compiler.

### Revisit when

A feature newer than the floor is wanted for a good reason (then raise the floor deliberately and record it here), or when upstream's support window moves far enough from the floor that supporting it becomes a liability.

### What was and was not verified after applying

`go.mod` was changed with the one line above, and the README, `AGENTS.md` and `.codex/` statements of the minimum were updated with it; `.codex/DECISIONS.md` D-025 keeps its text and gains a "superseded" note. The environment this was done in cannot build a `go 1.25` module (its toolchains are 1.21 to 1.24, and `GOTOOLCHAIN=local` refuses a newer requirement), so the verification that matters is a run on a real 1.25 toolchain and on 1.27:

```sh
gofmt -l . && go build ./... && go vet ./... && go test -count=1 ./... && go test -race ./... && scripts/coverage.sh
```

Left alone on purpose, because CI changes are held: `.github/workflows/ci.yml` and `reference-acceptance.yml` still install Go `1.26.x`, so nothing automated tests the 1.25 floor or 1.27 yet, and `reference-acceptance.yml` would now install a toolchain that `scripts/acceptance.sh` rejects (see the CI stage checklist in `.claude/TASKS.md`). `scripts/acceptance.sh` names the reference-acceptance environment (Go 1.27.x), not the module's minimum.
