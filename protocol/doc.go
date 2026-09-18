// Package protocol contains SMPP wire-level constants, status and command
// values, address/data-coding types, session states, capability models, and
// typed PDU bodies for SMPP 3.4 plus the implemented SMPP 5.0 extensions.
//
// It intentionally has no dependency on codec, session, transport, client, or
// server packages. Protocol structs are caller-owned values; callers must not
// concurrently mutate the same value or backing byte slice without their own
// synchronization.
package protocol
