package codec

import (
	"errors"
	"sync"
	"testing"

	"github.com/majiddarvishan/go-smpp/protocol"
)

func TestRegistryFreezeAndLookup(t *testing.T) {
	builder := NewRegistryBuilder(RegistryCompatible)
	vendorCommand := protocol.CommandID(0x00010201)
	vendorTLV := uint16(0x1400)

	decodeCommand := func(header Header, body []byte) (any, error) {
		return len(body), nil
	}
	if err := builder.RegisterCommand(CommandDefinition{
		ID: vendorCommand, Name: "vendor_submit", Decode: decodeCommand,
	}); err != nil {
		t.Fatalf("register command: %v", err)
	}
	if err := builder.RegisterTLV(TLVDefinition{
		Tag: vendorTLV, Name: "vendor_feature",
		Decode: func(value []byte) (any, error) { return len(value), nil },
	}); err != nil {
		t.Fatalf("register TLV: %v", err)
	}

	registry, err := builder.Freeze()
	if err != nil {
		t.Fatalf("freeze: %v", err)
	}
	if registry.CommandCount() != 1 || registry.TLVCount() != 1 {
		t.Fatalf("unexpected registry counts: commands=%d tlvs=%d", registry.CommandCount(), registry.TLVCount())
	}

	command, ok := registry.Command(vendorCommand)
	if !ok || command.Name != "vendor_submit" || command.Decode == nil {
		t.Fatalf("vendor command lookup failed: ok=%v def=%+v", ok, command)
	}
	value, err := command.Decode(Header{CommandID: vendorCommand}, []byte{1, 2, 3})
	if err != nil || value.(int) != 3 {
		t.Fatalf("vendor command decoder returned value=%v err=%v", value, err)
	}

	tlv, ok := registry.TLV(vendorTLV)
	if !ok || tlv.Name != "vendor_feature" || tlv.Decode == nil {
		t.Fatalf("vendor TLV lookup failed: ok=%v def=%+v", ok, tlv)
	}

	again, err := builder.Freeze()
	if err != nil || again != registry {
		t.Fatal("Freeze must be idempotent and return the same immutable snapshot")
	}
}

func TestRegistryDuplicateAndFrozen(t *testing.T) {
	builder := NewRegistryBuilder(RegistryCompatible)
	command := CommandDefinition{ID: protocol.CommandSubmitSM, Name: "submit_sm"}
	tlv := TLVDefinition{Tag: 0x1401, Name: "vendor_option"}

	if err := builder.RegisterCommand(command); err != nil {
		t.Fatal(err)
	}
	if err := builder.RegisterCommand(command); !errors.Is(err, ErrDuplicateCommand) {
		t.Fatalf("expected duplicate command error, got %v", err)
	}
	if err := builder.RegisterTLV(tlv); err != nil {
		t.Fatal(err)
	}
	if err := builder.RegisterTLV(tlv); !errors.Is(err, ErrDuplicateTLV) {
		t.Fatalf("expected duplicate TLV error, got %v", err)
	}
	if _, err := builder.Freeze(); err != nil {
		t.Fatal(err)
	}
	if err := builder.RegisterCommand(CommandDefinition{ID: protocol.CommandDeliverSM, Name: "deliver_sm"}); !errors.Is(err, ErrRegistryFrozen) {
		t.Fatalf("expected frozen error, got %v", err)
	}
	if err := builder.RegisterTLV(TLVDefinition{Tag: 0x1402, Name: "late_vendor_option"}); !errors.Is(err, ErrRegistryFrozen) {
		t.Fatalf("expected frozen error, got %v", err)
	}
}

func TestRegistryStrictAndCompatibleUnknownHandling(t *testing.T) {
	compatible, err := NewRegistryBuilder(RegistryCompatible).Freeze()
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := compatible.ResolveCommand(0x12345678); err != nil || found {
		t.Fatalf("compatible unknown command: found=%v err=%v", found, err)
	}
	if _, found, err := compatible.ResolveTLV(0xEE01); err != nil || found {
		t.Fatalf("compatible unknown TLV: found=%v err=%v", found, err)
	}

	strict, err := NewRegistryBuilder(RegistryStrict).Freeze()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := strict.ResolveCommand(0x12345678); !errors.Is(err, ErrUnknownCommand) {
		t.Fatalf("expected strict unknown command error, got %v", err)
	}
	if _, _, err := strict.ResolveTLV(0xEE01); !errors.Is(err, ErrUnknownTLV) {
		t.Fatalf("expected strict unknown TLV error, got %v", err)
	}
}

func TestRegistryInvalidDefinitionsAndMode(t *testing.T) {
	builder := NewRegistryBuilder(RegistryCompatible)
	if err := builder.RegisterCommand(CommandDefinition{ID: 1}); !errors.Is(err, ErrInvalidRegistryDefinition) {
		t.Fatalf("expected invalid command definition, got %v", err)
	}
	if err := builder.RegisterTLV(TLVDefinition{Tag: 1}); !errors.Is(err, ErrInvalidRegistryDefinition) {
		t.Fatalf("expected invalid TLV definition, got %v", err)
	}
	if _, err := NewRegistryBuilder(RegistryMode(255)).Freeze(); !errors.Is(err, ErrInvalidRegistryMode) {
		t.Fatalf("expected invalid mode error, got %v", err)
	}
}

func TestRegistryConcurrentConfigurationAndReads(t *testing.T) {
	const count = 128
	builder := NewRegistryBuilder(RegistryCompatible)

	var wg sync.WaitGroup
	wg.Add(count * 2)
	for i := 0; i < count; i++ {
		i := i
		go func() {
			defer wg.Done()
			id := protocol.CommandID(0x10010000 + uint32(i))
			if err := builder.RegisterCommand(CommandDefinition{ID: id, Name: "vendor_command"}); err != nil {
				t.Errorf("register command %d: %v", i, err)
			}
		}()
		go func() {
			defer wg.Done()
			tag := uint16(0xE000 + i)
			if err := builder.RegisterTLV(TLVDefinition{Tag: tag, Name: "vendor_tlv"}); err != nil {
				t.Errorf("register TLV %d: %v", i, err)
			}
		}()
	}
	wg.Wait()

	registry, err := builder.Freeze()
	if err != nil {
		t.Fatal(err)
	}
	if registry.CommandCount() != count || registry.TLVCount() != count {
		t.Fatalf("unexpected counts: commands=%d tlvs=%d", registry.CommandCount(), registry.TLVCount())
	}

	wg.Add(count * 2)
	for i := 0; i < count; i++ {
		i := i
		go func() {
			defer wg.Done()
			id := protocol.CommandID(0x10010000 + uint32(i))
			if _, ok := registry.Command(id); !ok {
				t.Errorf("missing command %d", i)
			}
		}()
		go func() {
			defer wg.Done()
			tag := uint16(0xE000 + i)
			if _, ok := registry.TLV(tag); !ok {
				t.Errorf("missing TLV %d", i)
			}
		}()
	}
	wg.Wait()
}
