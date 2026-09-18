package codec

import "testing"

func FuzzFramer(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0, 0, 0, 16, 0, 0, 0, 21, 0, 0, 0, 0, 0, 0, 0, 1})
	f.Add([]byte{0, 0, 0, 15})

	f.Fuzz(func(t *testing.T, data []byte) {
		framer, err := NewFramer(4096)
		if err != nil {
			t.Fatal(err)
		}
		_ = framer.Feed(data, func(frame []byte) error {
			if len(frame) < HeaderSize {
				t.Fatalf("emitted short frame: %d", len(frame))
			}
			h, err := DecodeHeader(frame)
			if err != nil {
				return err
			}
			if int(h.CommandLength) != len(frame) {
				t.Fatalf("declared=%d actual=%d", h.CommandLength, len(frame))
			}
			return nil
		})
	})
}


func FuzzDecodePDU(f *testing.F) {
	registry, err := NewSMPP34Registry(RegistryCompatible)
	if err != nil {
		f.Fatal(err)
	}
	f.Add([]byte{})
	f.Add([]byte{0, 0, 0, 16, 0, 0, 0, 21, 0, 0, 0, 0, 0, 0, 0, 1})
	f.Add([]byte{0, 0, 0, 15})

	f.Fuzz(func(t *testing.T, frame []byte) {
		// The fuzz contract is panic-freedom and bounded parsing. Structurally
		// invalid inputs may return typed fatal errors; compatible unknown
		// commands may decode as RawBody.
		_, _ = DecodePDU(frame, registry)
	})
}
