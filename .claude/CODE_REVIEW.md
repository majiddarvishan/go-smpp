# go-smpp — code review

Commit reviewed: `00c80cd docs: finalize phase 17 reference gate [reference]`
Reviewer environment: go1.26.5 windows/amd64 · 94 Go files · ~15,000 LOC

## Verification actually performed

| Check | Result |
| --- | --- |
| `go build ./...` | clean |
| `go vet ./...` | clean |
| `go test ./...` | **all packages pass** |
| `gofmt -l .` | **12 files non-compliant** |
| `go test -race ./...` | **not run** — no cgo / C toolchain on this host |

The race detector could not run here (`-race requires cgo`; `gcc` not on PATH). CI runs it on
ubuntu-latest. Everything below about concurrency is derived from reading the code, not from a
local race run — findings B1–B4 are reasoned, not detector-confirmed.

Severity scale: **S1** production incident risk · **S2** correctness or resource risk under
load · **S3** latent / defensive · **S4** cosmetic.

---

## 1. Bugs and correctness risks

### B1 — `requestWindow.release()` panics inside a library release path · S1

`session/window.go:77`

```go
func (w *requestWindow) release() {
	select {
	case <-w.tokens:
		w.inUse.Add(^uint32(0))
		w.notify()
	default:
		panic("smpp session: outbound window released without acquisition")
	}
}
```

`release` is reachable from three places — the `windowOwned` defer at `session/session.go:312`,
the `releaseWindow` closure stored on every `pendingRequest` (`session/session.go:328`), and
`pendingRequest.finish()` at `session/pending.go:163`. The double-release guard is
`finish()`'s `finished` flag plus the `windowOwned` handoff. That reasoning is sound today, but
the failure mode if it is ever wrong is **a panic that takes down the host process**, not just
the session.

A library used by a carrier-grade SMS gateway must not be able to panic a customer's process
because of an internal accounting slip. The invariant is worth asserting — but as a metric and
a logged fatal-class event, not `panic`.

**Fix:** make `release()` total. Return a `bool`, increment a `windowReleaseUnderflows`
counter, emit an observer event, and let the session continue.

```go
func (w *requestWindow) release() bool {
	select {
	case <-w.tokens:
		w.inUse.Add(^uint32(0))
		w.notify()
		return true
	default:
		w.underflows.Add(1)
		return false
	}
}
```

Keep the `panic` only under a build tag or a `Config.StrictInvariants` flag used in tests.

### B2 — no write deadline anywhere in library code · S1

Confirmed: `grep -rn "SetWriteDeadline\|SetReadDeadline\|SetDeadline" --include=*.go .`
excluding tests returns **nothing**. `transport.WriteFull` and `transport.WriteBuffers` block
indefinitely.

Consequence: a peer that completes the TCP handshake and then stops reading fills its receive
window, then the local send buffer, and `txLoop` blocks in `conn.Write` forever. From that
moment:

- the TX queue backs up and every `Request` blocks on `s.tx <- item`;
- the response-deadline heap never fires for those requests, because deadlines are armed by
  `txLoop` *after* the write completes — so the usual timeout safety net does not engage;
- only `livenessLoop` can rescue the session, and only if `InactivityTimeout` is enabled, since
  `EnquireLink` itself would also block.

This is the classic slow-loris exposure for a wire protocol server, and `server` accepts
untrusted inbound connections.

**Fix:** add `Config.WriteTimeout` (suggest 30 s default) and have `txLoop` call
`conn.SetWriteDeadline(time.Now().Add(d))` before each write and clear it after. Treat
`os.ErrDeadlineExceeded` as fatal → `terminate()`. Once a write deadline trips on a TCP conn
the stream position is indeterminate, so terminating is the only correct response — which fits
the existing fail-closed philosophy exactly.

### B3 — `livenessLoop` blocks on a synchronous `EnquireLink` · S2

`session/session.go:739`

```go
if s.config.EnquireLinkInterval > 0 && idle >= s.config.EnquireLinkInterval {
	if err := s.EnquireLink(s.ctx); err != nil {
```

`EnquireLink` → `Request` → `window.acquire(ctx, done, wait=true)`. When the window is
saturated (the normal condition at the 100k PDU/s design point), the liveness goroutine parks
waiting for a window token. While parked it cannot evaluate `SessionInitTimeout` or
`InactivityTimeout`. Liveness supervision is disabled precisely when the session is most
loaded — and, combined with B2, a stalled peer disables it indefinitely.

**Fix (either, preferably both):**

1. Issue the keepalive with `wait=false` and skip the tick on `ErrWindowFull`. A session pushing
   enough traffic to saturate the window demonstrably has a live peer; a keepalive is redundant
   there. Only escalate if no response of any kind has been seen for the interval.
2. Reserve one window slot for lifecycle PDUs so keepalive and unbind can never be starved by
   message traffic.

### B4 — completion-channel pooling depends on a fragile drain invariant · S2

`session/session.go:452`

```go
func (s *Session) releaseRequestCompletion(done chan requestResult) {
	// ... drain an already-terminal value before reusing the completion channel
	select {
	case <-done:
	default:
	}
	select {
	case s.completions <- done:
	default:
	}
}
```

