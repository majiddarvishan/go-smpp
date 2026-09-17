# Target Architecture

This document describes the intended architecture before implementation. Package names may be refined, but dependency direction and performance boundaries should remain stable unless a decision is updated.

## Dependency direction

```text
Application
    |
    v
High-level client/server API
    |
    v
Session engine
    |-- state machine
    |-- sequence manager
    |-- pending request table
    |-- window/backpressure
    |-- timer/deadline subsystem
    |-- flow-control hooks
    |
    v
Codec / framing
    |-- header
    |-- mandatory fields
    |-- TLVs
    |-- registry
    |
    v
Transport
    |-- TCP
    |-- TLS
    |-- caller-supplied net.Conn-compatible transport
```

Lower layers must not import higher layers.

## Candidate repository/package layout

```text
/
  smpp.go or top-level API package
  client/
  server/
  protocol/
    command/
    status/
    pdu/
    tlv/
    registry/
  codec/
    framing/
    encoder/
    decoder/
  session/
    state/
    sequence/
    pending/
    window/
    timers/
    flow/
  transport/
    tcp/
    tls/
  encoding/
    gsm7/
    ucs2/
  message/
    segmentation/
    reassembly/
    receipt/
  internal/
    pool/
    deadline/
    benchmark helpers/
```

Do not create all packages mechanically before code needs them. Use this as a boundary guide, not as a requirement to maximize package count.

## Protocol core

The protocol core contains wire-level concepts only:

- command identifiers and statuses
- session-independent field types
- PDU structures/views
- TLV structures/views
- PDU/TLV registries
- SMPP capability/version metadata

It does not dial sockets, own sessions, reconnect or run handlers.

## Codec

### Framing

SMPP is a TCP byte-stream protocol. The framer reads the first 4 bytes to determine `command_length`, validates it, and emits exactly one complete PDU frame while retaining any following bytes for the next frame.

The implementation must handle:

- one PDU split across many reads
- several PDUs in one read
- partial 4-byte length prefix
- malformed lengths below header size
- lengths above configured limit
- connection close in the middle of a frame

### Fail-closed structural error policy

Framing correctness is a connection-level invariant. When the decoder discovers an unrecoverable structural violation that makes continued byte-stream alignment or trustworthy decoding unsafe, it reports a **fatal protocol/frame error** to the owning session. The owning session then closes that transport and fails the session exactly once.

Examples include:

- `command_length < 16`
- `command_length` above the configured maximum
- body layout that cannot fit inside the declared PDU frame
- mandatory variable-length/C-Octet fields that cannot terminate within the declared frame
- TLV header/value lengths that run past the declared PDU boundary
- another structural decode condition where continuing would require guessing where the next PDU begins

The implementation must not scan for a plausible next header or otherwise attempt heuristic resynchronization on the same TCP stream. Any bytes following the corrupt frame are discarded with that connection. This deliberately favors deterministic safety over trying to salvage a corrupted stream.

A distinction is required between **fatal structural/framing errors** and **recoverable protocol/application errors**. For example, a syntactically framed PDU with an unsupported command or invalid parameter value may still be answerable with the applicable SMPP status/generic_nack behavior; compatibility mode must never convert a framing-safety violation into a recoverable condition.

### Fatal protocol diagnostics

Before/while terminating a session for structural corruption, emit a structured error diagnostic. Include safe context when available:

- error category/reason
- session identifier and current state
- local/remote endpoint
- declared `command_length`
- `command_id`
- `sequence_number`

Do not dump passwords, full message payloads, or arbitrary PDU bodies by default. Fatal-protocol diagnostics are mandatory operational logs, distinct from optional packet tracing and per-PDU logging.

### Decode model

Prefer low-copy parsing. The codec may expose internal borrowed views whose fields reference the frame buffer. Public APIs that allow data to outlive the callback/frame lifetime must use owned copies or make lifetime constraints explicit.

Unknown TLVs should be preserved in an ordered form. Convenient lookup can be layered over the ordered representation.

## Session engine

A session owns:

- one transport connection
- peer/local role and bind mode
- current state
- local outbound sequence generator
- pending outbound request correlation
- outstanding window/backpressure
- session deadlines/timers
- RX processing loop
- TX serialization/queueing strategy
- reconnect state for client sessions
- event/metric/logging hooks

The RX path is authoritative for fatal decode/framing failures. Once such a failure is raised, session shutdown/transport close must be idempotent and safe against concurrent TX, timeout, caller `Close`, or reconnect activity. Pending requests are failed under the same exactly-once completion rules used for ordinary session loss.

### Bidirectional operation

TRX must allow outbound requests and inbound requests concurrently. An inbound request is not confused with a local pending request merely because its sequence number is equal; each peer has its own request sequence space.

