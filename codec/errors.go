package codec

import "errors"

var (
	ErrShortBuffer          = errors.New("smpp codec: short buffer")
	ErrInvalidCommandLength = errors.New("smpp codec: command_length must be at least 16")
	ErrInvalidMaxPDUSize    = errors.New("smpp codec: maximum PDU size must be at least 16")
	ErrFieldTooLong         = errors.New("smpp codec: field exceeds configured maximum")
	ErrEmbeddedNUL          = errors.New("smpp codec: C-Octet String contains embedded NUL")
	ErrTLVValueTooLong      = errors.New("smpp codec: TLV value exceeds 65535 octets")
	ErrNotInitialized       = errors.New("smpp codec: not initialized; create framers with NewFramer")
)