The pool is correct only if, at `releaseRequestCompletion` time, no other goroutine can still
send on `done`. That holds because `pending.finish()` has run — but it is an ordering invariant
spread across `request()`, `pendingTable`, `handleResponse`, `failAll`, and the deadline
manager, enforced by nothing mechanical. A single-value drain also silently masks the two-sends
bug it would otherwise surface.

The saving is one 1-element channel allocation per request. Against a `sync.Pool`-able frame
buffer (P1) this is a rounding error, and it is the riskiest allocation optimization in the
codebase.

**Fix:** either delete the pool and allocate the channel per request, or add an atomic
generation counter on `pendingRequest` that the sender checks, so a stale send is detected
rather than silently delivered to the next request reusing the channel. Given the risk
asymmetry, deleting the pool is the better trade.

### B5 — `ensureSCInterfaceVersion` appends to a caller-owned slice · S2

`session/session.go:1308`

```go
response.Optional = append(response.Optional, protocol.OptionalParameter{
	Tag: protocol.TLVTagSCInterfaceVersion, Value: []byte{byte(version)},
})
```

`response` is a struct copy, but `response.Optional` still aliases the caller's backing array.
If the caller supplied a slice with spare capacity — typical when a handler builds responses
from a reused buffer — this `append` writes into memory the caller still owns. A handler that
constructs several `BindResponse` values from one backing array gets silent cross-contamination.

**Fix:** copy before appending.

```go
opts := make([]protocol.OptionalParameter, len(response.Optional), len(response.Optional)+1)
copy(opts, response.Optional)
response.Optional = append(opts, protocol.OptionalParameter{
	Tag: protocol.TLVTagSCInterfaceVersion, Value: []byte{byte(version)},
})
```

### B6 — every non-fatal decode error is reported as `ESME_RINVMSGLEN` · S3

`session/session.go:964`

```go
// Framing is still trustworthy. Reject the semantic/body error without
// poisoning or resynchronizing the TCP stream.
_ = s.queueGenericNACK(header, protocol.StatusInvalidMessageLength)
```

Any semantic decode failure is answered with `StatusInvalidMessageLength` (0x01). A bad TLV
stream, a conflicting `message_payload`/`short_message` pair, or an unparseable address all
report "invalid message length". `handleRequest:1023` does the same for an out-of-range inbound
sequence number, which is not a length problem either.

`protocol` already defines the right codes — `StatusInvalidOptionalParameterStream` (0xC0),
`StatusOptionalParameterNotAllowed` (0xC1), `StatusInvalidCommandLength` (0x02). Peers use
`command_status` for alarm routing; a wrong code sends operators down the wrong path.

**Fix:** have `SemanticError` carry a `Status protocol.CommandStatus`, set it at each decode
site, and map it through in `processFrame`. Fall back to 0x01 only when unset.

