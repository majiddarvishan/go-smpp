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

func TestParseDeliveryReceiptTextStatusMapping(t *testing.T) {
	states := map[string]uint8{"ENROUTE": 1, "DELIVRD": 2, "EXPIRED": 3, "DELETED": 4, "UNDELIV": 5, "ACCEPTD": 6, "UNKNOWN": 7, "REJECTD": 8}
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

func TestParseDeliveryReceiptRejectsAmbiguousTLVs(t *testing.T) {
	tags := make(map[string][]protocol.OptionalParameter)
	tags["empty ID"] = []protocol.OptionalParameter{receiptTLV(protocol.TLVTagReceiptedMessageID, nil)}
	tags["only NUL ID"] = []protocol.OptionalParameter{receiptTLV(protocol.TLVTagReceiptedMessageID, []byte{0})}
	tags["embedded NUL ID"] = []protocol.OptionalParameter{receiptTLV(protocol.TLVTagReceiptedMessageID, []byte{'A', 0, 'B', 0})}
	tags["conflicting IDs"] = []protocol.OptionalParameter{
		receiptTLV(protocol.TLVTagReceiptedMessageID, []byte("a")),
		receiptTLV(protocol.TLVTagReceiptedMessageID, []byte("b")),
	}
	tags["state length"] = []protocol.OptionalParameter{receiptTLV(protocol.TLVTagMessageState, []byte{1, 2})}
	tags["state zero"] = []protocol.OptionalParameter{receiptTLV(protocol.TLVTagMessageState, []byte{0})}
	tags["state nine"] = []protocol.OptionalParameter{receiptTLV(protocol.TLVTagMessageState, []byte{9})}
	tags["state conflict"] = []protocol.OptionalParameter{
		receiptTLV(protocol.TLVTagMessageState, []byte{2}),
		receiptTLV(protocol.TLVTagMessageState, []byte{5}),
	}
	tags["payload conflict"] = []protocol.OptionalParameter{
		receiptTLV(protocol.TLVTagMessagePayload, []byte("id:a")),
		receiptTLV(protocol.TLVTagMessagePayload, []byte("id:b")),
	}
	for name, tlvs := range tags {
		t.Run(name, func(t *testing.T) {
			_, err := ParseDeliveryReceipt(protocol.DeliverSM{Optional: tlvs})
			if !errors.Is(err, ErrInvalidDeliveryReceipt) {
				t.Fatalf("want invalid DLR, got %v", err)
			}
		})
	}
}

func TestParseDeliveryReceiptRejectsInvalidTextAndMixedCarriers(t *testing.T) {
	for _, body := range []string{
		"id:A id:B stat:DELIVRD",
		"id:A stat:DELIVRD stat:EXPIRED",
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
