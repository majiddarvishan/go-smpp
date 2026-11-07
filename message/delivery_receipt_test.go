package message

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/majiddarvishan/go-smpp/protocol"
)

func receiptTLV(tag uint16, value []byte) protocol.OptionalParameter {
	return protocol.OptionalParameter{Tag: tag, Value: value}
}

func TestParseDeliveryReceiptPrecedenceAndConflicts(t *testing.T) {
	pdu := protocol.DeliverSM{
		Optional: []protocol.OptionalParameter{
			receiptTLV(protocol.TLVTagReceiptedMessageID, []byte("000abc\x00")),
			receiptTLV(protocol.TLVTagMessageState, []byte{5}),
		},
	}
	pdu.ESMClass = 0x04
	pdu.ShortMessage = []byte("id:000AbC sub:001 dlvrd:001 stat:DELIVRD err:000 text:hi id:FAKE stat:REJECTD")
	got, err := ParseDeliveryReceipt(pdu)
	if err != nil {
		t.Fatal(err)
	}
	if got.MessageID != "000AbC" || got.MessageIDSource != ReceiptSourceText ||
		got.TextMessageID != "000AbC" || got.TLVMessageID != "000abc" || !got.MessageIDConflict {
		t.Fatalf("text-first opaque ID mismatch: %+v", got)
	}
	if !got.HasState || got.State != 5 || got.StateSource != ReceiptSourceTLV ||
		got.TextState != 2 || !got.HasTextState || !got.HasTLVState || !got.StateConflict {
		t.Fatalf("TLV-first state mismatch: %+v", got)
	}
	state, ok := got.SMPPMessageState()
	if !ok || state != protocol.MessageStateUndeliverable {
		t.Fatalf("typed state = %v, %v", state, ok)
	}
}

