# go-smpp — what it is and how it works

## What the project is

A from-scratch Go implementation of **SMPP 3.4 with SMPP 5.0 extensions**, covering both sides
of the protocol:

- **ESME / client** — the application that submits messages to an SMSC.
- **SMSC / server** — the short-message service centre that accepts binds and delivers messages.

Both roles run on one shared session core. There is no separate client stack and server stack;
`client` and `server` are thin lifecycle facades over the same `session.Session`.

Stated scope: 27 SMPP 3.4 command IDs, 44 TLV tags, SMPP 5.0 Cell Broadcast
(`broadcast_sm` / `query_broadcast_sm` / `cancel_broadcast_sm`), GSM 03.38 / UCS-2 / UTF-16BE
message encoding with UDH and SAR multipart segmentation, and an observability surface built on
atomic counters.

**Zero external dependencies.** `go.mod` has no `require` block. `unsafe` is banned and CI
enforces the ban.

Performance target: 100,000 aggregate bidirectional request PDUs/s on Linux/amd64, 8 cores,
10 GB, using the minimum practical number of sessions.

94 Go files, ~15,000 lines.

---

## Package map

| Package | Responsibility |
| --- | --- |
| `protocol` | Pure wire vocabulary. Command IDs, command statuses, TLV tag constants, body structs, sequence-number ranges, error taxonomy, capability negotiation. No I/O, no state. |
| `encoding` | GSM 03.38 default + extension alphabets, septet packing/unpacking with fill bits. |
| `codec` | PDU encode/decode, the stream framer, and the immutable command/TLV registry. Depends only on `protocol`. |
| `transport` | TCP and TLS dial/listen plus `WriteFull` / `WriteBuffers` / `ReadFull`. Knows nothing about SMPP. |
| `message` | Application-level message segmentation and reassembly (UDH 8/16-bit, SAR TLVs). |
| `session` | The engine: goroutines, request window, pending table, deadlines, state machine, liveness. |
| `client` | ESME lifecycle — dial, bind, reconnect with exponential backoff, rebind. |
| `server` | SMSC lifecycle — accept loop, session cap, handler dispatch. |
| `internal/perflab` | Throughput and resource-bound acceptance harness. |
| `cmd/smpp-sim` | Standalone SMSC simulator. |
| `examples/esme`, `examples/smsc` | Minimal runnable examples. |

### Dependency DAG

```
        protocol        encoding        transport
           │  │            │                │
           │  └────────────┼────────┐       │
           ▼               ▼        │       │
         codec          message     │       │
           │               │        │       │
           └──────┬────────┼────────┼───────┘
                  ▼        │        │
               session ◄───┼────────┘
                  │        │
        ┌─────────┴────────┴──────┐
        ▼                         ▼
     client                    server
```

This is machine-enforced by `architecture_test.go`, which parses imports with `go/ast` and
rejects any edge not in an explicit allow-table. That test is the single most valuable piece of
architectural hygiene in the repo — it makes the layering non-negotiable rather than aspirational.

---

## Session model

`session.New` starts **exactly four goroutines**, regardless of traffic volume:

```
wg.Add(4)
go s.rxLoop()          // read → frame → decode → dispatch
go s.txLoop()          // drain tx queue → batch → write
go s.deadlines.run()   // one shared min-heap timer for the whole session
go s.livenessLoop()    // enquire_link / inactivity supervision
```

There is deliberately **no goroutine per message and no goroutine per timer**. This is the
central design decision that makes the throughput target reachable: at 100k PDUs/s a
goroutine-per-message design would be dominated by scheduler and stack churn.

### Request lifecycle (outbound)

```
Request(ctx, cmd, body)
  │
  ├─ window.acquire(ctx, done, wait)        bounded concurrency (token channel, cap 1024)
  ├─ machine.BeginOutbound(cmd)             SMPP operation/state matrix check
  ├─ acquireRequestCompletion()             pooled chan, avoids per-request alloc
  ├─ pending.insert(seq, req)               retry on ErrSequenceInUse
  ├─ codec.EncodePDU(nil, header, body, reg)
  ├─ txQueue <- txItem
  │
  └─ select {
       <-request.done      → response / fatal / loss
       <-ctx.Done()        → cancellation
     }
```

Every one of those five terminal paths converges on `pendingRequest.finish()`, which
cancels the deadline and releases the window **exactly once**. That exactly-once property is
the reliability core of the library and is documented as a contract in `docs/CONCURRENCY.md`.

Note the deadline ordering: the response timer is armed by `txLoop` via
`deadlines.scheduleItemAt`, *after* the bytes are fully handed to the transport — not at
`Request()` entry. A request stuck behind a full TX queue does not burn its response budget
waiting to be written.

### TX batching

`txLoop` blocks for the first item, then **opportunistically** drains whatever else is already
queued — it never waits to fill a batch, so latency is not traded for throughput. Batches are
bounded by `DefaultTXBatchItems` (32) and `DefaultTXBatchBytes` (64 KiB). Dispatch:

