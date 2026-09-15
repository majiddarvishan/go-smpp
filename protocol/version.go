package protocol

// InterfaceVersion is the one-octet SMPP interface_version value.
type InterfaceVersion uint8

const (
	InterfaceVersion34 InterfaceVersion = 0x34
	InterfaceVersion50 InterfaceVersion = 0x50
)

// Profile describes protocol-version capabilities without duplicating the
// protocol/session implementation for each SMPP version.
type Profile struct {
	InterfaceVersion InterfaceVersion
}

// SMPP34Profile returns the standard SMPP 3.4 protocol profile.
func SMPP34Profile() Profile {
	return Profile{InterfaceVersion: InterfaceVersion34}
}

// SMPP50Profile returns the standard SMPP 5.0 protocol profile.
func SMPP50Profile() Profile {
	return Profile{InterfaceVersion: InterfaceVersion50}
}

// Valid reports whether the profile identifies a version explicitly supported
// by this library architecture.
func (p Profile) Valid() bool {
	return p.InterfaceVersion == InterfaceVersion34 || p.InterfaceVersion == InterfaceVersion50
}

// SupportsOptionalParameters reports whether TLV optional parameters are part
// of the selected protocol profile.
func (p Profile) SupportsOptionalParameters() bool {
	return p.Valid()
}

// IsSMPP50 reports whether SMPP 5.0-only capabilities may be negotiated.
func (p Profile) IsSMPP50() bool {
	return p.InterfaceVersion == InterfaceVersion50
}