func TestParseDeliveryReceiptStandardTextFields(t *testing.T) {
	got, err := ParseDeliveryReceipt(protocol.DeliverSM{
		ESMClass:     0x04,
		ShortMessage: []byte("id:000AbC sub:001 dlvrd:000 submit date:2411070900 done date:2411070901 stat:UNDELIV err:005 text:destination unavailable"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.MessageID != "000AbC" || got.SubmittedCount != "001" || got.DeliveredCount != "000" ||
		got.SubmitDate != "2411070900" || got.DoneDate != "2411070901" ||
		got.TextStatus != "UNDELIV" || got.ErrorCode != "005" || got.Text != "destination unavailable" {
		t.Fatalf("standard receipt fields not preserved: %+v", got)
	}
}

func TestParseDeliveryReceiptSplitStandardFieldValues(t *testing.T) {
	got, err := ParseDeliveryReceipt(protocol.DeliverSM{
		ESMClass:     0x04,
		ShortMessage: []byte("ID: ABC123 SUB: 002 DLVRD: 001 SUBMIT DATE: 2411070900 DONE DATE: 2411070901 STAT: DELIVRD ERR: 000 TEXT: delivery complete for 123"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.MessageID != "ABC123" || got.SubmittedCount != "002" || got.DeliveredCount != "001" ||
		got.SubmitDate != "2411070900" || got.DoneDate != "2411070901" ||
		got.TextStatus != "DELIVRD" || got.ErrorCode != "000" || got.Text != "delivery complete for 123" {
		t.Fatalf("split receipt fields not parsed: %+v", got)
	}
}

func TestParseDeliveryReceiptTextStatusMapping(t *testing.T) {
	states := map[string]uint8{
		"ENROUTE": 1, "DELIVRD": 2, "EXPIRED": 3, "DELETED": 4,
		"UNDELIV": 5, "ACCEPTD": 6, "UNKNOWN": 7, "REJECTD": 8,
	}
	for name, want := range states {
		t.Run(name, func(t *testing.T) {
			got, err := ParseDeliveryReceipt(protocol.DeliverSM{
				ESMClass: 0x04, ShortMessage: []byte("ID:000123 STAT:" + strings.ToLower(name)),
			})
			if err != nil {
				t.Fatal(err)
			}
			if got.MessageID != "000123" || !got.HasState || got.State != want ||
				got.StateSource != ReceiptSourceText {
				t.Fatalf("status %s: %+v", name, got)
			}
			state, ok := got.SMPPMessageState()
			if !ok || uint8(state) != want {
				t.Fatalf("typed state %v, %v; want %d", state, ok, want)
			}
		})
	}
	got, err := ParseDeliveryReceipt(protocol.DeliverSM{
		ESMClass: 0x04, ShortMessage: []byte("id:ABC123 stat:SCHEDULED text:ignored"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.TextStatus != "SCHEDULED" || got.HasTextState || got.HasState {
		t.Fatalf("unknown vendor state was guessed: %+v", got)
	}
	if _, ok := got.SMPPMessageState(); ok {
		t.Fatal("unknown text status produced a standard message state")
	}
	if _, ok := (DeliveryReceipt{}).SMPPMessageState(); ok {
		t.Fatal("zero receipt produced a standard message state")
	}
}

func TestParseDeliveryReceiptStandardTLVs(t *testing.T) {
	got, err := ParseDeliveryReceipt(protocol.DeliverSM{Optional: []protocol.OptionalParameter{
		receiptTLV(protocol.TLVTagReceiptedMessageID, []byte("TLV-42\x00")),
		receiptTLV(protocol.TLVTagMessageState, []byte{5}),
		receiptTLV(protocol.TLVTagNetworkErrorCode, []byte{1, 2, 3}),
		receiptTLV(protocol.TLVTagDeliveryFailureReason, []byte{0x0b}),
		receiptTLV(protocol.TLVTagAdditionalStatusInfoText, []byte("route failed\x00")),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got.MessageID != "TLV-42" || got.MessageIDSource != ReceiptSourceTLV ||
		got.State != 5 || got.StateSource != ReceiptSourceTLV {
		t.Fatalf("receipt identity/state: %+v", got)
	}
	if !got.HasNetworkErrorCode || got.NetworkErrorCode != [3]byte{1, 2, 3} {
		t.Fatalf("network_error_code: %+v", got)
	}
	if !got.HasDeliveryFailureReason || got.DeliveryFailureReason != 0x0b {
		t.Fatalf("delivery_failure_reason: %+v", got)
	}
	if !got.HasAdditionalStatusInfoText || got.AdditionalStatusInfoText != "route failed" {
		t.Fatalf("additional_status_info_text: %+v", got)
	}
}

func TestParseDeliveryReceiptResponseOnlyTLVsDoNotIdentifyReceipt(t *testing.T) {
	cases := map[string][]protocol.OptionalParameter{
		"delivery_failure_reason": {
			receiptTLV(protocol.TLVTagDeliveryFailureReason, []byte{2}),
		},
		"additional_status_info_text": {
			receiptTLV(protocol.TLVTagAdditionalStatusInfoText, []byte("diagnostic\x00")),
		},
		"both response-only TLVs": {
			receiptTLV(protocol.TLVTagDeliveryFailureReason, []byte{2}),
			receiptTLV(protocol.TLVTagAdditionalStatusInfoText, []byte("diagnostic\x00")),
		},
	}
	for name, tlvs := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseDeliveryReceipt(protocol.DeliverSM{Optional: tlvs})
			if !errors.Is(err, ErrNotDeliveryReceipt) {
				t.Fatalf("response-only TLVs identified a delivery receipt: %v", err)
			}
		})
	}
}

func TestParseDeliveryReceiptIdenticalStandardTLVDuplicates(t *testing.T) {
	got, err := ParseDeliveryReceipt(protocol.DeliverSM{Optional: []protocol.OptionalParameter{
		receiptTLV(protocol.TLVTagNetworkErrorCode, []byte{1, 2, 3}),
		receiptTLV(protocol.TLVTagNetworkErrorCode, []byte{1, 2, 3}),
		receiptTLV(protocol.TLVTagDeliveryFailureReason, []byte{7}),
		receiptTLV(protocol.TLVTagDeliveryFailureReason, []byte{7}),
		receiptTLV(protocol.TLVTagAdditionalStatusInfoText, []byte{0}),
		receiptTLV(protocol.TLVTagAdditionalStatusInfoText, []byte{0}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !got.HasNetworkErrorCode || !got.HasDeliveryFailureReason || !got.HasAdditionalStatusInfoText ||
		got.AdditionalStatusInfoText != "" {
		t.Fatalf("identical/empty optional TLVs not preserved: %+v", got)
	}
}

func TestParseDeliveryReceiptRejectsAmbiguousStandardTLVs(t *testing.T) {
	cases := map[string][]protocol.OptionalParameter{
		"receipt ID missing terminator": {
			receiptTLV(protocol.TLVTagReceiptedMessageID, []byte("missing-terminator")),
		},
		"receipt ID embedded NUL": {
			receiptTLV(protocol.TLVTagReceiptedMessageID, []byte{'a', 0, 'b', 0}),
		},
		"receipt ID too long": {
			receiptTLV(protocol.TLVTagReceiptedMessageID, append(bytes.Repeat([]byte{'a'}, 65), 0)),
		},
		"receipt ID conflicting duplicate": {
			receiptTLV(protocol.TLVTagReceiptedMessageID, []byte("one\x00")),
			receiptTLV(protocol.TLVTagReceiptedMessageID, []byte("two\x00")),
		},
		"message state conflicting duplicate": {
			receiptTLV(protocol.TLVTagMessageState, []byte{1}),
			receiptTLV(protocol.TLVTagMessageState, []byte{2}),
		},
		"network error wrong length": {
			receiptTLV(protocol.TLVTagNetworkErrorCode, []byte{1, 2}),
		},
		"network error conflict": {
			receiptTLV(protocol.TLVTagNetworkErrorCode, []byte{1, 2, 3}),
			receiptTLV(protocol.TLVTagNetworkErrorCode, []byte{1, 2, 4}),
		},
		"delivery failure wrong length": {
			receiptTLV(protocol.TLVTagDeliveryFailureReason, []byte{1, 2}),
		},
		"delivery failure conflict": {
			receiptTLV(protocol.TLVTagDeliveryFailureReason, []byte{1}),
			receiptTLV(protocol.TLVTagDeliveryFailureReason, []byte{2}),
		},
		"additional status empty TLV": {
			receiptTLV(protocol.TLVTagAdditionalStatusInfoText, nil),
		},
		"additional status missing terminator": {
			receiptTLV(protocol.TLVTagAdditionalStatusInfoText, []byte("unterminated")),
		},
		"additional status too long": {
			receiptTLV(protocol.TLVTagAdditionalStatusInfoText, append(bytes.Repeat([]byte{'s'}, 256), 0)),
		},
		"additional status embedded NUL": {
			receiptTLV(protocol.TLVTagAdditionalStatusInfoText, []byte{'a', 0, 'b', 0}),
		},
		"additional status conflict": {
			receiptTLV(protocol.TLVTagAdditionalStatusInfoText, []byte("one\\x00")),
			receiptTLV(protocol.TLVTagAdditionalStatusInfoText, []byte("two\\x00")),
		},
	}
	for name, tlvs := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseDeliveryReceipt(protocol.DeliverSM{Optional: tlvs})
			if !errors.Is(err, ErrInvalidDeliveryReceipt) {
				t.Fatalf("want invalid receipt, got %v", err)
			}
		})
	}
}

func TestParseDeliveryReceiptTLVOnlyAndDuplicates(t *testing.T) {
	pdu := protocol.DeliverSM{Optional: []protocol.OptionalParameter{
		receiptTLV(protocol.TLVTagReceiptedMessageID, []byte("00A39f\x00")),
		receiptTLV(protocol.TLVTagReceiptedMessageID, []byte("00A39f\x00")),
		receiptTLV(protocol.TLVTagMessageState, []byte{2}),
		receiptTLV(protocol.TLVTagMessageState, []byte{2}),
	}}
	got, err := ParseDeliveryReceipt(pdu)
	if err != nil {
		t.Fatal(err)
	}
	if got.MessageID != "00A39f" || got.MessageIDSource != ReceiptSourceTLV ||
		!got.HasState || got.State != 2 || got.StateSource != ReceiptSourceTLV ||
		got.MessageIDConflict || got.StateConflict {
		t.Fatalf("TLV-only DLR: %+v", got)
	}
}

func TestParseDeliveryReceiptPayloadAndUCS2(t *testing.T) {
	fromPayload := protocol.DeliverSM{Optional: []protocol.OptionalParameter{
		receiptTLV(protocol.TLVTagMessagePayload, []byte("id:payload-001 stat:EXPIRED")),
	}}
	got, err := ParseDeliveryReceipt(fromPayload)
	if err != nil || got.MessageID != "payload-001" || !got.HasState || got.State != 3 {
		t.Fatalf("message_payload: %+v err=%v", got, err)
	}
	text := "id:UCS2-42 stat:DELIVRD"
	ucs2 := make([]byte, 0, len(text)*2)
	for i := 0; i < len(text); i++ {
		ucs2 = append(ucs2, 0, text[i])
	}
	got, err = ParseDeliveryReceipt(protocol.DeliverSM{
		DataCoding: protocol.DataCodingUCS2, ShortMessage: ucs2,
	})
	if err != nil || got.MessageID != "UCS2-42" || got.State != 2 {
		t.Fatalf("UCS2: %+v err=%v", got, err)
	}
}

func TestParseDeliveryReceiptIA5AndLatin1(t *testing.T) {
	ia5, err := ParseDeliveryReceipt(protocol.DeliverSM{
		ESMClass: 0x04, DataCoding: protocol.DataCodingIA5,
		ShortMessage: []byte("id:IA5-1 stat:DELIVRD"),
	})
	if err != nil || ia5.MessageID != "IA5-1" || ia5.State != 2 {
		t.Fatalf("IA5 receipt: %+v err=%v", ia5, err)
	}

	latin1, err := ParseDeliveryReceipt(protocol.DeliverSM{
		ESMClass: 0x04, DataCoding: protocol.DataCodingLatin1,
		ShortMessage: []byte{'i', 'd', ':', 'c', 'a', 'f', 0xe9, ' ', 's', 't', 'a', 't', ':', 'D', 'E', 'L', 'I', 'V', 'R', 'D'},
	})
	if err != nil || latin1.MessageID != "café" || latin1.State != 2 {
		t.Fatalf("Latin-1 receipt: %+v err=%v", latin1, err)
	}
}

func TestParseDeliveryReceiptUnsupportedAndInvalidDataCoding(t *testing.T) {
	_, err := ParseDeliveryReceipt(protocol.DeliverSM{
		ESMClass: 0x04, DataCoding: protocol.DataCodingJIS,
		ShortMessage: []byte("id:unknown stat:DELIVRD"),
	})
	if !errors.Is(err, ErrUnsupportedDeliveryReceiptDataCoding) {
		t.Fatalf("unsupported DCS error = %v", err)
	}

	_, err = ParseDeliveryReceipt(protocol.DeliverSM{
		DataCoding: protocol.DataCodingJIS, ShortMessage: []byte("ordinary message"),
	})
	if !errors.Is(err, ErrNotDeliveryReceipt) {
		t.Fatalf("ordinary unsupported-DCS message error = %v", err)
	}

	_, err = ParseDeliveryReceipt(protocol.DeliverSM{
		ESMClass: 0x04, DataCoding: protocol.DataCodingIA5,
		ShortMessage: []byte{'i', 'd', ':', 'x', 0xff},
	})
	if !errors.Is(err, ErrInvalidDeliveryReceipt) {
		t.Fatalf("invalid IA5 error = %v", err)
	}
}

func TestParseDeliveryReceiptRejectsTextConflictsAndMixedCarriers(t *testing.T) {
	for _, body := range []string{
		"id:A id:B stat:DELIVRD",
		"id:A stat:DELIVRD stat:EXPIRED",
		"sub:001 sub:002 id:A stat:DELIVRD",
		"submit date:240101 submit date:240102 id:A stat:DELIVRD",
	} {
		_, err := ParseDeliveryReceipt(protocol.DeliverSM{ShortMessage: []byte(body)})
		if !errors.Is(err, ErrInvalidDeliveryReceipt) {
			t.Fatalf("body %q: %v", body, err)
		}
	}
	dlr := protocol.DeliverSM{ShortMessage: []byte("id:a stat:DELIVRD")}
	dlr.Optional = []protocol.OptionalParameter{
		receiptTLV(protocol.TLVTagMessagePayload, []byte("id:b stat:REJECTD")),
	}
	_, err := ParseDeliveryReceipt(dlr)
	if !errors.Is(err, ErrInvalidDeliveryReceipt) {
		t.Fatalf("mixed carriers: %v", err)
	}
	_, err = ParseDeliveryReceipt(protocol.DeliverSM{
		DataCoding: protocol.DataCodingUCS2, ShortMessage: []byte{0},
	})
	if !errors.Is(err, ErrInvalidDeliveryReceipt) {
		t.Fatalf("bad UCS2: %v", err)
	}
}

func TestParseDeliveryReceiptOrdinaryMessageAndEchoedText(t *testing.T) {
	for _, body := range []string{
		"hello world",
		"your id:ABC is ready",
		"text:hello id:ABC stat:DELIVRD",
	} {
		_, err := ParseDeliveryReceipt(protocol.DeliverSM{ShortMessage: []byte(body)})
		if !errors.Is(err, ErrNotDeliveryReceipt) {
			t.Fatalf("ordinary SMS %q: %v", body, err)
		}
	}
	_, err := ParseDeliveryReceipt(protocol.DeliverSM{})
	if !errors.Is(err, ErrNotDeliveryReceipt) {
		t.Fatalf("zero PDU: %v", err)
	}
}

func TestParseDeliveryReceiptPreservesTextPayloadAndOpaqueColonID(t *testing.T) {
	wantText := " first  line\tsecond  "
	got, err := ParseDeliveryReceipt(protocol.DeliverSM{
		ESMClass:     0x04,
		ShortMessage: []byte("id: SMSC:00:AB stat: DELIVRD text: " + wantText),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.MessageID != "SMSC:00:AB" {
		t.Fatalf("opaque ID containing colons = %q", got.MessageID)
	}
	if got.Text != wantText {
		t.Fatalf("text payload whitespace changed: got %q, want %q", got.Text, wantText)
	}
}

func TestParseDeliveryReceiptDoesNotTreatStatusOnlyTextAsReceipt(t *testing.T) {
	_, err := ParseDeliveryReceipt(protocol.DeliverSM{
		ShortMessage: []byte("stat:DELIVRD"),
	})
	if !errors.Is(err, ErrNotDeliveryReceipt) {
		t.Fatalf("status-only ordinary text error = %v", err)
	}

	got, err := ParseDeliveryReceipt(protocol.DeliverSM{
		ESMClass:     0x04,
		ShortMessage: []byte("stat:DELIVRD"),
	})
	if err != nil || !got.HasState || got.State != uint8(protocol.MessageStateDelivered) {
		t.Fatalf("marked status-only receipt = %+v err=%v", got, err)
	}
}

func TestParseDeliveryReceiptMessagePayloadDuplicates(t *testing.T) {
	payload := []byte("id:payload-1 stat:DELIVRD")
	got, err := ParseDeliveryReceipt(protocol.DeliverSM{Optional: []protocol.OptionalParameter{
		receiptTLV(protocol.TLVTagMessagePayload, payload),
		receiptTLV(protocol.TLVTagMessagePayload, payload),
	}})
	if err != nil || got.MessageID != "payload-1" || got.State != uint8(protocol.MessageStateDelivered) {
		t.Fatalf("identical message_payload duplicates: %+v err=%v", got, err)
	}

	_, err = ParseDeliveryReceipt(protocol.DeliverSM{Optional: []protocol.OptionalParameter{
		receiptTLV(protocol.TLVTagMessagePayload, payload),
		receiptTLV(protocol.TLVTagMessagePayload, []byte("id:payload-2 stat:DELIVRD")),
	}})
	if !errors.Is(err, ErrInvalidDeliveryReceipt) {
		t.Fatalf("conflicting message_payload duplicates error = %v", err)
	}
}

func TestParseDeliveryReceiptCOctetTLVMaximumLengthsAndOpaqueBytes(t *testing.T) {
	id := append(bytes.Repeat([]byte{'i'}, 64), 0)
	status := append(bytes.Repeat([]byte{'s'}, 255), 0)
	got, err := ParseDeliveryReceipt(protocol.DeliverSM{Optional: []protocol.OptionalParameter{
		receiptTLV(protocol.TLVTagReceiptedMessageID, id),
		receiptTLV(protocol.TLVTagAdditionalStatusInfoText, status),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.TLVMessageID) != 64 || len(got.AdditionalStatusInfoText) != 255 {
		t.Fatalf("maximum C-Octet values not preserved: id=%d additional=%d", len(got.TLVMessageID), len(got.AdditionalStatusInfoText))
	}

	opaqueID := string([]byte{0x80})
	got, err = ParseDeliveryReceipt(protocol.DeliverSM{Optional: []protocol.OptionalParameter{
		receiptTLV(protocol.TLVTagReceiptedMessageID, []byte{0x80, 0}),
	}})
	if err != nil || got.TLVMessageID != opaqueID {
		t.Fatalf("opaque non-UTF8 ID was not preserved: id=%q err=%v", got.TLVMessageID, err)
	}
}

func FuzzParseDeliveryReceipt(f *testing.F) {
	f.Add([]byte("id:ABC123 stat:DELIVRD text:hello"), uint8(0), uint8(0x04), uint16(0), []byte(nil), uint16(0), []byte(nil))
	f.Add([]byte(nil), uint8(protocol.DataCodingUCS2), uint8(0), uint16(protocol.TLVTagReceiptedMessageID), []byte("ABC123\x00"), uint16(protocol.TLVTagMessageState), []byte{2})
	f.Add([]byte{0xff, 0x00, 0x80}, uint8(protocol.DataCodingIA5), uint8(0x04), uint16(protocol.TLVTagAdditionalStatusInfoText), []byte{0x80, 0}, uint16(0), []byte(nil))

	f.Fuzz(func(t *testing.T, shortMessage []byte, dataCoding uint8, esmClass uint8, tag1 uint16, value1 []byte, tag2 uint16, value2 []byte) {
		pdu := protocol.DeliverSM{
			DataCoding:   protocol.DataCoding(dataCoding),
			ESMClass:     esmClass,
			ShortMessage: shortMessage,
		}
		if tag1 != 0 {
			pdu.Optional = append(pdu.Optional, receiptTLV(tag1, value1))
		}
		if tag2 != 0 {
			pdu.Optional = append(pdu.Optional, receiptTLV(tag2, value2))
		}
		_, _ = ParseDeliveryReceipt(pdu)
	})
}
