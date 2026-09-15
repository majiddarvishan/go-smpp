package protocol

// DataCoding is the SMPP data_coding octet.
type DataCoding uint8

const (
	DataCodingSMSCDefault    DataCoding = 0x00
	DataCodingIA5            DataCoding = 0x01
	DataCodingOctetBinary1   DataCoding = 0x02
	DataCodingLatin1         DataCoding = 0x03
	DataCodingOctetBinary2   DataCoding = 0x04
	DataCodingJIS            DataCoding = 0x05
	DataCodingCyrillic       DataCoding = 0x06
	DataCodingLatinHebrew    DataCoding = 0x07
	DataCodingUCS2           DataCoding = 0x08
	DataCodingPictogram      DataCoding = 0x09
	DataCodingISO2022JP      DataCoding = 0x0A
	DataCodingExtendedKanji  DataCoding = 0x0D
	DataCodingKSC5601        DataCoding = 0x0E
)