- one item → `transport.WriteFull`
- batch on `*net.TCPConn` → `transport.WriteBuffers` (`net.Buffers` scatter/gather, no copy)
- batch on TLS → a coalescing `writeBuffer` (TLS needs one record, so copying is correct here)

For responses, `machine.CompleteInbound` is committed **before** the write becomes visible, so
the peer can never observe a response the state machine has not yet accounted for.

### Framing and the fail-closed rule

`codec.Framer` is a stream framer: it hands out complete PDUs as sub-slices of its read buffer
with no copy, and buffers partial frames. Length validation enforces
`HeaderSize <= command_length <= maxPDUSize` (default 1 MiB).

On a `*protocol.FatalError` the framer is **permanently poisoned**. There is no attempt to
resynchronize the stream. This is a deliberate fail-closed choice: once framing is lost, every
subsequent byte offset is a guess, and guessing on a billing-relevant protocol is worse than
dropping the connection.

Non-fatal (semantic) decode errors are different — the session answers with `generic_nack` and
keeps the connection.

Because PDUs are borrowed sub-slices of the framer buffer, anything that outlives the dispatch
call must copy. `ownDecodedPDU` does exactly that, cloning every byte slice in the decoded body.

### Error taxonomy

`protocol` splits errors into two kinds, and the split drives all downstream behaviour:

- `SemanticError` — the PDU was well-framed but invalid. Recoverable: nack and continue.
- `FatalError` (with a `FatalErrorKind`) — framing itself is untrustworthy. Poison and close.

### Reconnect

`client.lifecycle()` dials, builds a session, binds from a **cloned** bind profile, and on loss
retries with exponential backoff (100 ms initial, 5 s cap, ×2). Requests in flight at the moment
of loss fail with a `LossError`. Nothing is replayed — per `AGENTS.md`, an ambiguous request is
never auto-resubmitted, because a resubmitted `submit_sm` is a duplicate SMS and a duplicate
charge.

---

## Observability

Lock-free atomic counters plus two opt-in callback hooks:

- `Observer` — lifecycle and error events (`EventEnquireLinkSent`, `EventEnquireLinkTimeout`, …).
- `PacketTracer` — per-PDU tracing, **off by default** by rule.
- `WindowObserver` — window occupancy snapshots including a high-water mark.

`docs/CONCURRENCY.md` states per-type concurrency guarantees explicitly, including that
`*codec.Framer` is **not** concurrent-safe (it is owned by `rxLoop` alone), and that callbacks
must return quickly and must not re-enter a blocking session call.

---

## Message layer

`message` handles the part of SMPP that applications actually get wrong:

- GSM7 / UCS2 / UTF16BE / binary segmentation.
- Concatenation via UDH (`BuildConcatUDH8`, `BuildConcatUDH16`, `GSM7UDHFillBits`) *or* SAR TLVs
  (`SARTLVs`, `ParseSAR`).
- `splitUTF16` keeps surrogate pairs intact across a segment boundary — splitting a surrogate
  pair is the classic emoji-corruption bug and it is handled.
- `Reassemble` validates reference, kind, coding, and total consistency before joining.

`message_payload` vs `short_message` exclusivity (`ErrConflictingMessageData`) is enforced in
both the encode and decode directions, which many implementations skip.

---

## Testing and CI

CI runs on ubuntu-latest with Go 1.26.x:

1. `go test ./...`
2. `go test -race ./...`
3. Fuzz smoke — `FuzzFramer` and `FuzzDecodePDU`, 2 s each
4. `protocol` and `codec` benchmarks
5. Phase 17 perf smoke
6. A 2 s resource-bound acceptance run
7. A 60 s 100k-RPS soak, gated on `[soak]` in the commit message
8. An `unsafe`-import ban check
9. `[profile]` / `[batch]` gated profiling runs

The fuzz targets sit on exactly the right surface — the framer and the PDU decoder are where
hostile input lands first.

---

## Overall assessment

The architecture is the strongest part of this codebase. Specifically:

- The layering is correct *and* enforced by a test, not by convention.
- The concurrency model is bounded everywhere: fixed goroutine count, bounded window, bounded
  pending table, bounded TX queue, shared timer heap.
- The exactly-once completion contract is stated, implemented, and tested.
- Fail-closed framing and no-replay reconnect are the right calls for a protocol where a
  duplicate message costs real money.
- Zero dependencies and an `unsafe` ban give a very small supply-chain and audit surface.

The weaknesses are not architectural. They are: a panic on a library release path, missing write
deadlines, a hot-path allocation per outbound PDU, one oversized file, and missing repository
hygiene (LICENSE, .gitignore, lint config, tags). Those are enumerated with fixes in
`CODE_REVIEW.md` and prioritized in `FINDINGS.md`.
