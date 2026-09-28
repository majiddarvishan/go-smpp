# Concurrency guarantees

The active runtime is designed for concurrent Go callers.

| Type / API | Guarantee |
| --- | --- |
| *client.Client | Concurrent-safe. Bind/request methods, Session, metrics, reconnect inspection, WaitConnected, and Close may be called by multiple goroutines. A request is bound to one session snapshot and is never replayed onto a replacement session. |
| *server.Server | Concurrent-safe for Sessions, server-originated sends, and Close. Only one Serve call may be active. |
| *session.Session | Concurrent-safe for all exported operations. Multiple requests may be in flight and responses may complete out of order. Close is idempotent and may race with requests, responses, timeouts, and transport loss. |
| *session.StateMachine | Concurrent-safe. State reads are atomic and lifecycle transitions are serialized internally. |
| *codec.RegistryBuilder | Concurrent-safe during configuration. Freeze serializes with registration; after freeze, further registration fails. |
| *codec.Registry | Immutable after construction and safe for concurrent reads. |
| *codec.Framer | Not concurrent-safe. It is state for one ordered byte stream and must have one owner feeding bytes in TCP order. |
| Protocol/message/config value structs | Caller-owned values. Concurrent reads are fine when the caller does not mutate them. Concurrent mutation of the same value or backing slice requires caller synchronization. |

## Callback rules

session.Handler, server.Authenticator, server.SubmitHandler, session.Observer, session.PacketTracer, session.WindowObserver, and session.FlowController are application callbacks.

A callback instance shared by multiple sessions can be invoked concurrently and must provide its own synchronization if it mutates shared state. Callbacks on the session hot path should return promptly. The library does not spawn a goroutine per callback to hide slow application code.

## Exactly-once request completion

A request can race among response arrival, protocol response timeout, caller cancellation, fatal protocol failure, and session loss. Exactly one path removes the pending entry and releases the window slot. Late or duplicate responses do not complete a newer unrelated request.

The implementation uses a bounded pending table, bounded request window, shared deadline manager, and fixed long-lived session goroutines. It does not create a goroutine or independent timer per message/request.

## Session goroutines and liveness supervision

Each session runs exactly three long-lived goroutines: `rxLoop` (read, frame, decode, dispatch), `txLoop` (drain, batch, write), and `deadlines.run` (the one shared min-heap timer). There is no fourth, ticker-driven liveness goroutine: it was removed by Task 2.4 (Finding P5), which also removed the 10 ms tick-resolution floor that goroutine imposed on sessions with short timers.

The three liveness checks are three fixed `deadlineItem` slots owned by `Session` — `sessionInitDeadline`, `inactivityDeadline`, `enquireLinkDeadline` — scheduled once in `New` and rescheduled in place with `deadlineManager.scheduleItemAt`, so keeping them alive allocates nothing. `expireDeadline` routes them by pointer identity before falling through to per-request timeout handling. Only `deadlines.run`'s single goroutine ever schedules or fires these slots after `New`, so unlike a request's deadline they have no concurrent canceller to arbitrate against and need no lock of their own.

- **Session-init** is one-shot: it fires once at `SessionInitTimeout` after creation and terminates the session only if it is still Open/Outbound. If the session already bound, nothing remains for it to do, and a session never returns to Open afterward.
- **Inactivity** and **enquire_link** do not reschedule on every packet. When one fires, it re-derives idle time from the live `lastActivityTime()`. If the session isn't bound yet, or activity was recorded after the slot was armed, it is not time to act, and the slot reschedules itself for the moment idle would actually reach its threshold. So `noteActivityAt` remains a single atomic store that never touches the heap, and a busy session wakes each slot roughly once per interval instead of once per PDU.

**The enquire_link probe runs in a short-lived goroutine. This is a deliberate, narrow exception to the no-goroutine-per-timeout rule, and it is worth being explicit about.** `tryEnquireLink` blocks until the probe's response arrives or its own response-timeout deadline fires — and that deadline lives on the same heap `deadlines.run` services. Calling it synchronously from `fireEnquireLinkDeadline` would therefore have `deadlines.run` wait on a deadline that only `deadlines.run` can fire: a self-deadlock, not a slow tick. It was hit in practice while implementing Task 2.4 (`TestAutomaticEnquireLinkTimeoutClosesSession` failed with the session never closing) and it does not recover on its own. The probe therefore runs in its own goroutine, and `fireEnquireLinkDeadline` reschedules its slot and returns immediately. The rate of that goroutine follows idle time, not message volume: it fires only when the session has been idle for a full `EnquireLinkInterval`, and under real traffic it essentially never does. Successive probes cannot pile up in the normal case, because dispatching a probe records activity and so resets idle before the next check (`TestLivenessRescheduleKeepsHeapAndGoroutinesBounded` asserts both the heap and the goroutine count stay bounded across 20+ probe cycles). A goroutine-free alternative exists — dispatch the probe without waiting, mark its pending entry, and have `expireDeadline` terminate the session when a marked entry times out — but it needs a new dispatch-only variant of `request()`, which is the most heavily depended-on function in this package, so it was not attempted here.

Behavior is otherwise unchanged from the ticker-based design: `ErrWindowFull` from a probe skips that cycle without terminating (Task 1.3), and any other probe failure terminates the session.

## Completion channel lifetime

