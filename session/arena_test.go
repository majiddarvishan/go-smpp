package session

import (
	"testing"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
)

// TestOwnDecodedPDUDoesNotAliasSourceBuffer is the direct proof for the
// acceptance criterion "no borrowed slice outlives dispatch": build a
// SubmitSMResp whose MessageID and TLV values are views into one shared
// scratch buffer (standing in for the framer's reused read buffer), call
// ownDecodedPDU, then overwrite every byte of that scratch buffer — exactly
// what happens to the real read buffer once rxLoop's next Read lands. If the
// clone aliased the source instead of owning independent memory, every
// assertion below would see 'X' instead of the original text.
func TestOwnDecodedPDUDoesNotAliasSourceBuffer(t *testing.T) {
	const messageIDText = "borrowed-message-id"
	const tlv1Text = "borrowed-tlv-one"
	const tlv2Text = "borrowed-tlv-two"

	scratch := []byte(messageIDText + tlv1Text + tlv2Text)
	messageID := scratch[0:len(messageIDText)]
	tlv1 := scratch[len(messageIDText) : len(messageIDText)+len(tlv1Text)]
	tlv2 := scratch[len(messageIDText)+len(tlv1Text):]

	pdu := codec.DecodedPDU{
		Header: codec.Header{CommandID: protocol.CommandSubmitSMResp},
		Body: protocol.SubmitSMResp{
			MessageID: messageID,
			Optional: []protocol.OptionalParameter{
				{Tag: 1, Value: tlv1},
				{Tag: 2, Value: tlv2},
			},
		},
	}

	owned := ownDecodedPDU(pdu)

	for i := range scratch {
		scratch[i] = 'X'
	}

	body, ok := owned.Body.(protocol.SubmitSMResp)
	if !ok {
		t.Fatalf("owned.Body type = %T", owned.Body)
	}
	if string(body.MessageID) != messageIDText {
		t.Fatalf("MessageID = %q after source buffer reuse, want %q (aliases the source instead of owning a copy)", body.MessageID, messageIDText)
	}
	if len(body.Optional) != 2 {
		t.Fatalf("len(Optional) = %d, want 2", len(body.Optional))
	}
	if body.Optional[0].Tag != 1 || string(body.Optional[0].Value) != tlv1Text {
		t.Fatalf("Optional[0] = %+v after source buffer reuse, want tag=1 value=%q", body.Optional[0], tlv1Text)
	}
	if body.Optional[1].Tag != 2 || string(body.Optional[1].Value) != tlv2Text {
		t.Fatalf("Optional[1] = %+v after source buffer reuse, want tag=2 value=%q", body.Optional[1], tlv2Text)
	}
}

// TestOwnDecodedPDUSubmitMultiRespExactContent exercises the most structurally
// complex case (MessageID + Optional + a struct slice with its own byte-slice
// field) and checks every cloned byte exactly, including non-alias proof via
// the same overwrite technique. This is the case most likely to reveal an
// arena size/take mismatch (Task 2.3's central risk: the size pass and the
// take pass silently disagreeing and truncating a field).
func TestOwnDecodedPDUSubmitMultiRespExactContent(t *testing.T) {
	const messageIDText = "multi-message-id"
	const optValText = "opt-value"
	const dest1Text = "1000000001"
	const dest2Text = "1000000002"

	scratch := []byte(messageIDText + optValText + dest1Text + dest2Text)
	messageID := scratch[0:len(messageIDText)]
	rest := scratch[len(messageIDText):]
	optVal := rest[0:len(optValText)]
	rest = rest[len(optValText):]
	dest1 := rest[0:len(dest1Text)]
	dest2 := rest[len(dest1Text):]

	pdu := codec.DecodedPDU{
		Header: codec.Header{CommandID: protocol.CommandSubmitMultiResp},
		Body: protocol.SubmitMultiResp{
			MessageID: messageID,
			Optional:  []protocol.OptionalParameter{{Tag: 7, Value: optVal}},
			Unsuccessful: []protocol.UnsuccessfulSME{
				{DestinationAddr: dest1, ErrorStatusCode: protocol.CommandStatus(1)},
				{DestinationAddr: dest2, ErrorStatusCode: protocol.CommandStatus(2)},
			},
		},
	}

	owned := ownDecodedPDU(pdu)

	for i := range scratch {
		scratch[i] = 'X'
	}

	body, ok := owned.Body.(protocol.SubmitMultiResp)
	if !ok {
		t.Fatalf("owned.Body type = %T", owned.Body)
	}
	if string(body.MessageID) != messageIDText {
		t.Fatalf("MessageID = %q, want %q", body.MessageID, messageIDText)
	}
	if len(body.Optional) != 1 || string(body.Optional[0].Value) != optValText {
		t.Fatalf("Optional = %+v, want one entry with value %q", body.Optional, optValText)
	}
	if len(body.Unsuccessful) != 2 {
		t.Fatalf("len(Unsuccessful) = %d, want 2", len(body.Unsuccessful))
	}
	if string(body.Unsuccessful[0].DestinationAddr) != dest1Text || body.Unsuccessful[0].ErrorStatusCode != protocol.CommandStatus(1) {
		t.Fatalf("Unsuccessful[0] = %+v, want dest=%q status=1", body.Unsuccessful[0], dest1Text)
	}
	if string(body.Unsuccessful[1].DestinationAddr) != dest2Text || body.Unsuccessful[1].ErrorStatusCode != protocol.CommandStatus(2) {
		t.Fatalf("Unsuccessful[1] = %+v, want dest=%q status=2", body.Unsuccessful[1], dest2Text)
	}
}

