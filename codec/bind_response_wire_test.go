package codec

import (
	"bytes"
	"testing"

	"github.com/majiddarvishan/go-smpp/protocol"
)

// In SMPP 3.4 an unsuccessful bind response carries the 16-octet header
// without system_id. A peer that always expects a trailing C-Octet String
// may reject this standard error response, even though the wire is valid.
func TestSMPP34BindErrorResponseExactWire(t *testing.T) {
	registry := mustSMPP34Registry(t)
	for _, tc := range []struct {
		name string
		id   protocol.CommandID
	}{
		{"transmitter", protocol.CommandBindTransmitterResp},
		{"receiver", protocol.CommandBindReceiverResp},
		{"transceiver", protocol.CommandBindTransceiverResp},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, body := range []struct {
				name  string
				value any
			}{
				{"empty_body", protocol.EmptyBody{}},
				{"provided_system_id", protocol.BindResponse{SystemID: []byte("smsc")}},
			} {
				t.Run(body.name, func(t *testing.T) {
					header := Header{
						CommandID:      tc.id,
						CommandStatus:  protocol.StatusInvalidPassword,
						SequenceNumber: 0xadc00008,
					}
					got, err := EncodePDU(nil, header, body.value, registry)
					if err != nil {
						t.Fatal(err)
					}
					want := []byte{
						0, 0, 0, 16,
						byte(uint32(tc.id) >> 24), byte(uint32(tc.id) >> 16), byte(uint32(tc.id) >> 8), byte(tc.id),
						0, 0, 0, 0x0e,
						0xad, 0xc0, 0, 0x08,
					}
					if !bytes.Equal(got, want) {
						t.Fatalf("bind error wire mismatch: got %x, want %x", got, want)
					}
					decoded, err := DecodePDU(got, registry)
					if err != nil {
						t.Fatal(err)
					}
					if decoded.Header.CommandID != tc.id ||
						decoded.Header.CommandStatus != protocol.StatusInvalidPassword ||
						decoded.Header.SequenceNumber != header.SequenceNumber {
						t.Fatalf("unexpected decoded bind error header: %+v", decoded.Header)
					}
					bind, ok := decoded.Body.(protocol.BindResponse)
					if !ok || len(bind.SystemID) != 0 || len(bind.Optional) != 0 {
						t.Fatalf("unexpected bind error body: %#v", decoded.Body)
					}
				})
			}
		})
	}
}

// On success the system_id C-Octet String is present. Even an empty ID has
// one terminating NUL octet, unlike an error response.
func TestSMPP34BindResponseSuccessContainsCString(t *testing.T) {
	registry := mustSMPP34Registry(t)
	for _, body := range []struct {
		name   string
		id     []byte
		prefix []byte
	}{
		{"empty_system_id", nil, []byte{0}},
		{"named_system_id", []byte("smsc"), []byte{'s', 'm', 's', 'c', 0}},
	} {
		t.Run(body.name, func(t *testing.T) {
			wire, err := EncodePDU(nil, Header{
				CommandID: protocol.CommandBindTransceiverResp, SequenceNumber: 0xadc00008,
			}, protocol.BindResponse{SystemID: body.id}, registry)
			if err != nil {
				t.Fatal(err)
			}
			if len(wire) != HeaderSize+len(body.prefix) || !bytes.Equal(wire[HeaderSize:], body.prefix) {
				t.Fatalf("bind success wire: %x, expected %x after 16-octet header", wire, body.prefix)
			}
			decoded, err := DecodePDU(wire, registry)
			if err != nil {
				t.Fatal(err)
			}
			bind, ok := decoded.Body.(protocol.BindResponse)
			if !ok || !bytes.Equal(bind.SystemID, body.id) {
				t.Fatalf("unexpected decoded bind success body: %#v", decoded.Body)
			}
		})
	}
}