Each `Request` allocates its own `chan requestResult` and lets it become garbage once the request completes. This channel used to be pooled on `Session` (a fixed-size ring recycled across requests), saving one small allocation per request. The pool was removed (Finding B4, Task 1.5): it was correct only as long as nothing could still be mid-send on a channel at the moment it went back in the pool, which was an invariant spread across `request()`, `pendingTable`, `handleResponse`, and the deadline manager and enforced by nothing mechanical — a single-value drain on release would have silently masked a two-sends bug rather than surfacing it. Against the allocation work already planned for Phase 2 (frame-buffer pooling, decoded-response arenas), the saving was marginal, so the correctness risk was not worth carrying.

Measured cost of the removal, `BenchmarkInMemorySessionBidirectional/sessions_1` (`internal/perflab`, `-benchmem`, single in-process session, this host):

| | Pooled (before) | Per-request alloc (after) |
| --- | --- | --- |
| allocs/op | 10 | 12 |
| B/op | 873 | 1009 |

ns/op and request_pdu/s moved within normal run-to-run noise (~3050 vs ~2930 ns/op) and are not attributable to this change. The two extra allocations and ~136 B/op are the one-channel-per-request cost; there is no generation counter or other mitigation in place, since deleting the pool was the chosen fix rather than the atomic-generation-check alternative.

## Outbound frame pool lifetime

`request()`, `SendOneWay()`, and the response path (`queueResponse()`) each encode into a buffer drawn from a per-session `sync.Pool` (`session/pool.go`, `Session.frames`) instead of a fresh `nil` slice (Finding P1, Task 2.2). The buffer is sized from `codec.EncodedPDUSizeHint` when a hint is available (submit/deliver request and response bodies today; see Task 2.6), and `EncodePDU` grows it further on its own when the hint is absent or undershoots.

**Write-completion boundary.** A buffer is returned to the pool once `txLoop`'s write call for the batch containing it has returned — success or failure — *and*, on the success path specifically, once `tracePacket` has been called for that item. `tracePacket` is the only code that reads `item.frame` after the write; it never retains a bare reference to it (`PacketTrace.RawPDU` is nil unless `Config.TraceRawPDU` is set, in which case it is an owned copy — see `session/observability.go`). `observeOutbound` and everything else the per-item success loop does afterward touch only header fields, never the frame bytes. On the error path, and for a `txRequest` item txLoop drops before ever writing it (cancelled/completed elsewhere first), nothing reads the frame at all, so it is returned to the pool immediately. Every return point clears the batch item's `frame`/`framePtr` fields afterward so a future change to this loop that accidentally reads them again fails loudly (nil slice) rather than silently touching recycled memory.

This boundary relies on the underlying transport's `Write` call being fully synchronous with no retained reference after it returns, which holds for `net.Conn`/`*net.TCPConn` and `*tls.Conn` alike: once `Write` (or the `net.Buffers` scatter/gather write) returns, the kernel (or, for TLS, the record layer) has already consumed what it needed from the caller's slice.

**Pooling as `*[]byte`, not `[]byte`.** `sync.Pool.Get`/`Put` take and return `any`; boxing a `[]byte` value (a 3-word slice header) into an interface allocates on every `Put`, which would quietly reintroduce the allocation this pool exists to remove. A `*[]byte` is pointer-shaped and boxes without allocating. `get`/`put` pass that same pointer through the whole request lifetime rather than ever taking `&localVariable` at `Put` time, which would have the identical problem.

**No reuse guarantee, by design.** `sync.Pool`'s own documentation is explicit that a `Get` need not return anything a prior `Put` supplied, and an item can be dropped on any GC cycle. `session/pool_test.go` does not test object-identity reuse for exactly this reason (an earlier version did, and flaked under `go test -race -count=10`). The property this pooling actually depends on for correctness is narrower and is proven separately, end to end, in `session/pool_content_test.go`: many sequential and concurrent `SubmitSM` calls, each carrying a globally unique, peer-verified payload, run clean under `-race`. If the write-completion boundary above were wrong, that test would show a request's bytes overwritten by a different request's, not merely an absence of reuse.

**Oversized buffers are not retained.** A single PDU can be as large as `Config.MaxPDUSize` (1 MiB by default). Permanently keeping a buffer that size in a session's pool after one outlier would trade the allocation this pool removes for a standing per-session memory floor instead, so `put` drops (does not pool) any buffer larger than 64 KiB; `get` falls back to allocating fresh in that case. This threshold is a judgment call, not a number derived from a finding — revisit it if a workload's typical PDU size is routinely close to it.

Measured impact, `BenchmarkInMemorySessionBidirectional/sessions_1` (`internal/perflab`, `-benchmem`, single in-process session pair, this host — exercises the pool on both the request side and the response side of a round trip):

| | Before (no frame pool) | After (per-session pool) |
| --- | --- | --- |
| allocs/op | 12 | 10 |
| B/op | 1009 | ~905 |

ns/op and request_pdu/s were within normal run-to-run noise in both directions and are not attributable to this change. A local `TestPhase17ReferenceAcceptance` run at sandbox scale (not the documented reference machine — see the benchmarking section of this repo's README) did not show a clear `heap_peak_mib` reduction: one comparison tied at 3 MiB, another showed a 1 MiB *increase* that tracked a ~2% higher completed-request count in that particular run rather than a regression. At this scale a ~100 B/op difference is well below both the metric's 1 MiB reporting granularity and its single-sample noise floor; the `-benchmem` numbers above are the reliable evidence for this change, and confirming a reference-machine-scale heap effect is left to that dedicated run.
