package protocol

// PeerCapabilities is the negotiated view of the remote SMPP peer for one
// bound session. PeerVersion is the version explicitly advertised by the peer.
// NegotiatedVersion is the highest mutually supported version. When a bind
// response omits sc_interface_version, VersionAdvertised and TLV support are
// false as required by the compatibility rules.
type PeerCapabilities struct {
	PeerVersion       InterfaceVersion
	NegotiatedVersion InterfaceVersion
	VersionAdvertised bool
	SupportsTLV       bool
	SupportsSMPP50    bool
}

// NegotiatePeerCapabilities combines the local profile with an explicitly
// advertised peer version. The protocol only defines 0x34 and 0x50 as modern
// versions; reserved values are not interpreted as future-compatible support.
func NegotiatePeerCapabilities(local Profile, peer InterfaceVersion, advertised bool) PeerCapabilities {
	caps := PeerCapabilities{PeerVersion: peer, VersionAdvertised: advertised}
	if !local.Valid() || !advertised {
		return caps
	}
	switch peer {
	case InterfaceVersion34:
		caps.NegotiatedVersion = InterfaceVersion34
		caps.SupportsTLV = true
	case InterfaceVersion50:
		caps.SupportsTLV = true
		if local.InterfaceVersion == InterfaceVersion50 {
			caps.NegotiatedVersion = InterfaceVersion50
			caps.SupportsSMPP50 = true
		} else {
			caps.NegotiatedVersion = InterfaceVersion34
		}
	}
	return caps
}

// SCInterfaceVersion returns the first structurally valid sc_interface_version
// value from ordered optional parameters.
func SCInterfaceVersion(optional []OptionalParameter) (InterfaceVersion, bool) {
	for _, parameter := range optional {
		if parameter.Tag == TLVTagSCInterfaceVersion && len(parameter.Value) == 1 {
			return InterfaceVersion(parameter.Value[0]), true
		}
	}
	return 0, false
}

// CongestionStateFromOptional returns the first congestion_state TLV. A present
// TLV must have exactly one octet and a defined 0..100 value.
func CongestionStateFromOptional(optional []OptionalParameter) (CongestionState, bool, error) {
	for _, parameter := range optional {
		if parameter.Tag != TLVTagCongestionState {
			continue
		}
		if len(parameter.Value) != 1 {
			return 0, true, ErrInvalidCongestionState
		}
		state := CongestionState(parameter.Value[0])
		if !state.Valid() {
			return state, true, ErrInvalidCongestionState
		}
		return state, true, nil
	}
	return 0, false, nil
}