### Pending correlation

The hot-path operations are:

```text
insert(sequence, pending)
lookup(sequence)
complete(sequence, response)
expire(sequence)
cancel(sequence)
fail-all-on-session-loss
```

Do not commit early to a single global mutex map as the permanent design. Start with the simplest correct bounded implementation that can be benchmarked, then compare sharding/fixed-slot alternatives under realistic windows.

Each pending request has exactly one terminal completion. A matching response, caller cancellation/deadline, protocol response timeout, fatal protocol/frame failure, or session loss may race, but only one path may remove the pending entry, release window capacity and wake the synchronous caller.

### Public synchronous API

Conceptual use:

```go
resp, err := sess.Submit(ctx, msg)
```

The call may block waiting for window/TX capacity and then for its response, but the session transport remains asynchronous and continues processing other requests/responses.

The implementation must not require a goroutine per call/request inside the library. Caller-created goroutines are the caller's choice.

### Timeout model

Timeouts are separate concepts and must not be collapsed into one ambiguous duration.

#### Caller context/deadline

The caller-provided context may bound the whole API call, including waiting for window capacity, local TX admission and response completion.

#### Request response timeout

Every outbound SMPP request that expects a response has a configurable protocol response timeout. This timer starts only after the request PDU has been fully dispatched to the transport.

On expiry:

1. the pending request is atomically marked expired/completed,
2. correlation state is removed,
3. its outstanding-window slot is released exactly once,
4. the synchronous caller receives a typed response-timeout error containing useful metadata such as command and sequence,
5. any later response is classified as late/unmatched and must not complete another request.

The final high-throughput implementation must avoid one independent `time.Timer` per outstanding request. Shared deadline structures such as deadline buckets, a batched heap or a timer wheel will be selected by benchmark.

#### Session Init timeout

Session Init timeout bounds the time between transport establishment and establishment of a valid SMPP session.

Relevant behavior includes:

- server: an accepted connection must send/complete the required bind/session-init flow within the configured limit,
- client: connect/bind session establishment is bounded and cannot remain indefinitely half-open,
- Outbind-related behavior can later use the same timer infrastructure when that SMPP 3.4 feature is implemented.

#### Enquire Link

When no SMPP activity has occurred for the configured Enquire Link interval, the session may originate `enquire_link` to test peer liveness. `enquire_link` uses the same request correlation and response-timeout machinery as other request/response PDUs.

Only one liveness probe should be active when policy requires it; the design must avoid an unbounded stream of probes while a previous probe is still outstanding.

#### Inactivity timeout

A configurable inactivity timeout tracks session activity. Healthy traffic, including valid liveness traffic, updates activity safely. If the inactivity policy expires, the session performs deterministic shutdown behavior (graceful unbind where appropriate/configured, followed by close as necessary).

Recommended configuration must keep Enquire Link timing meaningfully below the inactivity limit so a healthy idle peer can be probed before the session is discarded.

### Timer concurrency

RX traffic, TX traffic, timeout expiry, caller cancellation, `Close`, fatal protocol failure, and reconnect may all happen concurrently. Timer state and pending completion therefore require explicit synchronization/ownership rules. No timer callback may directly perform an uncoordinated second completion of a request.

## Concurrency and thread-safety contract

In Go terms, the library must be safe for concurrent use by multiple goroutines where documented. In particular:

- active `Client`, `Server` and `Session` instances are designed for concurrent calls,
- multiple goroutines may submit outbound requests concurrently,
- RX and TX processing occur concurrently,
- inbound application handlers may overlap according to bounded dispatch policy,
- sequence allocation is race-free,
- session state transitions are race-free,
- pending insert/response/timeout/cancel/session-loss operations are race-free,
- fatal decoder shutdown is race-free with normal close/reconnect/timeout paths,
- window acquire/release accounting is exact under races,
- `Close` is idempotent/concurrent-safe,
- auto-reconnect does not race with explicit close into resurrecting a closed client,
- event/metrics/logging hooks must not force unsafe access to internal mutable state.

The project must use `go test -race` continuously for concurrency scenarios, not only at release time.

Avoid unnecessary global locks in the hot path. Concurrency safety does not mean every object should share one mutex; ownership, immutable snapshots, sharding and carefully scoped locks/atomics may be used as measurements justify.

## Server architecture

The server accepts connections and creates one session object per accepted transport. Authentication is injected through a hook/interface; protocol packages do not know account databases.

A server session can:

- accept bind requests
- enforce Session Init timeout
- validate legal state transitions
- receive `submit_sm`
- invoke an application handler
- write `submit_sm_resp`
- originate `deliver_sm` on RX/TRX sessions
- correlate `deliver_sm_resp`
- apply request response timeouts to server-originated operations
- terminate only the offending session when fatal structural corruption is received

