// Package encoding implements message-content encodings independently from the
// SMPP protocol codec hot path.
//
// It includes GSM 03.38/GSM 7-bit default and extension alphabets, septet
// packing/unpacking, strict UCS-2/BMP, UTF-16BE with surrogate-pair support for
// emoji and supplementary Unicode, and binary payload ownership helpers. Strict
// UCS-2 and surrogate-enabled UTF-16BE remain distinct because peer support for
// emoji under SMPP data_coding 0x08 varies.
package encoding
