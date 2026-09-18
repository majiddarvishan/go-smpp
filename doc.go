// Package smpp is the module-level documentation anchor for go-smpp.
//
// High-level ESME functionality lives in package client, SMSC functionality in
// package server, the shared concurrent runtime in package session, wire framing
// and registries in package codec, wire types/constants in package protocol,
// text/binary encodings in package encoding, SMS segmentation/reassembly in
// package message, and TCP/TLS adapters in package transport.
//
// The supported transport model is TCP, optionally wrapped in TLS. See the
// repository docs directory for API compatibility, concurrency, timeout,
// interoperability, and release policies.
package smpp
