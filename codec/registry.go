package codec

import (
	"errors"
	"fmt"
	"sync"

	"github.com/majiddarvishan/go-smpp/protocol"
)

var (
	ErrRegistryFrozen            = errors.New("smpp codec: registry is frozen")
	ErrDuplicateCommand          = errors.New("smpp codec: duplicate command registration")
	ErrDuplicateTLV              = errors.New("smpp codec: duplicate TLV registration")
	ErrUnknownCommand            = errors.New("smpp codec: unknown command")
	ErrUnknownTLV                = errors.New("smpp codec: unknown TLV")
	ErrInvalidRegistryMode       = errors.New("smpp codec: invalid registry mode")
	ErrInvalidRegistryDefinition = errors.New("smpp codec: invalid registry definition")
)

// RegistryMode controls how a frozen registry reports unknown, structurally
// valid commands/TLVs. It never weakens framing or length validation.
type RegistryMode uint8

const (
	RegistryCompatible RegistryMode = iota
	RegistryStrict
)

func (m RegistryMode) Valid() bool {
	return m == RegistryCompatible || m == RegistryStrict
}

// CommandDecoder decodes the body of a structurally valid SMPP PDU. The body
// slice may be borrowed from the receive frame and must not be retained unless
// the decoder explicitly copies it.
type CommandDecoder func(header Header, body []byte) (any, error)

// CommandEncoder appends an encoded PDU body for value to dst.
type CommandEncoder func(dst []byte, value any) ([]byte, error)

// TLVDecoder converts a structurally valid TLV value to an optional typed form.
type TLVDecoder func(value []byte) (any, error)

// TLVEncoder appends an encoded TLV value for value to dst.
type TLVEncoder func(dst []byte, value any) ([]byte, error)

// CommandDefinition describes one standard or vendor-specific command.
type CommandDefinition struct {
	ID     protocol.CommandID
	Name   string
	Decode CommandDecoder
	Encode CommandEncoder
}

// TLVDefinition describes one standard or vendor-specific TLV tag.
type TLVDefinition struct {
	Tag    uint16
	Name   string
	Decode TLVDecoder
	Encode TLVEncoder
}

// RegistryBuilder is safe for concurrent configuration. Freeze creates an
// immutable Registry snapshot and permanently prevents further mutation.
type RegistryBuilder struct {
	mu       sync.Mutex
	mode     RegistryMode
	frozen   bool
	snapshot *Registry
	commands map[protocol.CommandID]CommandDefinition
	tlvs     map[uint16]TLVDefinition
}

func NewRegistryBuilder(mode RegistryMode) *RegistryBuilder {
	return &RegistryBuilder{
		mode:     mode,
		commands: make(map[protocol.CommandID]CommandDefinition),
		tlvs:     make(map[uint16]TLVDefinition),
	}
}

func (b *RegistryBuilder) RegisterCommand(def CommandDefinition) error {
	if b == nil {
		return ErrInvalidRegistryDefinition
	}
	if def.Name == "" {
		return fmt.Errorf("%w: command 0x%08x has empty name", ErrInvalidRegistryDefinition, uint32(def.ID))
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.frozen {
		return ErrRegistryFrozen
	}
	if _, exists := b.commands[def.ID]; exists {
		return fmt.Errorf("%w: 0x%08x", ErrDuplicateCommand, uint32(def.ID))
	}
	b.commands[def.ID] = def
	return nil
}

func (b *RegistryBuilder) RegisterTLV(def TLVDefinition) error {
	if b == nil {
		return ErrInvalidRegistryDefinition
	}
	if def.Name == "" {
		return fmt.Errorf("%w: TLV 0x%04x has empty name", ErrInvalidRegistryDefinition, def.Tag)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.frozen {
		return ErrRegistryFrozen
	}
	if _, exists := b.tlvs[def.Tag]; exists {
		return fmt.Errorf("%w: 0x%04x", ErrDuplicateTLV, def.Tag)
	}
	b.tlvs[def.Tag] = def
	return nil
}

// Freeze returns an immutable snapshot suitable for concurrent hot-path use.
// Repeated calls return the same snapshot. A Register call that races with
// Freeze is serialized: it either completes before the snapshot or fails with
// ErrRegistryFrozen after it.
func (b *RegistryBuilder) Freeze() (*Registry, error) {
	if b == nil {
		return nil, ErrInvalidRegistryDefinition
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.mode.Valid() {
		return nil, ErrInvalidRegistryMode
	}
	if b.snapshot != nil {
		return b.snapshot, nil
	}

	commands := make(map[protocol.CommandID]CommandDefinition, len(b.commands))
	for id, def := range b.commands {
		commands[id] = def
	}
	tlvs := make(map[uint16]TLVDefinition, len(b.tlvs))
	for tag, def := range b.tlvs {
		tlvs[tag] = def
	}

	b.frozen = true
	b.snapshot = &Registry{mode: b.mode, commands: commands, tlvs: tlvs}
	return b.snapshot, nil
}

// Registry is an immutable command/TLV lookup snapshot. Once constructed by
// Freeze it is safe for concurrent reads without locks.
type Registry struct {
	mode     RegistryMode
	commands map[protocol.CommandID]CommandDefinition
	tlvs     map[uint16]TLVDefinition
}

func (r *Registry) Mode() RegistryMode {
	if r == nil {
		return RegistryCompatible
	}
	return r.mode
}

func (r *Registry) CommandCount() int {
	if r == nil {
		return 0
	}
	return len(r.commands)
}

func (r *Registry) TLVCount() int {
	if r == nil {
		return 0
	}
	return len(r.tlvs)
}

func (r *Registry) Command(id protocol.CommandID) (CommandDefinition, bool) {
	if r == nil {
		return CommandDefinition{}, false
	}
	def, ok := r.commands[id]
	return def, ok
}

func (r *Registry) TLV(tag uint16) (TLVDefinition, bool) {
	if r == nil {
		return TLVDefinition{}, false
	}
	def, ok := r.tlvs[tag]
	return def, ok
}

// ResolveCommand applies the configured unknown-command policy after framing is
// already known to be structurally valid. Compatible mode leaves the command
// unresolved so a higher layer can apply SMPP generic_nack/status behavior.
func (r *Registry) ResolveCommand(id protocol.CommandID) (CommandDefinition, bool, error) {
	if def, ok := r.Command(id); ok {
		return def, true, nil
	}
	if r != nil && r.mode == RegistryStrict {
		return CommandDefinition{}, false, fmt.Errorf("%w: 0x%08x", ErrUnknownCommand, uint32(id))
	}
	return CommandDefinition{}, false, nil
}

// ResolveTLV applies the configured unknown-TLV policy after TLV boundaries
// have already been validated. Compatible mode leaves the TLV unresolved so
// its raw ordered representation can be preserved.
func (r *Registry) ResolveTLV(tag uint16) (TLVDefinition, bool, error) {
	if def, ok := r.TLV(tag); ok {
		return def, true, nil
	}
	if r != nil && r.mode == RegistryStrict {
		return TLVDefinition{}, false, fmt.Errorf("%w: 0x%04x", ErrUnknownTLV, tag)
	}
	return TLVDefinition{}, false, nil
}
