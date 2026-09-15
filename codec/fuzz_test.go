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
