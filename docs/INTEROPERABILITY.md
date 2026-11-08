# Interoperability notes

## Sequence numbers

Locally generated request sequence numbers use 1..0x7fffffff. Inbound non-zero request sequence numbers through 0xffffffff are accepted for interoperability and responses echo the received value exactly.

Sequence numbers are session-local. Responses from a previous TCP connection are never matched to a replacement session.

## TCP framing and malformed input

TCP read boundaries are not PDU boundaries. The framer accepts fragmented PDUs and multiple coalesced PDUs from one read.

Structural corruption is fail-closed. Examples include command_length smaller than 16, a PDU above the configured maximum, mandatory fields or C-Octet Strings escaping the declared frame, TLV overruns, and EOF with a partial frame.

After a fatal structural error, the framer is poisoned, a structured diagnostic is emitted, and the offending TCP session is closed. The library never scans forward for a plausible next header or attempts byte-stream resynchronization. Server mode keeps the listener and unrelated sessions healthy.

The default fatal diagnostic contains safe metadata such as fatal kind, state, role, declared length, command id, sequence number, and endpoint addresses. It never includes arbitrary decoder Reason text, bind passwords, short_message, message_payload or a full raw PDU. Explicit raw packet tracing is a separate opt-in path and is not appended to fatal diagnostic logs.

## Packet tracing and raw capture

Packet tracing is disabled unless a `PacketTracer` is configured. A configured tracer receives metadata only by default: direction, timestamp, decoded header and frame length. `RawPDU` remains nil unless `Config.TraceRawPDU` is also explicitly enabled; setting that flag without a tracer has no effect.

Raw mode is **unredacted** and returns an owned copy of the complete wire frame. Bind and outbind passwords are transmitted in clear text. Other frames may carry system IDs, source/destination addresses, message bodies, message IDs, optional parameters and arbitrary vendor TLVs. The generic library cannot safely redact unknown or peer-specific TLV contents. Treat every raw frame as secret material, not merely the bind password field.

Prefer metadata-only traces. If a raw capture is essential, use synthetic data where possible and keep it short-lived, isolated and local, with restricted filesystem access and encryption at rest. Drop bind_transmitter, bind_receiver, bind_transceiver and outbind frames entirely before persistence. Sanitize any other frame with a peer-aware allowlist that removes addresses, message content, identifiers and unknown/vendor TLVs. A fixed-offset password mask is not sufficient. If a trustworthy sanitizer is unavailable, discard the raw bytes and retain only metadata. Do not forward unsanitized frames to ordinary logs, shared telemetry, support bundles or third-party services.

The tracer callback runs synchronously on the RX or TX session goroutine. It must return promptly and must not perform blocking I/O; the library creates no trace worker or internal queue. A tracer that blocks can stall session processing. A panic in the tracer is recovered, but its panic value is not written to library logs/events.

Library-owned diagnostics also avoid formatting caller-controlled text: recovered panic logs contain source, panic type and stack while Observer events contain source and panic type; neither includes the panic value. Decode-failure events expose the error type rather than arbitrary decoder error text, and one-way handler error logs expose the error type rather than its message. `protocol.PanicError.Value`, `protocol.SemanticError.Reason` and `protocol.FatalError.Reason` remain available for explicit inspection and may themselves contain sensitive data; their `Error()` methods omit those values, and callers must not log or format them directly.

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


## Local simulator interoperability probe

The repository includes an opt-in, build-tagged local probe for testing the library against separate SMPP 3.4 simulator components. It exercises both roles sequentially:

1. go-smpp as an ESME connects to an external SMSC at `127.0.0.1:3000`, binds TRX, sends enquire_link and submit_sm with `registered_delivery=0x03`, and observes any returned deliver_sm/DLR.
2. go-smpp as an SMSC listens at `127.0.0.1:4000`, accepts one external ESME TRX bind, sends enquire_link and a synthetic deliver_sm receipt, and observes the deliver_sm_resp and any ESME-originated submit_sm.

Run from the Linux host where both simulator components are running:

```bash
scripts/interop-local.sh all
```

For `all` or `smsc` mode, the SMSC simulator must be running before the probe starts. For `all` or `esme` mode, keep the ESME simulator stopped until the runner prints `READY`; then start or restart it so it connects to the go-smpp listener. In `esme` mode, go-smpp acts as the SMSC on its configured listener, so no external SMSC simulator is needed. Each phase uses at most one connection, and the phases do not overlap. Use `scripts/interop-local.sh smsc` or `scripts/interop-local.sh esme` to run one direction only.

