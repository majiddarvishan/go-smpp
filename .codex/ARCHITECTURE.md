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
- event/metric hooks

### Bidirectional operation

TRX must allow outbound requests and inbound requests concurrently. An inbound request is not confused with a local pending request merely because its sequence number is equal; each peer has its own request sequence space.

### Pending correlation

The hot-path operations are:

```text
insert(sequence, pending)
lookup(sequence)
complete(sequence, response)
expire(sequence)
fail-all-on-session-loss
```

Do not commit early to a single global mutex map as the permanent design. Start with the simplest correct bounded implementation that can be benchmarked, then compare sharding/fixed-slot alternatives under realistic windows.

### Public synchronous API

Conceptual use:

```go
resp, err := sess.Submit(ctx, msg)
```

The call may block waiting for window capacity and then for its response, but the session transport remains asynchronous and continues processing other requests/responses.

The implementation must not require a goroutine per call/request inside the library. Caller-created goroutines are the caller's choice.

## Server architecture

The server accepts connections and creates one session object per accepted transport. Authentication is injected through a hook/interface; protocol packages do not know account databases.

A server session can:

- accept bind requests
- validate legal state transitions
- receive `submit_sm`
- invoke an application handler
- write `submit_sm_resp`
- originate `deliver_sm` on RX/TRX sessions
- correlate `deliver_sm_resp`

Application handlers must be isolated so a slow handler cannot cause unbounded core memory growth.

## Client architecture

A client session can:

- dial TCP/TLS
- bind TX/RX/TRX
- maintain enquire-link/inactivity behavior
- send requests synchronously through the asynchronous engine
- receive inbound requests and dispatch handlers
- reconnect/rebind after transport loss

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

## Registries and vendor extensions

Registry responsibilities:

- map `command_id` to decoder/constructor metadata
- map TLV tag to optional typed decoding metadata
- permit custom/vendor registration
- avoid global mutable surprises where possible

Prefer explicit registry instances/configuration for deterministic tests and applications with different vendor profiles. A default standard registry may be provided as a convenience.

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

## Concurrency model

Initial target model:

- small fixed number of long-lived goroutines per session
- RX path continuously parses incoming frames
- TX path serializes writes safely
- request correlation is a data structure, not a goroutine fleet
- handlers may run through bounded dispatch depending on server/client API needs
- backpressure is explicit

Exact goroutine count is an implementation detail and should be benchmarked.

## Error categories

Keep these distinguishable:

- transport errors
- session/state errors
- context cancellation/deadline
- SMPP response `command_status` errors
- protocol/decode errors
- local overload/window errors
- reconnect/session-loss ambiguity

Applications need enough metadata to decide whether a message is safe to retry.
