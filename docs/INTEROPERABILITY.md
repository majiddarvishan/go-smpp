# Interoperability notes

## Sequence numbers

Locally generated request sequence numbers use 1..0x7fffffff. Inbound non-zero request sequence numbers through 0xffffffff are accepted for interoperability and responses echo the received value exactly.

Sequence numbers are session-local. Responses from a previous TCP connection are never matched to a replacement session.

## TCP framing and malformed input

TCP read boundaries are not PDU boundaries. The framer accepts fragmented PDUs and multiple coalesced PDUs from one read.

Structural corruption is fail-closed. Examples include command_length smaller than 16, a PDU above the configured maximum, mandatory fields or C-Octet Strings escaping the declared frame, TLV overruns, and EOF with a partial frame.

After a fatal structural error, the framer is poisoned, a structured diagnostic is emitted, and the offending TCP session is closed. The library never scans forward for a plausible next header or attempts byte-stream resynchronization. Server mode keeps the listener and unrelated sessions healthy.

The default fatal diagnostic contains safe metadata such as reason, state, role, declared length, command id, sequence number, and endpoint addresses. It does not include bind passwords, short_message, message_payload, or a full raw PDU unless explicit raw tracing is separately enabled.

## generic_nack status on a semantic/body error

A structural/framing error is fail-closed (see above): the session closes, no response is sent. A semantic error — the frame's structure is trustworthy but its content violates a decode-time rule (a wrong-length TLV, a body longer or shorter than the fields it should hold, a numeric field outside its valid range, conflicting fields) — is different: the session stays open and replies with generic_nack, whose command_status names what was actually wrong rather than a single blanket code for every kind of semantic error. Examples: a bind_response's sc_interface_version TLV with the wrong length gets StatusInvalidParameterLength; a header-only command (enquire_link, unbind, …) with an unexpected trailing octet, or a body with trailing octets past its last defined field, gets StatusInvalidCommandLength; a submit_multi with number_of_dests 0 or 255 gets StatusInvalidNumberOfDestinations (the SMPP status made for exactly this); a submit_sm/submit_multi/replace_sm with both short_message and message_payload carrying data gets StatusInvalidOptionalParameterValue. StatusInvalidMessageLength remains the default only where the decoder's error genuinely is about a message's length (submit_sm's sm_length 255, disallowed under SMPP 3.4), or where no more specific status exists for the condition (a request PDU with a nonzero command_status header field; an inbound sequence_number outside the valid range, which gets StatusSystemError instead since it is not a length problem at all) — not as a catch-all for every semantic error regardless of its actual shape.

## Unknown commands and TLVs

Compatible registry mode preserves structurally valid unknown/duplicate TLVs and leaves unknown commands unresolved for protocol-level handling. Strict mode reports unknown command/TLV errors. Neither mode makes invalid framing recoverable.

## Delivery receipt parsing (optional)

`message.ParseDeliveryReceipt` accepts one decoded `protocol.DeliverSM` and returns standard receipt metadata without interacting with Session, acknowledgement or request-correlation state.

It preserves the conventional text fields `id`, `sub`, `dlvrd`, `submit date`, `done date`, `stat`, `err` and trailing `text`. Counts, dates and error codes remain raw strings so callers can apply peer-specific interpretation without losing leading zeroes. The standard `deliver_sm` receipt TLVs are `receipted_message_id` and `message_state`, with `network_error_code` optionally present for SMSC Delivery Receipts and Intermediate Notifications. SMPP 3.4 defines `delivery_failure_reason` for `data_sm_resp`, while `additional_status_info_text` describes a response PDU. The parser tolerates and validates these two tags if a peer supplies them on `deliver_sm`, but treats them as peer-supplied diagnostics: neither tag alone identifies a delivery receipt.

**Identifier selection:** text `id:` takes precedence over `receipted_message_id` TLV. IDs remain opaque and case/leading-zero sensitive; both sources and a conflict flag are retained. **Status selection:** valid `message_state` TLV takes precedence over recognized text `stat:`; both sources and a conflict flag remain available. `SMPPMessageState()` returns the selected value as `protocol.MessageState`. Unknown vendor status labels remain literal text and never become fabricated standard states.

Text parsing supports conventional ASCII-compatible `data_coding=0x00`, IA5 `0x01`, Latin-1 `0x03`, and strict UCS-2/BMP `0x08`. Other encodings return `ErrUnsupportedDeliveryReceiptDataCoding` for a recognized receipt. Packed GSM7 is not guessed because the exact septet count/padding cannot be reliably inferred from `short_message` octet length alone.

Malformed/contradictory duplicate TLVs and text fields return `ErrInvalidDeliveryReceipt`; identical duplicate TLVs are accepted. Ordinary `deliver_sm` without recognizable receipt evidence returns `ErrNotDeliveryReceipt`. This parser does not correlate receipts, define delivery guarantees, classify a message as billable, or manage late/duplicate/missing receipt retention.

## registered_delivery bit fields

PDU structs continue to expose the raw `registered_delivery` octet as `uint8`. `protocol.DecodeRegisteredDelivery` provides a lossless field-level view and `RegisteredDelivery.Encode()` reconstructs the original octet.

The field layout is: bits 0-1 are the SMSC delivery-receipt request; bits 2-3 are the SME acknowledgement request; bit 4 requests intermediate notification; bits 5-7 are reserved and are preserved in `ReservedBits`.

For the SMSC receipt field, `SMSCReceiptModeNone`, `SMSCReceiptModeFinalOutcome` and `SMSCReceiptModeFailureOnly` represent the standard SMPP 3.4 encodings. SMPP 3.4 reserves the value `0b11`. The library also exposes `SMSCReceiptModeSuccessOnly` as a named, peer-specific interpretation of that same wire value for SMSCs that explicitly document or negotiate it as success-only. It is not a portable SMPP 3.4 meaning and must not be assumed without peer agreement. `SMSCReceiptModeReserved` remains available as the standards-oriented alias for the same raw value.

The decoder and encoder preserve every wire bit. They do not infer whether a peer implements the success-only convention and do not reject or normalize reserved/vendor values.

## deliver_sm_resp

The encoder emits the standard empty C-Octet message_id. The decoder also accepts a header-only form for interoperability with deployed peers that omit that empty field.

## SMPP 3.4 / 5.0 negotiation

The library uses one shared core. SMPP 5.0-only behavior is enabled only after capability negotiation. Missing sc_interface_version does not silently imply 5.0 capability.

congestion_state feedback is separate from the hard in-flight window. Applications may connect the optional flow-controller callback to their own rate controller.

## GSM 7-bit

The encoding package implements the GSM 03.38 default alphabet and extension table. Extension characters consume two septets. Multipart sizing is based on encoded septets, not UTF-8 bytes or Go rune count. GSM 03.38 extension-table characters have an ESC septet followed by the extension-table code; UDH and SAR multipart splitters must move both to the same segment instead of splitting at the nominal septet boundary. This may leave one unused septet in an earlier segment and is required to preserve exact content.

encoding.PackSeptets and PackSeptetsWithFill provide septet packing. UDH-bearing GSM7 parts account for fill bits.

A second explicit helper, `message.SegmentTextUDH16Unpacked`, builds **16-bit concatenation UDH** with **unpacked GSM7 septet octets** and strict UCS2/optional surrogate-preserving UTF16-BE. Its multipart capacities are 152 GSM7 septets or 66 UCS2 code units per part; `message.SegmentBinaryUDH16` uses 133 binary content octets per part. Both retain at most 255 parts and never split GSM7 escape pairs or UTF-16 surrogate pairs. The existing `SegmentTextUDH` remains unchanged (8-bit reference, packed GSM7); callers must select the representation accepted by their SMSC.

Some SMPP peers expect data_coding=0 text in an unpacked local character convention rather than TP-UD-style packed septets. Encoding/segmentation is separate from the low-level PDU codec so the application can choose the representation required by its SMSC.

## Unicode and emoji

Strict UCS-2 and UTF-16BE-with-surrogates are intentionally separate modes.

- strict UCS-2 accepts only BMP non-surrogate code points;
- supplementary-plane characters, including most emoji, require UTF-16 surrogate pairs;
- message.EncodeText(text, false) rejects text that needs surrogate pairs;
- message.EncodeText(text, true) permits UTF-16BE surrogate pairs.

For SMPP interoperability, strict UCS-2 and surrogate-enabled UTF-16BE use the SMPP data_coding value associated with UCS-2. Not every SMSC/carrier accepts surrogate pairs under that value, so applications should enable emoji only for peers/routes known to preserve them.

Multipart Unicode sizing uses 16-bit code units and never splits a surrogate pair across parts.
