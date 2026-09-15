package protocol

// SessionState represents the shared SMPP session state model.
type SessionState uint8

const (
	StateClosed SessionState = iota
	StateOpen
	StateOutbound
	StateBoundTX
	StateBoundRX
	StateBoundTRX
	StateUnbound
)

func (s SessionState) String() string {
	switch s {
	case StateClosed:
		return "Closed"
	case StateOpen:
		return "Open"
	case StateOutbound:
		return "Outbound"
	case StateBoundTX:
		return "Bound_TX"
	case StateBoundRX:
		return "Bound_RX"
	case StateBoundTRX:
		return "Bound_TRX"
	case StateUnbound:
		return "Unbound"
	default:
		return "Unknown"
	}
}
