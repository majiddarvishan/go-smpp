# Interoperability notes

## Sequence numbers

Locally generated request sequence numbers use 1..0x7fffffff. Inbound non-zero request sequence numbers through 0xffffffff are accepted for interoperability and responses echo the received value exactly.

Sequence numbers are session-local. Responses from a previous TCP connection are never matched to a replacement session.

## TCP framing and malformed input

TCP read boundaries are not PDU boundaries. The framer accepts fragmented PDUs and multiple coalesced PDUs from one read.

Structural corruption is fail-closed. Examples include command_length smaller than 16, a PDU above the configured maximum, mandatory fields or C-Octet Strings escaping the declared frame, TLV overruns, and EOF with a partial frame.

After a fatal structural error, the framer is poisoned, a structured diagnostic is emitted, and the offending TCP session is closed. The library never scans forward for a plausible next header or attempts byte-stream resynchronization. Server mode keeps the listener and unrelated sessions healthy.

The default fatal diagnostic contains safe metadata such as reason, state, role, declared length, command id, sequence number, and endpoint addresses. It does not include bind passwords, short_message, message_payload, or a full raw PDU unless explicit raw tracing is separately enabled.

## Unknown commands and TLVs

Compatible registry mode preserves structurally valid unknown/duplicate TLVs and leaves unknown commands unresolved for protocol-level handling. Strict mode reports unknown command/TLV errors. Neither mode makes invalid framing recoverable.

## deliver_sm_resp

The encoder emits the standard empty C-Octet message_id. The decoder also accepts a header-only form for interoperability with deployed peers that omit that empty field.

## SMPP 3.4 / 5.0 negotiation

The library uses one shared core. SMPP 5.0-only behavior is enabled only after capability negotiation. Missing sc_interface_version does not silently imply 5.0 capability.

congestion_state feedback is separate from the hard in-flight window. Applications may connect the optional flow-controller callback to their own rate controller.

## GSM 7-bit

The encoding package implements the GSM 03.38 default alphabet and extension table. Extension characters consume two septets. Multipart sizing is based on encoded septets, not UTF-8 bytes or Go rune count.

encoding.PackSeptets and PackSeptetsWithFill provide septet packing. UDH-bearing GSM7 parts account for fill bits.

Some SMPP peers expect data_coding=0 text in an unpacked local character convention rather than TP-UD-style packed septets. Encoding/segmentation is separate from the low-level PDU codec so the application can choose the representation required by its SMSC.

## Unicode and emoji

Strict UCS-2 and UTF-16BE-with-surrogates are intentionally separate modes.

- strict UCS-2 accepts only BMP non-surrogate code points;
- supplementary-plane characters, including most emoji, require UTF-16 surrogate pairs;
- message.EncodeText(text, false) rejects text that needs surrogate pairs;
- message.EncodeText(text, true) permits UTF-16BE surrogate pairs.

For SMPP interoperability, strict UCS-2 and surrogate-enabled UTF-16BE use the SMPP data_coding value associated with UCS-2. Not every SMSC/carrier accepts surrogate pairs under that value, so applications should enable emoji only for peers/routes known to preserve them.

Multipart Unicode sizing uses 16-bit code units and never splits a surrogate pair across parts.
