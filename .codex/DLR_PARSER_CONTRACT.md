# Delivery Receipt Parsing — Generic Library Contract

**Scope:** reusable `go-smpp/message` API. This document defines protocol-level parsing only; it does not define application correlation, charging, scheduling, retention or retry policy.

## Public API

`message.ParseDeliveryReceipt(protocol.DeliverSM) (message.DeliveryReceipt, error)`

The parser is pure over an already decoded `deliver_sm`. It performs no network I/O, acknowledgement, request correlation, queueing, timers, retries or goroutine creation. The Session handler remains responsible for the `deliver_sm_resp` lifecycle.

## Identifier and status selection

- The text `id:` field is selected before the `receipted_message_id` TLV.
- IDs remain opaque and case/leading-zero sensitive; no decimal/hex normalization is applied.
- Both original values remain available. `MessageIDConflict` reports disagreement.
- Numeric state independently selects valid `message_state` TLV before recognized text `stat:`.
- Both original state sources remain available; `StateConflict` reports disagreement.
- `SMPPMessageState()` exposes the selected standard value as `protocol.MessageState` without changing the existing raw numeric fields.
- Unknown text status labels remain in `TextStatus`; no standard state is invented.

## Standard receipt metadata

The parser preserves the conventional textual fields as raw strings: `id`, `sub`, `dlvrd`, `submit date`, `done date`, `stat`, `err` and the trailing `text` content. Counts, dates and error codes are not numerically or calendrically normalized because deployed peer formats vary.

The standard `deliver_sm` receipt TLVs are:

- `receipted_message_id`: C-Octet string with one final NUL terminator, no embedded NUL, and at most 65 octets including the terminator. Its payload remains opaque; no character-set or UTF-8 normalization is applied.
- `message_state`: exactly one octet, standard values 1..8.
- `network_error_code`: exactly three octets, preserved byte-for-byte; SMPP 3.4 permits it on SMSC Delivery Receipts and Intermediate Notifications.

Two additional standard TLV tags are tolerated and validated if a peer supplies them on `deliver_sm`, but SMPP 3.4 does not define them as standard `deliver_sm` receipt fields:

- `delivery_failure_reason`: exactly one octet. The specification defines it for `data_sm_resp`, not `deliver_sm`.
- `additional_status_info_text`: C-Octet string with one final NUL terminator, no embedded NUL, and at most 256 octets including the terminator. It is a response-PDU diagnostic, not standard `deliver_sm` receipt evidence. An empty payload is retained.

These two response-oriented TLVs are preserved as peer-supplied diagnostics when present, but neither one alone makes an ordinary `deliver_sm` a delivery receipt. Identical duplicate TLVs are tolerated; conflicting duplicates and malformed lengths/strings are errors. C-Octet terminators and field-specific maximum lengths are enforced. The trailing `text:` field removes exactly one separator whitespace byte after the marker and preserves the remaining payload byte-for-byte after text decoding. Unknown optional TLVs remain available on the original decoded PDU.

## Text encodings

The parser supports conventional ASCII-compatible receipt text for `data_coding=0x00`, strict IA5 (`0x01`), Latin-1 (`0x03`) and strict UCS-2/BMP (`0x08`). Unsupported data coding on a recognized receipt returns `ErrUnsupportedDeliveryReceiptDataCoding`; invalid IA5/UCS-2 returns `ErrInvalidDeliveryReceipt`.

Packed GSM7 and other national alphabets are not guessed. In particular, `short_message` octet length alone does not reliably provide the original packed-GSM7 septet count and padding. Add support only with a sound protocol-level length source and interoperable fixtures.

## Error semantics

- `ErrNotDeliveryReceipt`: no recognizable receipt marker, standard receipt TLV or text receipt pattern.
- `ErrInvalidDeliveryReceipt`: malformed or contradictory receipt metadata.
- `ErrUnsupportedDeliveryReceiptDataCoding`: a recognized receipt carries text in a coding the parser deliberately does not decode.

The parser is not a delivery guarantee and does not determine whether a receipt is late, duplicated, missing, billable or correlated to a particular application request. Those policies belong to each consuming application.

## Compatibility and verification

The parser is additive. Existing `protocol.DeliverSM`, Session ACK behavior and public raw fields remain unchanged. API snapshots and no-panic probes cover all exported additions. See `PLAN.md` Phase 21 and `.codex/TASKS.md` for implementation and verification status.
