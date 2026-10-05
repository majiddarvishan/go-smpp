package session

import (
	"fmt"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
)

// ownDecodedPDU deep-copies every borrowed byte-slice field of a decoded
// response body into memory this session no longer controls, since the
// framer's read buffer that pdu's fields currently point into is reused for
// the next inbound frame as soon as this call returns. This is necessary —
// but a response with several optional TLVs used to cost one allocation per
// byte-slice field (Finding P4). It now costs at most two: one shared byte
// arena for every field's bytes, sized by a first pass over the same body,
// plus (only for the two body types that carry one) a separate allocation
// for the []protocol.OptionalParameter or []protocol.UnsuccessfulSME struct
// slice itself, which cannot share a byte arena because it is not bytes.
func ownDecodedPDU(pdu codec.DecodedPDU) codec.DecodedPDU {
	switch body := pdu.Body.(type) {
	case protocol.BindResponse:
		arena := newResponseArena(len(body.SystemID) + optionalCloneSize(body.Optional))
		body.SystemID = arena.take(body.SystemID)
		body.Optional = arena.takeOptional(body.Optional)
		pdu.Body = body
	case protocol.SubmitSMResp:
		arena := newResponseArena(len(body.MessageID) + optionalCloneSize(body.Optional))
		body.MessageID = arena.take(body.MessageID)
		body.Optional = arena.takeOptional(body.Optional)
		pdu.Body = body
	case protocol.DeliverSMResp:
		arena := newResponseArena(len(body.MessageID) + optionalCloneSize(body.Optional))
		body.MessageID = arena.take(body.MessageID)
		body.Optional = arena.takeOptional(body.Optional)
		pdu.Body = body
	case protocol.DataSMResp:
		arena := newResponseArena(len(body.MessageID) + optionalCloneSize(body.Optional))
		body.MessageID = arena.take(body.MessageID)
		body.Optional = arena.takeOptional(body.Optional)
		pdu.Body = body
	case protocol.SubmitMultiResp:
		size := len(body.MessageID) + optionalCloneSize(body.Optional)
		for i := range body.Unsuccessful {
			size += len(body.Unsuccessful[i].DestinationAddr)
		}
		arena := newResponseArena(size)
		body.MessageID = arena.take(body.MessageID)
		body.Optional = arena.takeOptional(body.Optional)
		if len(body.Unsuccessful) > 0 {
			entries := make([]protocol.UnsuccessfulSME, len(body.Unsuccessful))
			copy(entries, body.Unsuccessful)
			for i := range entries {
				entries[i].DestinationAddr = arena.take(entries[i].DestinationAddr)
			}
			body.Unsuccessful = entries
		}
		pdu.Body = body
	case protocol.QuerySMResp:
		arena := newResponseArena(len(body.MessageID) + len(body.FinalDate) + optionalCloneSize(body.Optional))
		body.MessageID = arena.take(body.MessageID)
		body.FinalDate = arena.take(body.FinalDate)
		body.Optional = arena.takeOptional(body.Optional)
		pdu.Body = body
	case protocol.BroadcastSMResp:
		arena := newResponseArena(len(body.MessageID) + optionalCloneSize(body.Optional))
		body.MessageID = arena.take(body.MessageID)
		body.Optional = arena.takeOptional(body.Optional)
		pdu.Body = body
	case protocol.QueryBroadcastSMResp:
		arena := newResponseArena(len(body.MessageID) + optionalCloneSize(body.Optional))
		body.MessageID = arena.take(body.MessageID)
		body.Optional = arena.takeOptional(body.Optional)
		pdu.Body = body
	case protocol.OptionalResponse:
		arena := newResponseArena(optionalCloneSize(body.Optional))
		body.Optional = arena.takeOptional(body.Optional)
		pdu.Body = body
	case codec.RawBody:
		// A single field: an arena would be pure overhead over cloneBytes.
		pdu.Body = codec.RawBody(cloneBytes(body))
	}
	return pdu
}

// responseArena backs every byte-slice field ownDecodedPDU clones for one
// response body with a single shared allocation. Construct it with the exact
// total byte count every take() call below it will request, computed by a
// first pass over the same (immutable, local) body value — take() panics if
// that promise is broken. That can only happen from a bug in this file's own
// two passes disagreeing with each other; a decoded PDU's field lengths are
// fixed once decoded; peer-controlled content therefore cannot make a
// correctly sized arena run short. Panicking here trades a silent, corrupted,
// undersized clone (a real allocation win landing as a real data-loss bug)
// for an immediate, loud test failure — this is not the "no panic on
// peer-influenced input" class of panic Phase 1 removed from the window path.
type responseArena struct {
	buf []byte
}

func newResponseArena(size int) *responseArena {
	if size == 0 {
		return &responseArena{}
	}
	return &responseArena{buf: make([]byte, size)}
}

// take copies src into the arena's next unused bytes and returns that region,
// capped to length so a caller appending to the result grows into a fresh
// allocation instead of overwriting the next field sharing this arena.
func (a *responseArena) take(src []byte) []byte {
	if len(src) == 0 {
		return nil
	}
	if len(a.buf) < len(src) {
		panic(fmt.Sprintf("session: response arena undersized: need %d more bytes, have %d — ownDecodedPDU's size pass and take pass disagree", len(src), len(a.buf)))
	}
	n := copy(a.buf, src)
	dst := a.buf[:n:n]
	a.buf = a.buf[n:]
	return dst
}

func (a *responseArena) takeOptional(src []protocol.OptionalParameter) []protocol.OptionalParameter {
	if len(src) == 0 {
		return nil
	}
	dst := make([]protocol.OptionalParameter, len(src))
	for i := range src {
		dst[i] = protocol.OptionalParameter{Tag: src[i].Tag, Value: a.take(src[i].Value)}
	}
	return dst
}

// optionalCloneSize is the byte-arena space every Value in opts will need
// from a responseArena — the size-pass counterpart to takeOptional.
func optionalCloneSize(opts []protocol.OptionalParameter) int {
	n := 0
	for i := range opts {
		n += len(opts[i].Value)
	}
	return n
}

func responseOptionalParameters(body any) []protocol.OptionalParameter {
	switch v := body.(type) {
	case protocol.BindResponse:
		return v.Optional
	case protocol.SubmitSMResp:
		return v.Optional
	case protocol.DeliverSMResp:
		return v.Optional
	case protocol.DataSMResp:
		return v.Optional
	case protocol.SubmitMultiResp:
		return v.Optional
	case protocol.QuerySMResp:
		return v.Optional
	case protocol.BroadcastSMResp:
		return v.Optional
	case protocol.QueryBroadcastSMResp:
		return v.Optional
	case protocol.OptionalResponse:
		return v.Optional
	default:
		return nil
	}
}

func cloneBytes(src []byte) []byte {
	if len(src) == 0 {
		return nil
	}
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}
