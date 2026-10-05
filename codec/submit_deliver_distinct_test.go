package codec

import (
	"errors"
	"testing"

	"github.com/majiddarvishan/go-smpp/protocol"
)

// A6: protocol.SubmitSM and protocol.DeliverSM have the same fields on
// purpose (SMPP 3.4 gives the two PDUs the same mandatory-parameter layout)
// and are kept as distinct types so the Go type carries the direction. This
// pins the property that justifies the duplication: a body of one type
// handed to the other command is rejected by the encoder rather than being
// silently encoded under the wrong command ID. If the two types were merged
// (or one became an alias of the other) this test would stop compiling or
// fail, which is the signal to read the comments on both types first.
func TestEncodeRejectsSubmitSMAndDeliverSMBodiesUnderTheWrongCommand(t *testing.T) {
	registry, err := NewSMPP34Registry(RegistryStrict)
	if err != nil {
		t.Fatal(err)
	}
	submit := protocol.SubmitSM{SourceAddr: []byte("1"), DestinationAddr: []byte("2"), ShortMessage: []byte("x")}
	deliver := protocol.DeliverSM{SourceAddr: []byte("1"), DestinationAddr: []byte("2"), ShortMessage: []byte("x")}

	if _, err := EncodePDU(nil, Header{CommandID: protocol.CommandSubmitSM, SequenceNumber: 1}, submit, registry); err != nil {
		t.Fatalf("submit_sm with SubmitSM body: %v", err)
	}
	if _, err := EncodePDU(nil, Header{CommandID: protocol.CommandDeliverSM, SequenceNumber: 1}, deliver, registry); err != nil {
		t.Fatalf("deliver_sm with DeliverSM body: %v", err)
	}
	if _, err := EncodePDU(nil, Header{CommandID: protocol.CommandSubmitSM, SequenceNumber: 1}, deliver, registry); !errors.Is(err, ErrInvalidPDUValue) {
		t.Fatalf("submit_sm with DeliverSM body: err = %v, want ErrInvalidPDUValue", err)
	}
	if _, err := EncodePDU(nil, Header{CommandID: protocol.CommandDeliverSM, SequenceNumber: 1}, submit, registry); !errors.Is(err, ErrInvalidPDUValue) {
		t.Fatalf("deliver_sm with SubmitSM body: err = %v, want ErrInvalidPDUValue", err)
	}
}
