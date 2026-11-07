package message

import (
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
		ESMClass: 0x04,
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
		ESMClass: 0x04,
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
		"additional status embedded NUL": {
			receiptTLV(protocol.TLVTagAdditionalStatusInfoText, []byte{'a', 0, 'b', 0}),
		},
		"additional status conflict": {
			receiptTLV(protocol.TLVTagAdditionalStatusInfoText, []byte("one")),
			receiptTLV(protocol.TLVTagAdditionalStatusInfoText, []byte("two")),
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
		receiptTLV(protocol.TLVTagReceiptedMessageID, []byte("00A39f")),
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
