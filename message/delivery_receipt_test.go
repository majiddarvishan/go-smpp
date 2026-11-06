package message

import (
	"errors"
	"strings"
	"testing"

	"github.com/majiddarvishan/go-smpp/protocol"
)

func TestParseDeliveryReceiptTextIDFirstAndTLVStateFirst(t *testing.T) {
	dlr := protocol.DeliverSM{
		ESMClass: 0x04,
		ShortMessage: []byte("id:000AbC sub:001 dlvrd:001 submit date:2411070000 done date:2411070001 stat:DELIVRD err:000 text:hi id:FAKE stat:REJECTD"),
		Optional: []protocol.OptionalParameter{
			{Tag: protocol.TLVTagReceiptedMessageID, Value: []byte("000abc\x00")},
			{Tag: protocol.TLVTagMessageState, Value: []byte{5}},
		},
	}
	got, err := ParseDeliveryReceipt(dlr)
	if err != nil {
		t.Fatal(err)
	}
	if got.MessageID != "000AbC" || got.MessageIDSource != ReceiptSourceText {
		t.Fatalf("text ID priority not respected: %+v", got)
	}
	if got.TLVMessageID != "000abc" || !got.MessageIDConflict || got.TextMessageID != "000AbC" {
		t.Fatalf("case-sensitive ID conflict lost: %+v", got)
	}
	if !got.HasState || got.State != 5 || got.StateSource != ReceiptSourceTLV {
		t.Fatalf("TLV state priority not respected: %+v", got)
	}
	if got.TextStatus != "DELIVRD" || got.TextState != 2 || !got.HasTextState || !got.StateConflict {
		t.Fatalf("text state and disagreement not preserved: %+v", got)
	}
}

func TestParseDeliveryReceiptTLVOnlyWithoutNUL(t *testing.T) {
	dlr := protocol.DeliverSM{Optional: []protocol.OptionalParameter{
		{Tag: protocol.TLVTagReceiptedMessageID, Value: []byte("00A39f")},
		{Tag: protocol.TLVTagMessageState, Value: []byte{2}},
	}}
	got, err := ParseDeliveryReceipt(dlr)
	if err != nil {
		t.Fatal(err)
	}
	if got.MessageID != "00A39f" || got.MessageIDSource != ReceiptSourceTLV || !got.HasState || got.State != 2 || got.StateSource != ReceiptSourceTLV || got.MessageIDConflict {
		t.Fatalf("TLV-only receipt decoded incorrectly: %+v", got)
	}
}

func TestParseDeliveryReceiptTextOnlyStates(t *testing.T) {
	cases := []struct {
		text string
		state uint8
	}{
		{"ENROUTE", 1}, {"DELIVRD", 2}, {"EXPIRED", 3},
		{"DELETED", 4}, {"UNDELIV", 5}, {"ACCEPTD", 6},
		{"UNKNOWN", 7}, {"REJECTD", 8},
	}
	for _, tt := range cases {
		t.Run(tt.text, func(t *testing.T) {
			got, err := ParseDeliveryReceipt(protocol.DeliverSM{
				ESMClass: 0x04, ShortMessage: []byte("ID:000123 STAT:" + strings.ToLower(tt.text) + " text: hi"),
			})
			if err != nil {
				t.Fatal(err)
			}
			if got.MessageID != "000123" || !got.HasState || got.State != tt.state || got.StateSource != ReceiptSourceText {
				t.Fatalf("%s result=%+v", tt.text, got)
			}
		})
	}
}