The ESME simulator is operator-controlled through its CLI; it does not originate `submit_sm` automatically. Do not issue the CLI submit command when `READY` appears. First allow the TRX bind, `enquire_link`, and synthetic `deliver_sm` / `deliver_sm_resp` exchange to complete. Only when the probe prints `OBSERVE`, use the ESME simulator CLI to send exactly one `submit_sm` within the displayed observation window. The probe observes this command; it does not generate it.

The ESME simulator used in this pilot does not support SMPP Unbind. Accordingly, the ESME/server phase does **not** send a server-initiated `unbind` by default: it records `unbind=UNSUPPORTED` with `operation=not_attempted`, then closes the local session. This informational status does not fail the core bind/message interoperability result and is not reported as an Unbind success. For a peer known to support Unbind, opt into the strict exchange with `SMPP_INTEROP_ATTEMPT_ESME_UNBIND=true` or `-attempt-esme-unbind=true`; a missing `unbind_resp` then remains a failure. The library's Unbind implementation and its independent protocol tests are unchanged.

The ESME/server probe also verifies **connection continuity**, not just that a syntactically valid `submit_sm` was observed somewhere on the listener. The `submit_sm` handler retains the session identity and the probe reports PASS only when the request arrived on the exact session that completed the initial TRX bind. A submit on a replacement TCP session is reported as FAIL (`submit_received_on_replacement_session`) and is not exported as a peer fixture. After `enquire_link`, `session_health_after_enquire_link` records the original session state, whether it is closed, whether the server still owns it, and a sanitized close-cause/network-error classification. If that session has already closed, `deliver_sm` is BLOCKED rather than attempted against a stale session. The probe closes the server and joins all active sessions before taking its final evidence snapshot, preventing late frames from being mixed into a candidate.

The script asks for each bind password using a hidden terminal prompt. For non-interactive use, the corresponding environment variables are `SMPP_INTEROP_SMSC_PASSWORD` and `SMPP_INTEROP_ESME_PASSWORD`; never put passwords in command-line arguments or committed config. The default System IDs are `client_0` and `smpp_client_0`, System Type is `smpp`, and the bind type is TRX. Endpoints, IDs, System Type, synthetic test addresses/text and operation timeouts can be overridden with the `SMPP_INTEROP_*` environment variables or probe flags. The ESME-originated `submit_sm` observation window defaults to 15 seconds and can be changed with `SMPP_INTEROP_SUBMIT_WAIT` (for example, `30s`). Server-initiated Unbind in the ESME phase is disabled by default for the known simulator limitation; enable it explicitly with `SMPP_INTEROP_ATTEMPT_ESME_UNBIND=true` or `-attempt-esme-unbind=true` only when the peer supports it.

The probe sends only synthetic test addresses/text. The SMSC phase deliberately sends registered_delivery octet `0x03`, which SMPP 3.4 reserves; the library's success-only label is a peer-specific interpretation, not a portable protocol meaning.

### Capture safety

The probe enables raw tracing only inside the opt-in local test process. Bind receiver/transmitter/transceiver and outbind frames are discarded before body decoding. Only a narrow command allowlist is decoded and re-encoded for export. Source/destination addresses and message IDs are replaced; message content is removed or replaced with a synthetic receipt; only a small receipt-TLV allowlist is retained with identifiers replaced; all other optional TLVs and unsupported commands are dropped. Malformed or ambiguous receipt metadata (including conflicting IDs/states) is excluded rather than normalized into a misleading fixture. Raw frames remain in memory only and are never written to the report, archive or ordinary logs.

The script writes a permission-restricted archive under the system temporary directory containing the sanitized report, sanitized fixture candidate and run log. New candidates use source `simulator-peer` and default to `sanitization.reviewed=false`; a maintainer must inspect the exported bytes before changing that declaration and adding them to the manifest. The first two candidates from the local run below have been reviewed and added. Simulator evidence is not deployed SMSC/ESME vendor evidence and does not close the deployed-peer compatibility matrix.

The tool is compiled only with the `interop` build tag, adds no public API or runtime dependency, and is tested in CI without opening external connections.

### First local simulator capture — 2026-11-07

The first two-way local run was reviewed from its sanitized archive. No passwords, bind frames, raw PDUs, original addresses, message IDs or message content were committed.

