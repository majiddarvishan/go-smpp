// Package message provides higher-level SMS helpers above the protocol/session
// core, including encoding selection, UDH and SAR-TLV multipart segmentation,
// reassembly metadata, and binary payload support. Message decoding is opt-in
// and is not performed on the SMPP protocol hot path.
package message
