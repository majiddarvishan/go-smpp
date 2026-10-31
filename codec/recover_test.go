package codec

import (
	"errors"
	"testing"

	"github.com/majiddarvishan/go-smpp/protocol"
)

const panickingCommand protocol.CommandID = 0x4A0000F2

func panickingRegistry(t *testing.T) *Registry {
	t.Helper()
	builder, err := NewSMPP34RegistryBuilder(RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	err = builder.RegisterCommand(CommandDefinition{
		ID:     panickingCommand,
		Name:   "panicking",
		Decode: func(Header, []byte) (any, error) { panic("decoder exploded") },
		Encode: func([]byte, any) ([]byte, error) { panic("encoder exploded") },
	})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := builder.Freeze()
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestEncodePDURecoversAnEncoderPanic(t *testing.T) {
	dst := []byte("kept")
	out, err := EncodePDU(dst, Header{CommandID: panickingCommand, SequenceNumber: 1}, protocol.EmptyBody{}, panickingRegistry(t))
	var p *protocol.PanicError
	if !errors.As(err, &p) || p.Source != "codec.CommandEncoder" || p.Value != "encoder exploded" || len(p.Stack) == 0 {
		t.Fatalf("err = %#v, want a *protocol.PanicError from codec.CommandEncoder", err)
	}
	if string(out) != "kept" {
		t.Fatalf("dst = %q after a failed encode, want it unchanged", out)
	}
}

func TestDecodePDURecoversADecoderPanic(t *testing.T) {
	frame := make([]byte, HeaderSize)
	if err := EncodeHeader(frame, Header{CommandLength: HeaderSize, CommandID: panickingCommand, SequenceNumber: 9}); err != nil {
		t.Fatal(err)
	}
	_, err := DecodePDU(frame, panickingRegistry(t))
	var semantic *protocol.SemanticError
	var p *protocol.PanicError
	if !errors.As(err, &semantic) || semantic.Status != protocol.StatusSystemError || semantic.Sequence != 9 {
		t.Fatalf("err = %v, want a SemanticError with ESME_RSYSERR, so the session rejects only this PDU", err)
	}
	if !errors.As(err, &p) || p.Source != "codec.CommandDecoder" {
		t.Fatalf("err = %v, want it to carry a *protocol.PanicError from codec.CommandDecoder", err)
	}
}

func TestNilAndZeroFramerDoNotPanic(t *testing.T) {
	for name, f := range map[string]*Framer{"nil": nil, "zero": {}} {
		if err := f.Feed([]byte{0, 0, 0, 16}, nil); !errors.Is(err, ErrNotInitialized) {
			t.Errorf("%s Feed() = %v", name, err)
		}
		if err := f.Finalize(); !errors.Is(err, ErrNotInitialized) {
			t.Errorf("%s Finalize() = %v", name, err)
		}
		if !f.Failed() || f.Buffered() != 0 {
			t.Errorf("%s Failed()=%v Buffered()=%d", name, f.Failed(), f.Buffered())
		}
	}
}