- **SMSC simulator (`peer-001`):** go-smpp connected as ESME, bound TRX, completed enquire_link, submitted a synthetic message with `registered_delivery=0x03`, received a successful `submit_sm_resp` with a message ID, and parsed the returned `DELIVRD` receipt. The subsequent Unbind request was captured, but no matching `unbind_resp` was observed before the 8-second operation deadline.
- **ESME simulator (`peer-002`):** go-smpp listened as SMSC, accepted a TRX bind, completed enquire_link, sent a synthetic delivery receipt, and received a successful `deliver_sm_resp`. No ESME-originated `submit_sm` was observed during the original 5-second observation window. The subsequent Unbind request was captured, but no matching `unbind_resp` was observed before the operation deadline.
- Both simulators therefore demonstrated the bind, enquire_link and message/DLR exchanges above, but did not complete the Unbind request/response handshake in this run. SMPP 3.4 permits either role to issue Unbind and defines the corresponding `unbind_resp` with the original sequence number (Specification Issue 1.2, Table 2-1 and §4.2.2). Treat this as an observed simulator interoperability gap, not a reason to weaken the library's Unbind behavior.
- The ESME simulator used in the latest local run was confirmed by the operator not to support Unbind. The probe now records this known peer capability as `UNSUPPORTED` and skips the server-initiated Unbind by default; this is distinct from PASS and does not fail the core TRX/message exchange. Strict Unbind observation remains opt-in for peers that implement it.
- `submit_sm_from_esme=NOT_OBSERVED` is not itself a failure. The original 5-second window elapsed without an outbound ESME submit action. The opt-in probe now provides a 15-second default window, configurable with `SMPP_INTEROP_SUBMIT_WAIT`, and prints a just-in-time prompt before waiting.



**ESME session-continuity investigation — 2026-11-08**

- Archive `go-smpp-interop.g7u649.tar.gz`: TRX bind passed; `enquire_link` failed with `*session.LossError` caused by a `*net.OpError` on write; `deliver_sm` returned `server.ErrUnknownSession` (formatted as `*errors.errorString`); an ESME `submit_sm` and matching response were captured.
- The server's `ErrUnknownSession` means the original session was no longer owned by the server when `DeliverSM` was called. The old probe's submit handler did not retain the session pointer, so the later submit could have arrived on a replacement connection. The report's frame count (1) also differed from the later candidate's two frames because the final snapshot happened before deferred server shutdown. Therefore this run does **not** prove that the initial TRX connection stayed open through submit.
- The initiating transport write error is not yet attributed to local close versus peer/OS transport failure. The new probe emits `session_health_after_enquire_link` with the original session's terminal cause and safe network-error class, correlates submit to the original session, blocks stale-session DeliverSM, and shuts down/joins the server before final capture. Library transport/session semantics are unchanged pending evidence.

The follow-up archive `go-smpp-interop.WgVM1N.tar.gz` recorded a read-side TCP `ECONNRESET`, followed by the original Session being closed/removed and a later Submit on a replacement Session. The archive contains no PCAP or TCP flags, so it cannot identify the reset sender. A read-side reset means the local TCP stack reported a reset while reading; the Session then closes the socket during terminal-error handling. The previous `peer_reset_connection` wording was too specific and is now reported as `tcp_reset_received`.

The build-tagged ESME probe now wraps accepted sockets with a metadata-only lifecycle trace. It records a per-connection ID, complete SMPP header metadata (direction, command ID/status, sequence number and PDU length), read/write error classes, and the order of local `Close` calls relative to prior I/O errors. Bind-response correlation is tied to the exact accepted socket and is emitted only after the complete response has been successfully written; Submit reports its connection ID as well as the Session identity check. The probe closes/joins active sessions and waits for the server accept loop to exit before taking the final trace snapshot. No PDU body, credentials, addresses, raw TCP bytes or PCAP are persisted. This instrumentation is test-only and does not change public library/session behavior.

Interpret the next trace conservatively:
- `tcp_read_error` with `network_error=connection_reset` before `tcp_local_close` means go-smpp closed the socket after the reset was observed; it does not prove which remote process/component sent the reset.
- `tcp_local_close` before the read error means a local close was initiated first; inspect the caller/shutdown path.
- A packet capture on loopback showing TCP RST source/destination ports is still required to attribute the reset to the remote TCP endpoint with packet-level certainty.

The reviewed sanitized exchanges are retained as `simulator-peer` fixtures `simulator-smsc-peer-001` and `simulator-esme-peer-002`. They validate observed wire forms and decode behavior only; they do not claim full peer conformance or deployed-vendor interoperability.

## Offline interoperability fixtures

The repository keeps peer interoperability evidence in a versioned, test-only manifest at `testdata/interop/manifest.json`. The fixture harness is part of the repository's external test package; it does not add an exported API, runtime dependency, or product-specific behavior.