// TestOwnDecodedPDUEmptyFieldsStayNil pins the pre-existing behavior (from
// cloneBytes/cloneOptional, now from responseArena) that a zero-length
// borrowed field clones to nil, not an empty-but-non-nil slice, since some
// encoders distinguish the two.
func TestOwnDecodedPDUEmptyFieldsStayNil(t *testing.T) {
	pdu := codec.DecodedPDU{
		Header: codec.Header{CommandID: protocol.CommandSubmitSMResp},
		Body:   protocol.SubmitSMResp{MessageID: []byte{}, Optional: nil},
	}
	owned := ownDecodedPDU(pdu)
	body, ok := owned.Body.(protocol.SubmitSMResp)
	if !ok {
		t.Fatalf("owned.Body type = %T", owned.Body)
	}
	if body.MessageID != nil {
		t.Fatalf("MessageID = %#v, want nil", body.MessageID)
	}
	if body.Optional != nil {
		t.Fatalf("Optional = %#v, want nil", body.Optional)
	}
}

func TestOwnDecodedPDURawBodyClones(t *testing.T) {
	scratch := []byte("raw-body-bytes")
	pdu := codec.DecodedPDU{Body: codec.RawBody(scratch)}
	owned := ownDecodedPDU(pdu)
	for i := range scratch {
		scratch[i] = 'X'
	}
	body, ok := owned.Body.(codec.RawBody)
	if !ok {
		t.Fatalf("owned.Body type = %T", owned.Body)
	}
	if string(body) != "raw-body-bytes" {
		t.Fatalf("RawBody = %q after source buffer reuse, want %q", body, "raw-body-bytes")
	}
}

// TestResponseArenaTakePanicsWhenUndersized proves the internal consistency
// guard actually fires. This state is never reachable through ownDecodedPDU
// itself (its size pass and take pass always agree), but the guard exists
// specifically to turn that class of future bug into an immediate test
// failure instead of a silently truncated clone, so it needs its own direct
// test rather than relying on never observing it.
func TestResponseArenaTakePanicsWhenUndersized(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("take() did not panic when asked for more bytes than the arena has")
		}
	}()
	a := newResponseArena(2)
	a.take([]byte("way too long for a 2-byte arena"))
}

// BenchmarkOwnDecodedPDUWithOptionalTLVs targets the case Finding P4 actually
// describes — the existing end-to-end session benchmarks respond with a bare
// MessageID and no TLVs, which wouldn't exercise the N-allocations-to-one
// reduction this task makes (one field clones to one allocation either way).
func BenchmarkOwnDecodedPDUWithOptionalTLVs(b *testing.B) {
	optional := make([]protocol.OptionalParameter, 5)
	for i := range optional {
		optional[i] = protocol.OptionalParameter{Tag: uint16(i + 1), Value: []byte("tlv-value-0123456789")}
	}
	pdu := codec.DecodedPDU{
		Header: codec.Header{CommandID: protocol.CommandSubmitSMResp},
		Body: protocol.SubmitSMResp{
			MessageID: []byte("benchmark-message-id"),
			Optional:  optional,
		},
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ownDecodedPDU(pdu)
	}
}