**Implemented (Task 3.1).** `protocol.SemanticError` already had the `Status` field (unused
before this); every decode-site error that could actually reach the semantic path now
constructs one with a status instead of a plain wrapped sentinel, and `processFrame` extracts
it via `errors.As`, falling back to 0x01 only when a decode error wasn't classified. One
correction to this finding's own examples: `StatusInvalidOptionalParameterStream` (0xC0) is not
actually reachable here — every TLV-length violation (`ScanTLVs`, truncated header or an
over-length value) is already a `*protocol.FatalError`, not semantic, because a mis-parsed TLV
boundary means the rest of the body can't be trusted either; that classification predates this
task and wasn't reopened. `StatusOptionalParameterNotAllowed` (0xC1) has no decode-time check
behind it at all (no code currently rejects a TLV as "present but not allowed for this
command") — adding one would be new validation, not a status-mapping fix, so it wasn't added.
The out-of-range-sequence-number case this finding also names got `StatusSystemError`, the
closest available "not a length problem" signal — SMPP has no status specific to a malformed
sequence_number either. Where a decode error's condition and the 0x01 fallback's numeric value
coincide (sm_length 255; a request's nonzero command_status; both have no better SMPP status to
map to, so 0x01 stays, now explicit rather than accidental) that overlap is real, not a bug —
`codec/semantic_status_test.go`'s table asserts each one is a typed `*protocol.SemanticError`
regardless, and a mutation test on the session-level integration test confirmed the numeric
coincidence made that specific test unable to tell "mapped" from "fell back" — fixed by picking
a differently-valued case for that test, not by discarding the mapping.

### B7 — `Framer` permanently retains up to `maxPDUSize` per session · S3

`codec/framer.go`. When a fragmented PDU arrives, `Feed` grows `f.pending` to the declared
length:

```go
if cap(f.pending) < int(length) {
	f.pending = make([]byte, 0, int(length))
}
```

After emit it does `f.pending = f.pending[:0]` — length reset, **capacity retained**. One 1 MiB
PDU that happens to arrive fragmented permanently pins 1 MiB per session. At the 10 GB /
many-sessions design point, a peer can drive retained memory to `sessions × maxPDUSize` by
sending one large fragmented PDU per connection, then going quiet. The acceptance harness
already tracks `maxRetainedMB`, so this is a metric the project cares about.

**Fix:** after emitting, if `cap(f.pending)` exceeds a high-water threshold (e.g. 64 KiB, the
default read-buffer size), drop the buffer so the next fragment reallocates from small.

```go
f.pending = f.pending[:0]
if cap(f.pending) > shrinkThreshold {
	f.pending = nil
}
```

**Confirmed after implementation (Task 3.4):** the claim holds exactly as written. Before the
fix, feeding one 1 MiB PDU in 2-byte chunks (and 3-byte, 4 KiB, 64 KiB chunks — both entry paths
into `pending`) leaves `cap(f.pending) == 1048576` after the frame has been emitted
(`TestFramerReleasesLargeBufferAfterFragmentedPDU` failed on that number before the change).
Fix as proposed, with `framerShrinkThreshold = 64 << 10` in `codec/framer.go` (the same figure as
`session.DefaultReadBufferSize` and `maxPooledFrameCapacity`; `codec` cannot import `session`, so
it is a documented constant, not a reference).

Trade-off measured, not assumed (`BenchmarkFramerFragmentedPDU`, interleaved before/after
rounds, 64 KiB reads): PDUs that are *fragmented and larger than 64 KiB* now allocate once per
PDU instead of zero times — 100 KiB: ~3.1 µs/0 allocs → ~13-16 µs/1 alloc (106 496 B);
1 MiB: ~47-59 µs/0 allocs → ~150-163 µs/1 alloc (1 MiB). Fragmented PDUs at or below the
threshold are unaffected (0 allocs, buffer reused). Complete PDUs inside one read never touch
`pending` and are unaffected. SMPP PDUs above 64 KiB are exotic (typical ones are well under
1 KiB), so this is a deliberate bet that idle-session memory matters more than the throughput of a
sustained stream of >64 KiB fragmented PDUs.

Not addressed by this fix, and worth knowing: the buffer is still sized from the *declared*
`command_length` as soon as the 4-byte length word arrives, so a peer can make the framer
allocate up to `maxPDUSize` for a PDU it never finishes. That memory is held only while the PDU is
in flight and is bounded by the session's liveness/inactivity deadlines, so SEC1's "closed by
B2 + B7" stands for the *idle-retention* part; growing `pending` incrementally instead of up front
would close the in-flight part, but was not part of this finding.

Separate observation (not fixed, outside B7): if the `emit` callback returns a *non-fatal*
error, `Feed` returns it without resetting `pending`/`expected`, so the same frame stays buffered
and is emitted again on the next `Feed`. Unreachable through `Session` today (`rxLoop` terminates
the session on any `Feed` error), but it makes the `Framer` API misleading for other callers.

### B8 — `splitBytes` returns one empty segment for empty input · S3

`message/message.go:345`

```go
func splitBytes(data []byte, max int) [][]byte {
	if len(data) == 0 {
		return [][]byte{{}}
	}
```

An empty message yields one zero-length segment rather than zero segments, so callers emit a
`submit_sm` with an empty `short_message`. Whether that is intended is undocumented, and it
differs from the natural reading of "split into parts". If intentional, say so in the doc
comment; if not, return `nil`.

**Checked and decided (Task 3.5):** the finding's premise — "callers emit a `submit_sm` with an
empty `short_message`" *because of* `splitBytes` — does not hold. No caller reaches
`splitBytes` with empty input in a way that matters: `SegmentTextUDH`/`SegmentBinaryUDH` only
split when the data is already larger than one part, and `SegmentTextSAR`/`SegmentBinarySAR`
treat `len(chunks) <= 1` as "single part" and delegate to the UDH functions, which build the
one-part segment themselves. Probed on the pre-change code: `SegmentTextUDH("")`,
`SegmentTextSAR("")`, `SegmentBinaryUDH(nil|[]byte{})`, `SegmentBinarySAR(nil|[]byte{})` all return
exactly one segment (Total 1, Sequence 1, Units 0, empty Data/UserData/UDH, no TLVs), and that
segment comes from the "fits in one part" branches, not from the split helpers. The new public-API
test passed on the old code and passes unchanged on the new, i.e. the change is behaviour-preserving
at the public surface.

Decision: `splitBytes` now returns `nil` for empty input (the finding's first option), and
`splitUTF16` — which had the same shape of answer for empty input, `[][]byte{nil}`, and which the
finding did not mention — does the same so the helpers agree. A caller that forgets its own empty
guard now fails in `makeUDHSegments` (`ErrInvalidSegments`, zero parts) instead of emitting an empty
multipart segment. The public behaviour for an empty message (one empty single-part segment, not an
error) is unchanged and is now documented on all four `Segment*` functions and pinned by a test.

Open for the maintainer, deliberately not changed: whether an empty message should instead be an
*error* at the public API. A zero-length `short_message` (`sm_length` 0) is valid SMPP, nothing in
this repo submits one, and changing it would alter exported behaviour, so it was left as a decision
rather than made here.

### B9 — `Reassemble` uses a non-stable sort and rewrites `total` · S3

`message/message.go:207` and `:209`

```go
sort.Slice(parts, func(i, j int) bool { return parts[i].Sequence < parts[j].Sequence })

if total == 0 {
	total = uint8(len(parts))
}
```

Two issues:

- `sort.Slice` is not stable, so duplicate sequence numbers reassemble in an arbitrary order.
  Duplicate segments are a real occurrence on retried multipart SMS. Reject duplicates
  explicitly rather than silently picking one.
- `total == 0 → total = len(parts)` means a caller who passes an incomplete set with an unknown
  total gets a "successful" reassembly of a truncated message. Silent truncation is worse than
  an error here.

Also: `sort.Slice` uses reflection; `slices.SortFunc` is faster and this module is already on
Go 1.26.

**Fix:** use `slices.SortFunc`, return an error on duplicate `Sequence`, and require an explicit
non-zero `total`.

**Correction after implementation (Task 3.3):** neither claimed failure mode is actually
reachable in this code, and both were checked, not just reasoned about: `sort.Slice`'s
instability can only reorder *equal-Sequence* elements relative to each other, and since the
loop right after sorting requires `part.Sequence == i+1` at every position, any duplicate
necessarily leaves some other value in `1..total` missing — which always produces a
`part.Sequence != i+1` mismatch *somewhere* in the scan, regardless of which equal-Sequence
element the unstable sort placed first. Mutation-removing the new explicit duplicate check and
re-running `TestReassembleRejectsDuplicateSequence` confirmed this directly: it still errored
(`inconsistent part 3`), just with a less specific message. The `total == 0` fallback has the
same property for a different reason — the segment that triggered the fallback (`parts[0]`,
`Total == 0`) is the very same segment the next line checks against the now-nonzero recomputed
`total`, so it always fails its own check immediately; mutation-restoring the old fallback and
re-running `TestReassembleRejectsZeroTotal` confirmed the same way (`inconsistent part 1`).
Landed the fix anyway — a dedicated error naming the actual problem ("duplicate sequence N" /
"total must be specified") is real diagnosability value even where the generic check already
happened to catch it, and no longer leaving that correctness property to an unrelated check's
side effect is worth doing on its own. `slices.SortFunc`'s own claim held up: measured on
`BenchmarkReassemble` (3-part reassembly, interleaved rounds so machine drift cancels),
`sort.Slice` → `slices.SortFunc` alone is ~525ns → ~410ns and 7 → 4 allocs/op. Adding the
explicit duplicate-check loop back (the full change, as shipped) keeps the 4-alloc figure — the
small map apparently doesn't escape — but its own CPU cost brings latency back to roughly the
original ~520-550ns, a wash rather than a net win on that axis; allocations are still the real,
measured improvement.

### B10 — vendor commands bypass the operation matrix · S3

`session/session.go:1089`

```go
// Vendor-specific request commands do not have a standard operation-matrix
// entry. Keep their default policy conservative: they may run only after a
// session is bound, with application semantics delegated to Handler.
if !command.IsResponse() && (state == protocol.StateBoundTX ||
	state == protocol.StateBoundRX || state == protocol.StateBoundTRX) {
	return nil
}
```

`beginInbound` routes anything not in `isStandardSessionCommand` past
`StateMachine.BeginInbound`, accepting it in any bound state. The comment calls this
conservative, but it is strictly more permissive than `CanIssue`, whose `default` branch is
`return false` (`session/state.go:113`). A vendor command is accepted on a `bound_rx` session
where the equivalent standard command would be refused, and the direction (`RoleESME` vs
`RoleSMSC`) is never checked.

**Fix:** let `RegistryBuilder` record a direction and permitted-state mask per registered
command, including vendor ones, and have `beginInbound` consult it uniformly. That also removes
the `isStandardSessionCommand` list, which must currently be kept in sync by hand.

**Implemented (Task 3.2).** `codec.CommandDefinition` gained `Actor CommandActor` and
`AllowedStates []protocol.SessionState` (a new `CommandActor` enum — `ActorAny`/`ActorESME`/
`ActorSMSC` — lives in `codec`, not `session.Role`, since `session` already imports `codec` and
the reverse would be a cycle; `architecture_test.go` still passes). `isStandardSessionCommand`
is gone; `session/state.go`'s `CanIssue` switch became a `map[protocol.CommandID]operationRule`
(`standardRules`), and `IsStandardCommand` is just map membership — derived from the same data
`CanIssue` itself consults, so the two can't drift apart again. The switch→map translation was
checked mechanically, not just read over: `TestCanIssueMatchesOldSwitchExhaustively` copies the
old switch verbatim as an independent oracle and compares it against the new map for every
command this package names, across all 7 `SessionState` values and both roles (462 combinations,
zero mismatches). `beginInbound` now checks a vendor request's own declared `Actor`/
`AllowedStates` when present, falling back to the old permissive "any bound state, either role"
rule when a vendor command declares neither — opt-in per command, not a breaking change for
existing vendor registrations. Setting `AllowedStates` is required to get the stricter check;
`Actor` alone (states left nil) still falls back to permissive, since the fallback is keyed off
`len(AllowedStates) == 0`, documented on `CommandDefinition` and in `docs/VENDOR_EXTENSIONS.md`.

---

## 2. Performance

The design is sound — bounded goroutines, opportunistic batching, `net.Buffers` scatter/gather,
a shared timer heap, caller-owned deadline storage. The remaining wins are allocation-side.

### P1 — one full frame allocation per outbound PDU; no `sync.Pool` in the repo · S1 (perf)

Confirmed: `grep -rn "sync.Pool" --include=*.go .` returns **nothing**.

Three hot sites pass `nil` as the destination and therefore allocate a fresh frame every time:

- `session/session.go:359` — `request()`
- `session/session.go:421` — `SendOneWay()`
- `session/session.go:1143` — the response path

```go
frame, err := codec.EncodePDU(nil, header, body, s.registry)
```

At the stated 100,000 PDUs/s target this is 100,000 heap allocations per second of typically
100–300 bytes, all short-lived but all escaping to the heap because the frame is handed to
`txLoop` through a channel. This is the single largest remaining GC pressure source, and the
`codec` API was clearly built to avoid it — `EncodePDU` takes a `dst []byte` and
`reservePDUCapacity` computes exact size hints.

**Fix:** add a `sync.Pool` of frame buffers owned by the session. `request()` acquires a buffer,
encodes into it, and `txLoop` returns it to the pool after the write completes — the write is
the natural lifetime boundary and `txLoop` already knows when a batch has been fully handed to
the transport. Size the pooled buffers from `encodedBodySizeHint`.

Expected effect: near-elimination of steady-state allocation on the send path. Measure with
`-benchmem` before and after; `AGENTS.md` requires profile-driven change, so land it behind the
`[profile]` CI gate.

### P2 — throwaway 16-byte allocation per encode · S2 (perf)

`codec/pdu.go:76` and `codec/pdu.go:87`

```go
start := len(dst)
dst = append(dst, make([]byte, HeaderSize)...)
```

Both sites materialize a 16-byte slice purely to reserve header space. `make([]byte, 16)` inside
`append` is not reliably optimized away when the result escapes.

**Fix:** extend in place without the temporary, using the `slices.Grow` idiom already present in
`reservePDUCapacity`:

```go
start := len(dst)
dst = slices.Grow(dst, HeaderSize)[:len(dst)+HeaderSize]
```

Removes two allocations per encoded PDU.

**Correction after implementation (Task 2.1):** this claim does not hold on this codebase.
`go build -gcflags=-m` on the pre-fix code reports `make([]byte, 16) does not escape` at both
sites — go1.22's escape analysis already proves the temporary is fully consumed by the
immediately-following `append` and elides the heap allocation. An allocation profile
(`-memprofilerate=1`, `pprof -alloc_objects`) of `BenchmarkEncodeSubmitSM` confirms it: 100% of the
2 allocs/op attribute to `slices.Grow` in `reservePDUCapacity` (the unavoidable first allocation of
the backing array, since `dst` starts `nil`) and to the `body any` interface-boxing at the
benchmark's own call site — none to `pdu.go:76` or `:87`. `B/op` and `allocs/op` are unchanged
before/after (288 B, 2 allocs). The fix landed anyway (`slices.Grow` is the same idiom already used
in `reservePDUCapacity`, and removing a `make`+append-spread pattern is a reasonable consistency
improvement on its own), but the allocation-count justification above was wrong for this compiler,
and no throughput/memory win should be attributed to it. The real second allocation — boxing a
value body into `any` at each `EncodePDU`/`Request` call site — is a different, likely harder-to-fix
issue (would need an `EncodePDU` signature change to avoid `any`) and is not addressed by this task;
noted in `TASKS.md` as a candidate for whoever picks up the remainder of Phase 2.

### P3 — registry uses map lookups on the hot path · S3 (perf)

`Registry.ResolveCommand` hits a `map[protocol.CommandID]*CommandDef` on **every** encode and
decode. SMPP command IDs are dense in two narrow bands: `0x00000001–0x00000103` and
`0x80000001–0x80000103`. A flat array indexed by `id & 0x7fffffff` with a bit for the response
flag replaces a hash with a bounds-checked load.

**Fix:** keep the map as the authoritative builder structure, and have `Registry.freeze()`
additionally materialize a `[260]*CommandDef` fast-path table for in-range IDs, falling back to
the map for vendor IDs outside the band. TLV resolution can keep the map — it is off the
per-PDU critical path for most commands.

Quantify with the existing `codec` benchmarks first; if it is under a few percent it is not
worth the extra structure.

**Measured (Task 2.5):** it is under a few percent. `BenchmarkRegistryCommandLookup` (the isolated
map lookup `ResolveCommand` wraps) runs a stable ~3.6 ns/op; `BenchmarkEncodeSubmitSM` and
`BenchmarkDecodeSubmitSM` run a stable ~230 ns/op and ~210 ns/op respectively, each calling
`ResolveCommand` exactly once. That is roughly 1.6% and 1.7% of total time — below this finding's
own threshold. Closed as won't-fix rather than built; a flat fast-path table would add its own
correctness surface (keeping it in sync with the map, handling out-of-range vendor IDs) for a
saving this small.

### P4 — `ownDecodedPDU` clones every response body · S3 (perf)

`ownDecodedPDU` (a ~50-line type switch in `session/session.go`) deep-copies every byte slice of
a decoded PDU, because the framer hands out borrowed sub-slices. This is **correct and
necessary** — but it means every response allocates once per byte-slice field.

**Fix:** clone into a single per-request arena rather than field by field. Compute the total
byte length, allocate one backing slice, and re-slice each field out of it. That turns N small
allocations into one, and the arena can be pooled alongside P1.

### P5 — `livenessResolution()` can tick every 10 ms per session · S3 (perf)

`session/session.go:756`

```go
if resolution < 10*time.Millisecond {
	return 10 * time.Millisecond
}
```

Resolution is `min(SessionInitTimeout, EnquireLinkInterval, InactivityTimeout) / 4`, floored at
10 ms. Any user who configures a 40 ms timer gets a 100 Hz ticker **per session**. At the
"minimum practical session count" design point this is negligible, but a deployment with
thousands of sessions and short timers pays real scheduler cost for a loop that usually does
nothing.

**Fix:** move liveness deadlines into the existing shared `deadlineManager` heap. The
infrastructure already exists and is already per-session; this removes the fourth goroutine and
the ticker entirely. That is the cleanest structural performance change available.

### P6 — size hints cover only submit/deliver · S4 (perf)

`codec/size_hint.go` implements `encodedBodySizeHint` for submit/deliver requests and responses
only. Every other command falls through to `append` growth. `bind`, `data_sm`, and the broadcast
family are common enough to warrant hints — and once P1 lands, accurate hints determine pool
bucket sizing.

---

## 3. Architecture

This is the strongest dimension of the project. Most findings here are affirmations.

**A1 — layering is enforced, not merely documented.** `architecture_test.go` parses imports with
`go/ast` and rejects any edge outside an explicit allow-table. Very few Go projects do this. It
is the reason `codec` and `protocol` have stayed genuinely reusable.

**A2 — the concurrency model is bounded in every dimension.** Fixed four goroutines per session,
bounded window, bounded pending table, bounded TX queue, single shared timer heap. There is no
place where peer behaviour can drive unbounded resource growth — except B7's retained framer
buffer, which is bounded but higher than it needs to be.

**A3 — the exactly-once completion contract is real.** Five terminal paths (response, timeout,
cancellation, fatal error, session loss) all converge on `pendingRequest.finish()` under a
`finished` flag. It is stated in `docs/CONCURRENCY.md` and tested. This is the hardest thing to
get right in an async protocol client and it is right here.

**A4 — fail-closed framing and no-replay reconnect are the correct product decisions.** For a
protocol where a duplicated `submit_sm` is a duplicate SMS and a duplicate charge, refusing to
resynchronize a corrupted stream and refusing to auto-resubmit ambiguous requests are the only
defensible choices. Both are enforced, not just documented.

**A5 — `session/session.go` at 1401 lines is the one real structural problem.** It holds at
least six separable concerns: the request/response API surface, the TX loop and batching, the RX
loop and dispatch, liveness supervision, capability negotiation, and PDU ownership/cloning
helpers.

**Fix:** split along existing seams, keeping the type and its methods in one package:

| New file | Moves |
| --- | --- |
| `session/tx.go` | `txLoop`, batching, `writeBuffer`, dispatch accounting |
| `session/rx.go` | `rxLoop`, `processFrame`, `handleRequest`, `handleResponse` |
| `session/liveness.go` | `livenessLoop`, `livenessResolution`, activity tracking |
| `session/negotiate.go` | `ensureSCInterfaceVersion`, `bindNegotiationProfile`, `requiresSMPP50` |
| `session/own.go` | `ownDecodedPDU`, `responseOptionalParameters`, `cloneBytes`, `cloneOptional` |
| `session/session.go` | type, config, `New`, public request API, `terminate` |

Pure file movement — no behaviour change, no API change, and each concern becomes reviewable in
isolation.

**A6 — `protocol.SubmitSM` and `protocol.DeliverSM` are byte-identical structs.** This is
correct per SMPP 3.4 (the PDU bodies are the same), and keeping them distinct gives type safety
at call sites. Worth an explicit comment on both types saying the duplication is deliberate, so
a future contributor does not merge them.

---

## 4. Clean code

### C1 — 12 files are not gofmt-clean · S2

`gofmt -l .` reports:

```
api_contract_test.go
client/client_test.go
codec/fuzz_test.go
codec/pdu.go
codec/smpp34_test.go
protocol/data_coding.go
server/server_test.go
session/pending.go
session/pending_test.go
session/phase18_reliability_test.go
session/session.go
transport/transport_test.go
```

Two of these (`codec/pdu.go`, `session/session.go`) are the hottest files in the codebase.
`session/pending.go` has misaligned `deadlines` / `finished` struct fields.

**Fix:** run `gofmt -w .` and add a CI step that fails on non-empty `gofmt -l .` output. The
formatting is a one-command fix; the important part is the gate, so it cannot regress. CI
currently checks the `unsafe` ban but not formatting.

### C2 — no `.golangci.yml` · S3

No linter configuration and no lint step in CI. For a library of this ambition, `errcheck`,
`ineffassign`, `unused`, `govet`, `staticcheck`, and `gocritic` would have caught C3's dead code
and several formatting issues automatically.

**Fix:** add `.golangci.yml` and a CI job. Start with the default set plus `staticcheck`; add
`gocognit` / `funlen` later, after A5's split, or they will only generate noise.

### C3 — dead code · S4

- `Session.noteActivity()` at `session/session.go:698` — defined, never called. Both call sites
  (`:894`, `:968`) use `noteActivityAt`. Delete it.
- `deadlineManager.schedule()` at `session/deadline.go:56` — used **only** by tests
  (`deadline_test.go`, `timers_test.go`). Production code uses `scheduleItemAt`. Either move it
  into a `_test.go` helper or document it as a test-only convenience wrapper.

### C4 — duplicated parallel type switches · S3

`ownDecodedPDU` (~50 lines) and `responseOptionalParameters` (~25 lines) are two type switches
over the same set of body types. Adding a new PDU body requires editing both, and forgetting one
produces either a retained-borrowed-slice bug or a dropped TLV — both silent.

**Fix:** define small interfaces implemented by body types:

```go
type ownable interface{ own() any }
type optionalCarrier interface{ optional() []protocol.OptionalParameter }
```

Both functions then collapse to a single type assertion with a default. A new body type that
forgets to implement `ownable` fails loudly in one place instead of silently in two.

**Checked and decided (Task 4.2):** the proposed fix cannot be done as written. `ownable` /
`optionalCarrier` would have to be implemented *by the body types*, and those live in package
`protocol`: an interface declared in `session` with unexported methods can only be satisfied by types
in `session`, and exported methods would add public API to `protocol` (and make it aware of session's
response arena). That is not behaviour-preserving, so it was not done.

What the finding was actually after is that forgetting one of the two switches for a new body type
fails *loudly* instead of silently. That is now a single test, `TestEveryResponseBodyTypeIs…` in
`session/body_coverage_test.go`. It keeps no list of body types of its own (a third place to forget):
it asks the SMPP 5.0 codec registry, decoding every registered response command from synthetic bodies
with and without a trailing TLV, and checks each distinct Go type that comes back (11 today, including
`OptionalResponse`, which only appears when a TLV is present, and `codec.RawBody`). Per type, by
reflection over every field: `ownDecodedPDU` must leave no borrowed byte (or struct-slice element)
shared with the source, and `responseOptionalParameters` must return exactly the body's `Optional`
field, or nil if it has none. The two switches themselves are unchanged.

### C5 — ~15 near-identical typed operation wrappers · S4

`SubmitSM`, `DeliverSM`, `QuerySM`, `CancelSM`, `ReplaceSM`, `BroadcastSM`, … all follow the same
shape: call `Request`, assert the response body type, return it. Go generics fit exactly:

```go
func request[Req, Resp any](ctx context.Context, s *Session, id protocol.CommandID, req Req) (Resp, error)
```

This is a judgement call — the explicit wrappers are more greppable and give better godoc. Given
the repo's stdlib-only, explicit style, **leaving them as-is is defensible**. Flagged only so the
choice is deliberate.

### C6 — missing repository hygiene · S1 for LICENSE, S3 for the rest

Confirmed absent: `LICENSE`, `.gitignore`, `.golangci.yml`. `git tag` is empty.

**No LICENSE is the single highest-impact omission in the whole repository.** Without an
explicit license, default copyright applies and nobody can legally use this library. Every other
finding here is about quality; this one is about whether the project is usable at all. For a Go
library, MIT or Apache-2.0 are the conventional choices — Apache-2.0 if a patent grant matters.

`.gitignore` should cover at minimum `*.test`, `*.out`, `*.prof`, `/dist/`.

---

## 5. Testing and CI

**Strong.** `go test ./...` passes across all 10 packages. Fuzz targets (`FuzzFramer`,
`FuzzDecodePDU`) sit on exactly the right surface — the two places hostile bytes land first. CI
runs plain tests, race tests, fuzz smoke, benchmarks, a resource-bound acceptance run, a
commit-message-gated 60 s soak, and an `unsafe` ban check.

Gaps:

- **T1 · S2** — no coverage gate and no reported coverage number. Add `go test -coverprofile`
  and a floor, so the ratio does not silently drift as the codebase grows.
- **T2 · S2** — `gofmt` is not checked in CI (C1), and neither is `golangci-lint` (C2).
- **T3 · S3** — no test asserts the B2 stalled-peer behaviour, because the behaviour does not
  exist yet. When `WriteTimeout` lands, add a test with a peer that accepts the connection and
  never reads.
- **T4 · S3** — the fuzz corpus does not appear to be committed under `testdata/fuzz`. Without a
  committed corpus each CI run starts cold and the 2 s smoke explores very little. Commit the
  crashers and interesting inputs found so far.

---

## 6. Security and supply chain

**Positive, and worth advertising.** Zero external dependencies and a CI-enforced `unsafe` ban
give this project an unusually small attack surface. There is no transitive dependency to audit
and no third-party code inside the trust boundary. This belongs prominently in the README; it is
a genuine differentiator for telecom deployments.

**SEC1 · S1 — unauthenticated resource exposure (B2 restated from the security side).** `server`
accepts connections from untrusted peers. With no write deadline and no read deadline, a peer
that binds and then stalls holds a session, a TX goroutine, and its framer buffer indefinitely.
`MaxSessions` bounds the count, which means a modest number of stalled connections can exhaust
the session cap and deny service to legitimate ESMEs. Fixing B2 plus B7 closes this.

**SEC2 · S2 — no bind-attempt rate limiting.** `server.dispatchHandler` routes bind PDUs straight
to the `Authenticator`. There is no per-IP throttle, no backoff on repeated authentication
failure, and no lockout. SMPP `system_id` / `password` pairs are short and often weak; an
attacker gets unlimited online guesses at line rate.

**Fix:** add an optional `Config.BindRateLimiter` hook and, at minimum, a fixed delay on
authentication failure plus a counter the operator can alarm on. Keep the policy pluggable
rather than baked in; deployments differ.

**Implemented, and one claim here corrected (Task 5.1):** `Config.BindRateLimiter` (an interface),
`NewBindThrottle` (a default per-IP policy), `Config.BindFailureDelay`, and `Server.BindStats()`
(attempts / failures / throttled counters to alarm on) are in `server/`. The limiter is handed a
`BindAttempt` (remote address, system_id, mode) and never the password. See `TASKS.md` 5.1 for the
policy choices.

**SEC1's "fixing B2 plus B7 closes this" is only half true, and 5.1 does not close the rest.**
B2/B7 bound *bound* sessions that stall. A peer that connects and never binds is closed by
`SessionInitTimeout` (30 s by default), but it can reconnect as fast as it is closed. Measured with a
probe (not committed): `MaxSessions: 3`, `SessionInitTimeout: 300 ms`, an attacker keeping the server
topped up with silent connections — a legitimate peer's connection was refused at once on 268 of 268
attempts over 1.5 s. Bind rate limiting cannot help, because these connections never send a bind. What
would is a limit on concurrent *unbound* connections per remote address (or an admission hook at accept
time). That is new public API and a policy decision (NAT, trusted peers), so it was not added here; see
the open item in `TASKS.md` under Phase 5.

**SEC3 · S3 — credential exposure through `PacketTracer`.** `PacketTracer` receives the raw
frame, which on a bind PDU contains the password. The tracer is off by default, which is the
right default, but its doc comment should carry an explicit warning that enabling it exposes
bind credentials, so operators do not enable it casually in production. Separately, confirm
`BindRequest.Password` can never reach a log line.

**SEC4 · S3 — TLS configuration is entirely caller-supplied.** `transport.DialTLS` and
`ListenTLS` pass `*tls.Config` through untouched. This is the right design — the library should
not override deployment crypto policy — but a nil or default config silently produces a weaker
posture than an operator may expect. Document a recommended baseline
(`MinVersion: tls.VersionTLS12`, verified peer certificates) in the transport package doc.

**Done (Task 5.3), with one addition:** the baseline is in the transport package doc. The sharper
foot-gun is not a weak default `tls.Config` but `TLSConfig == nil`, which means *plain TCP* in
`server.Config` and `client.Config`; that is now the first trap the doc lists. `DialTLS` also does not
infer `ServerName` (unlike `crypto/tls.Dial`); documented, not changed. Every statement in the doc is
pinned by `transport/tls_baseline_test.go`.

---

## 7. Release readiness

README claims phases 0–18 complete with Phase 19 in progress. What actually blocks a `v1.0.0`:

| Blocker | Status |
| --- | --- |
| LICENSE file | **missing — hard blocker** (C6) |
| Panic-free library paths | **B1 open** |
| Write deadlines / slow-peer defence | **B2 open** |
| `gofmt` clean + CI gate | **C1 open** |
| Phase 17 reference-machine throughput evidence | per README, outstanding |
| First git tag | none exist |
| Public API compatibility statement | not present |

`api_contract_test.go` exists, which is the right mechanism for the last item — but the repo
does not state a compatibility promise. Before tagging v1, add a `COMPATIBILITY.md` (or a README
section) saying what SemVer covers: exported identifiers in `protocol`, `codec`, `message`,
`session`, `client`, `server`, with `internal/` explicitly excluded.

One further consideration: `go.mod` declares `go 1.26.0` and CI pins Go 1.26.x. Requiring the
newest toolchain narrows adoption for a library aimed at telecom operators, who tend to run
conservative toolchains. Unless a specific 1.26 feature is load-bearing, consider lowering the
declared minimum to widen the addressable audience.

---

## 8. Summary

This is a well-architected codebase. The layering is enforced by a test rather than by
convention, the concurrency model is bounded in every dimension, the exactly-once completion
contract is genuinely implemented, and the fail-closed / no-replay decisions show real
understanding of what SMPP correctness costs when you get it wrong. Zero dependencies and an
`unsafe` ban are a meaningful advantage for the target deployment environment.

The work remaining is narrow and mostly mechanical:

1. **Add a LICENSE.** Nothing else matters until this exists.
2. **Remove the panic** from `requestWindow.release()` (B1).
3. **Add write deadlines** (B2) — the real availability bug, and also SEC1.
4. **Pool the outbound frame** (P1) — the largest remaining throughput win, and the one most
   likely to decide whether the 100k PDU/s target is met on the reference machine.
5. **`gofmt -w .` plus a CI gate** (C1).

Items 1–3 and 5 are small, bounded changes. Item 4 is the one requiring profiling discipline,
which `AGENTS.md` already mandates and CI already supports through the `[profile]` gate.

See `FINDINGS.md` for the prioritized, sequenced version of this list.
