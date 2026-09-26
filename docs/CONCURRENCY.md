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

## Completion channel lifetime

Each `Request` allocates its own `chan requestResult` and lets it become garbage once the request completes. This channel used to be pooled on `Session` (a fixed-size ring recycled across requests), saving one small allocation per request. The pool was removed (Finding B4, Task 1.5): it was correct only as long as nothing could still be mid-send on a channel at the moment it went back in the pool, which was an invariant spread across `request()`, `pendingTable`, `handleResponse`, and the deadline manager and enforced by nothing mechanical — a single-value drain on release would have silently masked a two-sends bug rather than surfacing it. Against the allocation work already planned for Phase 2 (frame-buffer pooling, decoded-response arenas), the saving was marginal, so the correctness risk was not worth carrying.

Measured cost of the removal, `BenchmarkInMemorySessionBidirectional/sessions_1` (`internal/perflab`, `-benchmem`, single in-process session, this host):

| | Pooled (before) | Per-request alloc (after) |
| --- | --- | --- |
| allocs/op | 10 | 12 |
| B/op | 873 | 1009 |

ns/op and request_pdu/s moved within normal run-to-run noise (~3050 vs ~2930 ns/op) and are not attributable to this change. The two extra allocations and ~136 B/op are the one-channel-per-request cost; there is no generation counter or other mitigation in place, since deleting the pool was the chosen fix rather than the atomic-generation-check alternative.
