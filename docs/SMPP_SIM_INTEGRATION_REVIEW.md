# SMPP Test Studio integration review of go-smpp

Review date: 2026-11-07. Library baseline inspected: `main` at `28c44d3f9aac1c2a68896994e26ba7625ab25939`.

Scope: review and improve **this library only** for future integration with `majiddarvishan/smpp_sim`, whose V1 product specification is frozen under D-001–D-040. **Do not implement the simulator in this workstream.**

## Recommendation

**Reuse this module** (`github.com/majiddarvishan/go-smpp`) as the single SMPP wire/session engine. It already supplies ESME TX/RX/TRX binds, concurrent `SubmitSM`, inbound `deliver_sm`, SMPP 3.4 wire codec, ordered TLVs, bounded correlation, response deadlines, request windows, reconnect/rebind, encoding helpers and session counters. Do not rewrite these behaviors in a second SMPP stack.

Pin a reviewed commit (or first immutable production-ready release) when integrating. `main` is not a reproducible dependency version. The module declares Go 1.25 as its minimum and has no external runtime dependencies.

## Findings and changes

| Priority | Area | Verified baseline | Proposed action |
| --- | --- | --- | --- |
| P0 | Inbound ACK fault injection | `session/rx.go` always queues the response returned by `Handler.Handle`. Sleeping in Handler would block RX framing. | **This branch:** additive `Response.Action` controlling immediate, suppressed and deferred `deliver_sm_resp`. Delayed scheduling belongs to a **bounded application worker/queue**, not to the library or a goroutine per inbound message. Use `Session.RespondDeferred` against the original Session and header. |
| P0 | Payload interoperability | `message.SegmentTextUDH` uses **8-bit concatenation reference and GSM7 septet packing**, while smpp_sim V1 D-032 defaults to **16-bit UDH** and **unpacked GSM7** for its SMPP dialect. Existing lower-level `message.EncodeText`, `message.BuildConcatUDH16` and `encoding` helpers are available. | Add a **new** explicit helper/configured adapter after focused wire fixtures; do not silently change `SegmentTextUDH` or break existing clients. Compare 152/153-septet and 66/67-UCS2 boundaries. |
| P0 | Reconnect policy | `client.ReconnectPolicy` has configurable base/cap/multiplier, but no jitter and currently retries even a permanent `StatusInvalidPassword` or `StatusInvalidSystemID` bind response. Defaults 100ms→5s differ from smpp_sim D-039 1/2/4/8/16/30s. | Set 1s/30s/multiplier 2 in the adapter, and add optional, backward-compatible jitter plus configurable terminal-bind-status classification to the library with deterministic retry tests. Do not silently make a transient generic `StatusBindFailed` permanent. |
| P0 | Outstanding window | Library `session.Config.WindowSize` is a **fixed hard admission bound** (default 1,024), whereas smpp_sim D-037 Adaptive Window has an application-side batch controller (initial 32, max 1,024, 3s batch decision). | Keep the Adaptive Window controller above the library, with `WindowSize >= adaptive max`, respecting the global 8,192 pending PDU cap. Keep the library's 5s per-PDU response deadline distinct from the 3s controller decision deadline. |
| P1 | DLR parsing | The library decodes typed `protocol.DeliverSM` and ordered optional TLVs, but does not prescribe D-033 receipt parsing semantics. | Provide a separate optional receipt parser in `message` or the simulator integration adapter: **text `id:` first**, TLV fallback, but **state TLV first**, text `stat:` fallback. Preserve both IDs on disagreement; no global normalization assumptions. |
| P1 | Diagnostics/trace safety | Opt-in raw `PacketTracer` copies bind passwords and message text. | Simulator should default raw trace off, enforce redaction and bounded capture. Consider a safe header-only trace interface for telemetry as a later compatible addition. |
| P1 | API/release | Module public API is frozen for Phase 19; no first production-ready `v1.0.0` release is confirmed in the reviewed README/checklist. | Keep all additions backward compatible and update `testdata/api/session.txt` with the new API; do not imply a release or benchmark succeeded. |

## New inbound response contract on this branch

`ResponseImmediate` (zero value) retains the old synchronous auto-reply semantics. An inbound `deliver_sm` may choose:

- `ResponseSuppress`: no `deliver_sm_resp`, no timer or retained deferred token; the RX processing loop stays free to receive subsequent PDUs.
- `ResponseDeferred`: reserve a bounded one-shot reply token by original inbound sequence number in the originating `Session`. An application-managed scheduler can call `Session.RespondDeferred(originalHeader, Response{Status: ..., Body: protocol.DeliverSMResp{}})` later, or `AbandonDeferredResponse`. Once enqueued, an identical second ACK is rejected.
- The bound is `Config.MaxDeferredResponses` (default 1,024 **per Session**). The simulator still owns its stricter **10,000 queued delayed ACKs per Run** bound from D-036; the two limits are not interchangeable. Reaching the library cap is an explicit error and closes the affected session, rather than silently dropping or converting configured ACK actions.
- Deferred ACK registration occurs when the Handler **returns**, so a scheduler should invoke `RespondDeferred` only after that return; for immediate replies use `ResponseImmediate`. Retain only the copied `pdu.Header` and independently owned response body—not decoded borrowed PDU buffer slices.
- No deferred or suppressed replies are allowed for bind/keepalive/unbind lifecycle commands. The Session's own outstanding request window and receive path remain independent from the app's delayed-ACK queue.
- The reply token and one-shot mapping are local to the exact `Session`; a newly reconnected bound session never inherits or automatically retransmits it. On close, deferred state is discarded.

## Verification gates (not yet claimed PASS)

- `go test ./session ./client ./message ./encoding ./codec`, then `go test -race ./session ./client`, then `go test ./...`.
- Full API snapshot test (additive symbols acknowledged), no-unsafe/no-external-dependency posture, Go 1.25 compatibility.
- Network-level tests: suppress, delayed success/error, double-ACK rejection, cap overflow, session-close before delayed ACK, 3.4 bind parity, concurrent submit+DLR, handler panic isolation and no per-message goroutine.
- Reference Phase 17 performance evidence in `docs/PERFORMANCE.md` reports **713,932 request PDUs/s in 60s on localhost**; this does not prove `smpp_sim` can sustain **50k physical submit PDUs/s for five minutes** with its real traffic generator, DLR processing, reports and user interface.

**No simulator code or frozen product decisions are altered by this review.**
