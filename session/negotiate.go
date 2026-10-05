package session

import (
	"github.com/majiddarvishan/go-smpp/protocol"
)

func bindRequestVersion(body any) (protocol.InterfaceVersion, bool) {
	switch typed := body.(type) {
	case protocol.BindRequest:
		return typed.InterfaceVersion, true
	case *protocol.BindRequest:
		if typed != nil {
			return typed.InterfaceVersion, true
		}
	}
	return 0, false
}

// bindNegotiationProfile limits negotiated capabilities to what this local
// endpoint actually advertised in its bind request. Reserved and pre-3.4 bind
// versions deliberately produce no modern TLV/SMPP 5.0 capability assumption.
func bindNegotiationProfile(local protocol.Profile, advertised protocol.InterfaceVersion) (protocol.Profile, bool) {
	switch advertised {
	case protocol.InterfaceVersion34:
		return protocol.SMPP34Profile(), true
	case protocol.InterfaceVersion50:
		if local.IsSMPP50() {
			return protocol.SMPP50Profile(), true
		}
	}
	return protocol.Profile{}, false
}

// ensureSCInterfaceVersion follows the SMPP compatibility guidance that an MC
// supporting SMPP 3.4 or later should advertise its interface version in a
// successful bind response. An explicitly supplied tag is preserved so an
// application can intentionally advertise a narrower capability set.
func ensureSCInterfaceVersion(body any, version protocol.InterfaceVersion) any {
	appendIfMissing := func(response protocol.BindResponse) protocol.BindResponse {
		for _, parameter := range response.Optional {
			if parameter.Tag == protocol.TLVTagSCInterfaceVersion {
				return response
			}
		}
		// The caller still owns response.Optional. Appending in place would
		// write into its backing array whenever it has spare capacity, so a
		// reused BindResponse template would silently accumulate the tag or
		// have an unrelated element overwritten. Copy into a new slice sized
		// exactly for the result.
		injected := make([]protocol.OptionalParameter, len(response.Optional), len(response.Optional)+1)
		copy(injected, response.Optional)
		response.Optional = append(injected, protocol.OptionalParameter{
			Tag: protocol.TLVTagSCInterfaceVersion, Value: []byte{byte(version)},
		})
		return response
	}

	switch typed := body.(type) {
	case protocol.BindResponse:
		return appendIfMissing(typed)
	case *protocol.BindResponse:
		if typed == nil {
			return body
		}
		response := appendIfMissing(*typed)
		return response
	default:
		return body
	}
}

func requiresSMPP50(command protocol.CommandID) bool {
	switch command {
	case protocol.CommandBroadcastSM, protocol.CommandBroadcastSMResp,
		protocol.CommandQueryBroadcastSM, protocol.CommandQueryBroadcastSMResp,
		protocol.CommandCancelBroadcastSM, protocol.CommandCancelBroadcastSMResp:
		return true
	default:
		return false
	}
}