A malformed client connection must not terminate the listener or affect unrelated sessions.

Application handlers must be isolated so a slow handler cannot cause unbounded core memory growth.

## Client architecture

A client session can:

- dial TCP/TLS
- bind TX/RX/TRX
- enforce connect/bind Session Init timeout
- maintain Enquire Link/inactivity behavior
- send requests synchronously through the asynchronous engine
- apply per-request response timeouts
- receive inbound requests and dispatch handlers
- terminate a corrupted session on fatal malformed inbound PDU
- reconnect/rebind after transport loss or fatal protocol connection loss according to reconnect policy

A lost session fails its pending requests. Reconnect does not replay them automatically.

## Transport/TLS

Use a narrow boundary compatible with `net.Conn`. Built-in transports:

- TCP: `net.Dialer`, `net.Listener`
- TLS: `tls.Client`, `tls.Server` / TLS listeners

Benefits:

- no extra dependency
- caller controls certificates/cipher policy through `tls.Config`
- codec/session tests can use in-memory connections
- future proxy/custom transports can be injected without changing SMPP semantics

## SMPP version negotiation and 5.0 flow feedback

SMPP 5.0 does not create a second session engine. A configured protocol profile defines the maximum local capability and selects a standard 3.4 or 5.0 registry snapshot. Bind negotiation produces immutable-per-update `PeerCapabilities` for the active session. The client-side negotiated capability is capped by the `interface_version` actually sent in that bind; a process configured for 5.0 but binding as 3.4 must behave as 3.4 for that session. Reserved/pre-3.4 values do not imply modern TLV support.

On the SMSC side, a successful bind from a 3.4/5.0 ESME receives `sc_interface_version` automatically when the handler has not supplied one. Pre-3.4 ESMEs do not receive that TLV. V5-only Cell Broadcast commands are rejected before transmission unless 5.0 was mutually negotiated, and inbound v5-only commands without negotiated 5.0 capability are rejected without changing framing rules.

`congestion_state` is an advisory response signal, not a replacement for bounded ownership. The decoder preserves it on ordinary response bodies, header-only response bodies, and error responses where the standard body is omitted. A validated 0..100 value is delivered to the configured flow-controller hook. The outstanding-request window remains a hard independent limit so memory/pending state stays bounded even when a peer omits, delays, or misuses congestion feedback.

## Registries and vendor extensions

Registry responsibilities:

- map `command_id` to decoder/constructor metadata
- map TLV tag to optional typed decoding metadata
- permit custom/vendor registration
- avoid global mutable surprises where possible

Prefer explicit registry instances/configuration for deterministic tests and applications with different vendor profiles. A default standard registry may be provided as a convenience.

Registry mutation must be safe for concurrent configuration calls if exposed that way, or the API must provide an explicit build/freeze step that produces an immutable registry snapshot used by active sessions. The hot path should prefer immutable registry reads.

## Encoding/message layer

Encoding is intentionally above the protocol codec. A PDU codec should be able to transport arbitrary `short_message`/`message_payload` bytes without decoding GSM7/UCS2.

Separate packages handle:

- GSM 7-bit packing/unpacking
- UCS-2
- binary payloads
- UDH concatenation
- SAR TLVs
- multipart reassembly
- delivery receipt convenience parsing

## Memory ownership

Target rules:

- frame buffers have a clearly defined owner/lifetime
- avoid copying body fields during decode when a short-lived view is sufficient
- copy only when data escapes the frame lifetime
- pools are introduced only after benchmark evidence
- every pool has bounded retention behavior and tests for oversized-buffer retention
- timed-out/cancelled requests release all references that would otherwise retain request/PDU buffers
- fatal protocol shutdown releases/discards the corrupt receive buffer and does not recycle an untrusted parse state into another session

## Concurrency model

Initial target model:

- small fixed number of long-lived goroutines per session
- RX path continuously parses incoming frames
- TX path serializes writes safely
- request correlation is a data structure, not a goroutine fleet
- handlers may run through bounded dispatch depending on server/client API needs
- timer/deadline processing is centralized/shared rather than a goroutine/timer fleet per request
- backpressure is explicit

Exact goroutine count is an implementation detail and should be benchmarked.

## Error categories

Keep these distinguishable:

- transport errors
- session/state errors
- caller context cancellation/deadline
- protocol request response timeout
- Session Init timeout
- inactivity/liveness failure
- fatal protocol/framing/decode corruption
- recoverable SMPP response `command_status` / semantic protocol errors
- local overload/window errors
- reconnect/session-loss ambiguity

Applications need enough metadata to decide whether a message is safe to retry.
