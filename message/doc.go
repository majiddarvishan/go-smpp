// Package message provides higher-level SMS helpers above the protocol/session
// core.
//
// It performs encoding selection, GSM7/UCS2/UTF-16-aware sizing, UDH and SAR-TLV
// multipart segmentation, reassembly metadata handling, text decoding, and
// binary payload segmentation. Message decoding is opt-in and is not performed
// on the SMPP protocol hot path. Multipart sizing uses encoded septets or code
// units rather than UTF-8 byte length or Go rune count.
package message
