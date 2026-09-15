package protocol

// TON is the SMPP Type Of Number field.
type TON uint8

const (
	TONUnknown          TON = 0x00
	TONInternational    TON = 0x01
	TONNational         TON = 0x02
	TONNetworkSpecific  TON = 0x03
	TONSubscriberNumber TON = 0x04
	TONAlphanumeric     TON = 0x05
	TONAbbreviated      TON = 0x06
)

// NPI is the SMPP Numbering Plan Indicator field.
type NPI uint8

const (
	NPIUnknown     NPI = 0x00
	NPIISDN        NPI = 0x01
	NPIData        NPI = 0x03
	NPITelex       NPI = 0x04
	NPILandMobile  NPI = 0x06
	NPINational    NPI = 0x08
	NPIPrivate     NPI = 0x09
	NPIERMES       NPI = 0x0A
	NPIInternet    NPI = 0x0E
	NPIWAPClientID NPI = 0x12
)