func TestParseDeliveryReceiptUnknownTextStateIsPreserved(t *testing.T) {
	got, err := ParseDeliveryReceipt(protocol.DeliverSM{
		ESMClass: 0x04, ShortMessage: []byte("id:ABC123 stat:SCHEDULED text:hello"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.MessageID != "ABC123" || got.TextStatus != "SCHEDULED" || got.HasTextState || got.HasState || got.StateSource != ReceiptSourceNone {
		t.Fatalf("invented state for unknown vendor status: %+v", got)
	}
}

func TestParseDeliveryReceiptPayloadAndUCS2(t *testing.T) {
	for _, tt := range []struct{
		name string
		dlr protocol.DeliverSM
	}{
		{
			name: "message_payload",
			dlr: protocol.DeliverSM{Optional: []protocol.OptionalParameter{
				{Tag: protocol.TLVTagMessagePayload, Value: []byte("id:payload-001 stat:EXPIRED")},
			}},
		},
		{
			name: "ucs2",
			dlr: protocol.DeliverSM{DataCoding: protocol.DataCodingUCS2, ShortMessage: ucs2ASCII("id:UCS2-42 stat:DELIVRD")},
		},
	} {
		t.Run(tt.name,func(t *testing.T) {
			got, err := ParseDeliveryReceipt(tt.dlr)
			if err != nil {
				t.Fatal(err)
			}
			if got.MessageID == "" || !got.HasState || got.StateSource != ReceiptSourceText {
				t.Fatalf("invalid parsed DLR: %+v", got)
			}
		})
	}
}

func ucs2ASCII(s string) []byte {
	out:=make([]byte, 0, len(s)*2)
	for i:=0;i<len(s);i++ {
		out=append(out,0,s[i])
	}
	return out
}

func TestParseDeliveryReceiptRejectsConflictingAndMalformedTLVs(t *testing.T) {
	cases := []struct{
		name string
		tlvs []protocol.OptionalParameter
	}{
		{"empty-id", []protocol.OptionalParameter{{Tag: protocol.TLVTagReceiptedMessageID, Value: nil}}},
		{"only-null-id", []protocol.OptionalParameter{{Tag: protocol.TLVTagReceiptedMessageID, Value: []byte{0}}}},
		{"embedded-null-id", []protocol.OptionalParameter{{Tag: protocol.TLVTagReceiptedMessageID, Value: []byte{'A',0,'B',0}}}},
		{"conflicting-id", []protocol.OptionalParameter{{Tag: protocol.TLVTagReceiptedMessageID, Value: []byte("a")},{Tag:protocol.TLVTagReceiptedMessageID,Value:[]byte("b")}}},
		{"wrong-state-length", []protocol.OptionalParameter{{Tag:protocol.TLVTagMessageState,Value:[]byte{1,2}}}},
		{"state-0", []protocol.OptionalParameter{{Tag:protocol.TLVTagMessageState,Value:[]byte{0}}}},
		{"state-9", []protocol.OptionalParameter{{Tag:protocol.TLVTagMessageState,Value:[]byte{9}}}},
		{"conflicting-state", []protocol.OptionalParameter{{Tag:protocol.TLVTagMessageState,Value:[]byte{2}},{Tag:protocol.TLVTagMessageState,Value:[]byte{5}}}},
		{"conflicting-payload", []protocol.OptionalParameter{{Tag:protocol.TLVTagMessagePayload,Value:[]byte("id:a")},{Tag:protocol.TLVTagMessagePayload,Value:[]byte("id:b")}}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T){
			_, err:=ParseDeliveryReceipt(protocol.DeliverSM{Optional:tt.tlvs})
			if !errors.Is(err,ErrInvalidDeliveryReceipt){
				t.Fatalf("err=%v; want ErrInvalidDeliveryReceipt",err)
			}
		})
	}
}

func TestParseDeliveryReceiptRejectsConflictingTextAndPayloadCarriers(t *testing.T) {
	for _, value := range []string{
		"id:A id:B stat:DELIVRD",
		"id:A stat:DELIVRD stat:EXPIRED",
	}{
		_,err:=ParseDeliveryReceipt(protocol.DeliverSM{ShortMessage:[]byte(value)})
		if !errors.Is(err,ErrInvalidDeliveryReceipt){t.Fatalf("%q: %v",value,err)}
	}
	_,err:=ParseDeliveryReceipt(protocol.DeliverSM{
		ShortMessage:[]byte("id:a stat:DELIVRD"),
		Optional:[]protocol.OptionalParameter{{Tag:protocol.TLVTagMessagePayload,Value:[]byte("id:b stat:REJECTD")}},
	})
	if !errors.Is(err,ErrInvalidDeliveryReceipt){t.Fatalf("two payload carriers: %v",err)}
	_,err=ParseDeliveryReceipt(protocol.DeliverSM{DataCoding:protocol.DataCodingUCS2,ShortMessage:[]byte{0x00}})
	if !errors.Is(err,ErrInvalidDeliveryReceipt){t.Fatalf("invalid UCS2 text: %v",err)}
}

func TestParseDeliveryReceiptNonReceiptAndEchoedTextAreNotMatched(t *testing.T) {
	for _, dlr:=range []protocol.DeliverSM{
		{},
		{ShortMessage:[]byte("hello world")},
		{ShortMessage:[]byte("text:hello id:ABC stat:DELIVRD")},
	} {
		_,err:=ParseDeliveryReceipt(dlr)
		if !errors.Is(err,ErrNotDeliveryReceipt){
			t.Fatalf("not-a-DLR %+v: %v",dlr,err)
		}
	}
}

func TestParseDeliveryReceiptDuplicateIdenticalTLVs(t *testing.T) {
	got,err:=ParseDeliveryReceipt(protocol.DeliverSM{
		ShortMessage:[]byte("id:ABC stat:DELIVRD"),
		Optional:[]protocol.OptionalParameter{
			{Tag:protocol.TLVTagReceiptedMessageID, Value:[]byte("ABC\x00")},
			{Tag:protocol.TLVTagReceiptedMessageID, Value:[]byte("ABC")},
			{Tag:protocol.TLVTagMessageState,Value:[]byte{2}},
			{Tag:protocol.TLVTagMessageState,Value:[]byte{2}},
		},
	})
	if err!=nil || got.MessageIDConflict || got.StateConflict || got.State!=2{
		t.Fatalf("identical duplicate TLVs: %+v err=%v",got,err)
	}
}
