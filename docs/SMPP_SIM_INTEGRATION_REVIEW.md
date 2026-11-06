# SMPP Test Studio integration review of go-smpp

Review date: 2026-11-07. Initial library baseline inspected: `28c44d3f9aac1c2a68896994e26ba7625ab25939`. Reviewed improvements were merged into `main` through `bff9b61c5f1a923e5d02e8fc1e613c460db4f476`.

Scope: review and improve **this library only** for future integration with `majiddarvishan/smpp_sim`, whose V1 product specification is frozen under D-001–D-040. **Do not implement the simulator in this workstream.**

## Recommendation

**Reuse this module** (`github.com/majiddarvishan/go-smpp`) as the single SMPP wire/session engine. It already supplies ESME TX/RX/TRX binds, concurrent `SubmitSM`, inbound `deliver_sm`, SMPP 3.4 wire codec, ordered TLVs, bounded correlation, response deadlines, request windows, reconnect/rebind, encoding helpers and session counters. Do not rewrite these behaviors in a second SMPP stack.

Pin a reviewed commit (or first immutable production-ready release) when integrating. `main` is not a reproducible dependency version. The module declares Go 1.25 as its minimum and has no external runtime dependencies.

## Findings and changes

| Priority | Area | Verified baseline | Improvement status |
| --- | --- | --- | --- |
| P0 | Inbound ACK fault injection | `session/rx.go` always queues the response returned by `Handler.Handle`. Sleeping in Handler would block RX framing. | **MERGED PR #3:** additive `Response.Action` controlling immediate, suppressed and deferred `deliver_sm_resp`. Delayed scheduling belongs to a **bounded application worker/queue**, not to the library or a goroutine per inbound message. Use `Session.RespondDeferred` against the original Session and header. |
| P0 | Payload interoperability | `message.SegmentTextUDH` uses **8-bit concatenation reference and GSM7 septet packing**, while smpp_sim V1 D-032 defaults to **16-bit UDH** and **unpacked GSM7** for its SMPP dialect. Existing lower-level `message.EncodeText`, `message.BuildConcatUDH16` and `encoding` helpers are available. | **MERGED PR #4/#5:** GSM7 escape-safe segmentation and new `SegmentTextUDH16Unpacked` / `SegmentBinaryUDH16` helpers with 152/66/133 part limits. Existing `SegmentTextUDH` behavior remains unchanged. |
| P0 | Reconnect policy | `client.ReconnectPolicy` has configurable base/cap/multiplier, but no jitter and currently retries even a permanent `StatusInvalidPassword` or `StatusInvalidSystemID` bind response. Defaults 100ms→5s differ from smpp_sim D-039 1/2/4/8/16/30s. | **MERGED PR #6:** optional `JitterFraction` and `StopOnPermanentBindFailure`, explicitly classifying invalid password/system ID/system type. Default reconnect behavior is unchanged; the eventual adapter selects 1s/30s/multiplier 2 and enables these options. Generic `StatusBindFailed` is retryable. |
| P0 | Outstanding window | Library `session.Config.WindowSize` is a **fixed hard admission bound** (default 1,024), whereas smpp_sim D-037 Adaptive Window has an application-side batch controller (initial 32, max 1,024, 3s batch decision). | Keep the Adaptive Window controller above the library, with `WindowSize >= adaptive max`, respecting the global 8,192 pending PDU cap. Keep the library's 5s per-PDU response deadline distinct from the 3s controller decision deadline. |
| P1 | DLR parsing | The library decodes typed `protocol.DeliverSM` and ordered optional TLVs, but does not prescribe D-033 receipt parsing semantics. | Provide a separate optional receipt parser in `message` or the simulator integration adapter: **text `id:` first**, TLV fallback, but **state TLV first**, text `stat:` fallback. Preserve both IDs on disagreement; no global normalization assumptions. |
| P1 | Diagnostics/trace safety | Opt-in raw `PacketTracer` copies bind passwords and message text. | Simulator should default raw trace off, enforce redaction and bounded capture. Consider a safe header-only trace interface for telemetry as a later compatible addition. |
| P1 | API/release | Module public API is frozen for Phase 19; no first production-ready `v1.0.0` release is confirmed in the reviewed README/checklist. | Keep all additions backward compatible and update `testdata/api/session.txt` with the new API; do not imply a release or benchmark succeeded. |

## Confirmed inbound response contract (merged PR #3)

`ResponseImmediate` (zero value) retains the old synchronous auto-reply semantics. An inbound `deliver_sm` may choose:

- `ResponseSuppress`: no `deliver_sm_resp`, no timer or retained deferred token; the RX processing loop stays free to receive subsequent PDUs.
- `ResponseDeferred`: reserve a bounded one-shot reply token by original inbound sequence number in the originating `Session`. An application-managed scheduler can call `Session.RespondDeferred(originalHeader, Response{Status: ..., Body: protocol.DeliverSMResp{}})` later, or `AbandonDeferredResponse`. Once enqueued, an identical second ACK is rejected.
- The bound is `Config.MaxDeferredResponses` (default 1,024 **per Session**). The simulator still owns its stricter **10,000 queued delayed ACKs per Run** bound from D-036; the two limits are not interchangeable. Reaching the library cap is an explicit error and closes the affected session, rather than silently dropping or converting configured ACK actions.
- Deferred ACK registration occurs when the Handler **returns**, so a scheduler should invoke `RespondDeferred` only after that return; for immediate replies use `ResponseImmediate`. Retain only the copied `pdu.Header` and independently owned response body—not decoded borrowed PDU buffer slices.
- No deferred or suppressed replies are allowed for bind/keepalive/unbind lifecycle commands. The Session's own outstanding request window and receive path remain independent from the app's delayed-ACK queue.
- The reply token and one-shot mapping are local to the exact `Session`; a newly reconnected bound session never inherits or automatically retransmits it. On close, deferred state is discarded.

## Executed CI and remaining integration gates

- **Completed in GitHub Actions:** PR #3, #4, #5 and #6 each passed their respective Go 1.25 and Go 1.27 workflow matrices before merge. These workflows include gofmt, unit tests, API snapshot, race tests, coverage floor, fuzz smoke, performance smoke and no-direct-unsafe checks. See the PR pages and run results for the actual evidence; these are **library CI** results, not simulator acceptance.
- **Still unverified:** full `smpp_sim` integration and D-035/D-036 ACK distribution under load; negative/delayed receipt ACK interoperability with real vendor SMSCs; a combined benchmark after activating all simulator workload controllers; 50k physical `submit_sm` PDUs/s averaged for five minutes. Do not infer such PASS from library CI.
- The existing Phase 17 library reference report in `docs/PERFORMANCE.md` records **713,932 request PDUs/s for 60s on localhost**. It is not equivalent to the new simulator's 50k physical submit target.
- **Recommended next library-only tasks:** optional DLR receipt parser with D-033 text-first message ID/TLV-first state and conflict diagnostics; explicit vendor interoperability fixtures; safe redacted trace defaults; a separate long-running throughput/regression check on the merged HEAD; decide release tagging only after the library's own release gates are satisfied.
- **Dependency version:** pin the merged SHA `bff9b61c5f1a923e5d02e8fc1e613c460db4f476` until an immutable reviewed release tag exists. Do not pin mutable `main`.

**No simulator code or frozen product decisions are altered by this review.**