Run the offline fixture suite with:

```bash
go test . -run '^TestInteroperabilityFixtures$' -count=1
```

The manifest uses schema version 1. Each fixture identifies its source (`synthetic`, `simulator-peer`, or `real-peer-anonymized`), an opaque peer alias, SMPP profile, capture method, and ordered frames. Each frame records its TCP direction, exact PDU bytes as hexadecimal, expected command/status/sequence/body type, decode outcome, and whether exact re-encoding is expected. Frames in opposite directions are replayed as separate TCP byte streams; deterministic chunk sizes exercise both fragmentation and coalescing.

Synthetic fixtures must use `peer_alias=synthetic` and `capture_method=constructed`. They are useful for validating the harness and protocol vectors only. **They are not evidence of interoperability with a deployed SMSC/ESME.**

A `simulator-peer` fixture must use `capture_method=wire-capture-sanitized`, an opaque alias such as `peer-001`, an RFC3339 capture timestamp, an opaque `INT-01-PEER-NNN-NNN` evidence reference, and a reviewed `peer-aware-allowlist-v1` sanitization declaration with bind/outbind exclusion, replaced sensitive values, and `unknown_tlvs=removed`. The local probe deliberately emits this candidate with `reviewed=false`; review it before adding it to the manifest.

A deployed real-peer fixture must use an opaque alias such as `peer-001`, an RFC3339 capture timestamp, an opaque `INT-01-PEER-NNN-NNN` evidence reference, and a reviewed `peer-aware-allowlist-v1` sanitization declaration. The declaration must attest that bind/outbind frames were excluded, sensitive values were replaced, and unknown TLVs were removed or individually reviewed safe. The test harness enforces these metadata requirements and rejects bind/outbind command IDs, but it cannot independently prove that a human sanitizer removed every secret; the maintainer must review the bytes before committing them.

### Capture and fixture safety

- Do not commit PCAPs, raw capture files, bind/outbind frames, passwords, system IDs, subscriber addresses, message IDs, message bodies, IP addresses, hostnames, account names, or identifying vendor/customer labels.
- Capture only in an authorized isolated test environment. Keep original raw evidence outside the repository in restricted storage and delete it according to the applicable retention policy.
- Before adding a real fixture, replace sensitive mandatory-field values with synthetic values while preserving valid C-Octet boundaries and lengths. Remove unknown/vendor TLVs unless their semantics and values have been reviewed safe; if a vendor extension is required for a test, retain only the minimum safe, anonymized bytes and record the peer-specific evidence reference.
- Never assume the fixture harness performs redaction. It validates declarations and protocol framing; it is not a sanitizer.
- Use `reencode=exact` only when the peer's observed wire form is expected to round-trip byte-for-byte. Use `reencode=decode-only` for an accepted peer form whose decode behavior is under test but whose canonical encoder output is intentionally different.
- Use `outcome=reject` with `error_kind=fatal|semantic|other` when the expected library behavior is to reject a recorded frame. A rejection is evidence about the library's handling of that frame, not proof that the peer itself is non-conformant.

The manifest contains the constructed synthetic SMPP 3.4 vectors plus the two reviewed `simulator-peer` exchanges described above. **INT-01 remains open until anonymized deployed-peer exchanges are supplied and the representative-peer strict/compatible behavior matrix is built.**


### ESME closes TCP before Bind Response — 2026-11-08

The sanitized archive `go-smpp-interop.wvS2vr.tar.gz` records the following:

- TCP connection 1 is accepted.
- The peer sends a complete 45-byte `bind_transceiver` request (`command_id=0x00000009`, sequence `1006632970`); the configured authenticator accepts the TRX credentials.
- Before a successful `bind_transceiver_resp` write is observed, the next server-side read returns EOF (`peer_closed_connection`).
- go-smpp then closes the original Session/socket. The local Close event records `prior_io_error=read:peer_closed_connection`.
- A second connection is accepted just after the EOF and locally closed about 38 microseconds later. The old trace did not identify whether this close was caused by probe cleanup or another path.
- A later write on connection 1 fails with `local_connection_closed` after its local Close. This is a secondary error and not the initiating event. No submit/deliver exchange occurred, no sanitized fixture was captured, and the candidate manifest is empty.

This differs from the earlier `ECONNRESET` case: the current evidence is an orderly EOF observed while reading. It proves the remote TCP peer closed its stream before the Bind Response was observed, but it does not explain why the ESME simulator did so. The report's `authenticator_result=accepted` means the request passed the probe's credential/mode checks; it does not mean the bind handshake completed.

