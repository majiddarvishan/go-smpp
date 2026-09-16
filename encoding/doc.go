// Package encoding implements message-content encodings independently from the
// SMPP protocol codec hot path. It includes GSM 03.38/GSM 7-bit default and
// extension alphabets, septet packing/unpacking, strict UCS-2/BMP, UTF-16BE with
// surrogate-pair support for emoji, and binary payload ownership helpers.
package encoding
