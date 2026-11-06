# Delivery Receipt Parser — Library Contract

**Scope:** `go-smpp` `message` package only, Phase 20 / DLR-01..07. Approved library improvement; **not simulator implementation**.

## Public API

`message.ParseDeliveryReceipt(protocol.DeliverSM) (message.DeliveryReceipt, error)`

- Pure optional parsing over an already decoded `protocol.DeliverSM`. No networking, ACK behavior, queues, retries, timers, stateful correlation or goroutines. Session Handler still controls `deliver_sm_resp`.
- Exact opaque `MessageID`: text `id:` field first, `receipted_message_id` TLV fallback. Case and leading zeros **are significant**. Both `TextMessageID` and `TLVMessageID` remain visible; `MessageIDConflict` reports disagreement.
- Numeric `State` follows SMPP 3.4 `message_state` TLV (values 1..8) first, recognized text `stat:` fallback. `HasState` is false when there is no recognized state. `TextStatus` keeps unknown labels for vendor adapters; `StateConflict` records disagreement between two recognized statuses.
- TLV IDs may include one terminal C-Octet NUL or no terminal NUL. Empty IDs, embedded NULs, conflicting duplicate values and malformed/unknown `message_state` are **errors**; identical duplicate values are tolerated. Neither conflict flag silently chooses a new normalization.
- Text metadata parses ASCII-compatible `short_message`, or optional `message_payload` when `short_message` is empty; `data_coding=0x08` is strictly UCS-2/BMP decoded. Parsing stops at `text:` to avoid accidentally treating echoed SMS content as receipt metadata. Unsupported payload codecs are not guessed.
- Ordinary `deliver_sm` with no recognizable receipt metadata yields `ErrNotDeliveryReceipt`. Malformed/ambiguous receipt fields yield `ErrInvalidDeliveryReceipt`. The caller should log/categorize those outcomes without fabricating message delivery status.

## Boundaries

1. This helper does **not** determine whether a requested DLR is missing, delivered, late, duplicated or expired in a Test Run. Those decisions require the simulator's separately bounded correlation store and D-033/D-039 policy.
2. An accepted `submit_sm_resp` is not a delivered status.
3. A textual `id:` and TLV ID are not interpreted as interchangeable decimal/hex strings; vendor normalization must be opt-in, separately reviewed and tested.
4. Do not enable raw-PDU tracing by default. PDU payloads may contain phone numbers, message content and credentials.
5. If new external SMSC samples conflict with this baseline, add focused fixtures and an explicit compatible parser option rather than silently changing the defaults.

## Verification and handoff

See `PLAN.md` Phase 20 and `.codex/TASKS.md`. Require a clean Go 1.25/1.27 CI matrix (format/unit/race/API/no-panic/fuzz). Store PR link, verification outcome and exact next work in `.codex/SESSION.md`. Neither the library's formal reference-machine throughput goal nor the independent `smpp_sim` 50k throughput objective is closed by these tests.