The test-only probe now:
- marks local socket closes that occur after probe shutdown begins (`probe_shutdown_started=true`), to identify connection-2 cleanup;
- records only the attempted SMPP header (command ID/status/sequence/length) when a socket Write fails, without persisting the PDU body;
- retains the per-socket accepted/read/written/error/close event ordering.

For the next run, a `tcp_write_error` with `attempted_command_id=0x80000009` will confirm that the failed write was a `bind_transceiver_resp`; because the write error follows the read EOF and local Close, it remains a consequence of the already-closed Session. The `probe_shutdown_started` field should identify whether connection 2 was closed during early-return cleanup.

The interop probe uses distinct decimal-only IDs: `12345678901234567890` for the application-supplied `submit_sm_resp` and `9876543210987654321` for the unrelated synthetic DLR. The SMPP field remains a NUL-terminated C-Octet String. These are test fixture values, not a generic library restriction: applications supply `server.SubmitResult.MessageID`, and the generic DLR parser preserves external peer IDs as opaque strings.

To explain why the ESME closes immediately after Bind, collect the ESME simulator's local log around the bind attempt. If that is insufficient, capture only FIN/RST packet headers on loopback (no payload), because bind requests carry credentials. Do not export a raw PCAP or packet payload into the repository.


### Successful ESME/server repeat and graceful shutdown — 2026-11-08

The reviewed archive `go-smpp-interop.2KhVNK.tar.gz` confirms a complete single-session SMPP 3.4 exchange:

| Direction | Command | Sequence | Result |
|---|---|---:|---|
| ESME → SMSC | `bind_transceiver` | 2218786818 | received |
| SMSC → ESME | `bind_transceiver_resp` | 2218786818 | written, status 0 |
| SMSC → ESME | `enquire_link` | 1 | written |
| ESME → SMSC | `enquire_link_resp` | 1 | received |
| SMSC → ESME | `deliver_sm` | 2 | written |
| ESME → SMSC | `deliver_sm_resp` | 2 | received, status 0 |
| ESME → SMSC | `submit_sm` | 1912602627 | received on original connection |
| SMSC → ESME | `submit_sm_resp` | 1912602627 | written, status 0 |

All eight PDU headers are associated with `connection_id=1`. The bind request/response are excluded from the sanitized fixture; the remaining six frames are captured. The Submit observation reports `session_match=true`, so this is not a replacement-session exchange. The candidate fixture was manually reviewed and added separately as `simulator-esme-peer-002-submit` (`INT-01-PEER-002-002)); the earlier peer-002 fixture remains unchanged because it records a different run and the earlier strict Unbind attempt.

**Do not confuse Submit with Unbind:** SMPP command ID `0x00000004` is `submit_sm`, and `0x80000004` is `submit_sm_resp`. The last PDU pair in this successful trace is the ESME-originated Submit and its response. No Unbind PDU (`0x00000006`) or Unbind Response (`0x80000006`) appears in this archive.

The report's `unbind=UNSUPPORTED` event is a separate test-control result: because this ESME simulator does not support **server-initiated Unbind**, the probe did not send an Unbind from the SMSC to the ESME. It then closed the local session after the message observation completed. The event now explicitly identifies `operation_kind=server_initiated_unbind`, `initiator_role=smsc`, `peer_role=esme`, and `request_direction=smsc_to_esme`. `request_sent=false` and `response_observed=false` mean the operation was skipped, not that a peer response was lost. The optional `SMPP_INTEROP_ATTEMPT_ESME_UNBIND=true` switch is only for peers that support this server-initiated exchange.

The accompanying loopback capture used local time UTC+03:30. Its FIN exchange at `16:43:51.816897` (server port 4000 to client port 46662), followed by the peer FIN at `16:43:51.816946`, aligns with the report's `tcp_local_close` at `13:13:51.816872Z`. This is the expected orderly TCP shutdown after the probe finishes; there is no RST on this active connection. The other RST/ACK packets target different ephemeral ports. Their timing (before the listener starts or after it exits, plus a connection attempt immediately after shutdown) is consistent with connection attempts arriving while no listener is accepting; they are not evidence that the active connection 46662 was reset. The filtered capture omitted SYN packets and the trace does not record remote ports, so it cannot identify which local process made those separate attempts.

The earlier `wvS2vr` archive remains useful evidence of a different, non-reproduced event: the ESME peer sent Bind and then returned EOF before a Bind Response was observed; go-smpp closed only after that EOF. The successful repeat demonstrates that the same simulator pair can complete the handshake and message exchange. No public Session/Transport behavior change was made.

