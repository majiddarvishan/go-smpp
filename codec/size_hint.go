package codec

import (
	"slices"

	"github.com/majiddarvishan/go-smpp/protocol"
)

// reservePDUCapacity provides exact capacity hints for the dominant
// submit/deliver request/response paths. It is intentionally only a hint:
// validation remains in the command encoders, and unsupported/vendor commands
// retain the generic append-growth behavior.
func reservePDUCapacity(dst []byte, command protocol.CommandID, body any) []byte {
	bodyLen, ok := encodedBodySizeHint(command, body)
	if !ok {
		return dst
	}
	return slices.Grow(dst, HeaderSize+bodyLen)
}

func encodedBodySizeHint(command protocol.CommandID, body any) (int, bool) {
	switch command {
	case protocol.CommandSubmitSM:
		switch v := body.(type) {
		case protocol.SubmitSM:
			return submitSMBodySize(v.ServiceType, v.SourceAddr, v.DestinationAddr, v.ScheduleDeliveryTime, v.ValidityPeriod, v.ShortMessage, v.Optional), true
		case *protocol.SubmitSM:
			if v != nil {
				return submitSMBodySize(v.ServiceType, v.SourceAddr, v.DestinationAddr, v.ScheduleDeliveryTime, v.ValidityPeriod, v.ShortMessage, v.Optional), true
			}
		}
	case protocol.CommandDeliverSM:
		switch v := body.(type) {
		case protocol.DeliverSM:
			return submitSMBodySize(v.ServiceType, v.SourceAddr, v.DestinationAddr, v.ScheduleDeliveryTime, v.ValidityPeriod, v.ShortMessage, v.Optional), true
		case *protocol.DeliverSM:
			if v != nil {
				return submitSMBodySize(v.ServiceType, v.SourceAddr, v.DestinationAddr, v.ScheduleDeliveryTime, v.ValidityPeriod, v.ShortMessage, v.Optional), true
			}
		}
	case protocol.CommandSubmitSMResp:
		switch v := body.(type) {
		case nil, protocol.EmptyBody, *protocol.EmptyBody:
			return 0, true
		case protocol.SubmitSMResp:
			return len(v.MessageID) + 1 + optionalParametersSize(v.Optional), true
		case *protocol.SubmitSMResp:
			if v != nil {
				return len(v.MessageID) + 1 + optionalParametersSize(v.Optional), true
			}
		case protocol.OptionalResponse:
			return optionalParametersSize(v.Optional), true
		case *protocol.OptionalResponse:
			if v != nil {
				return optionalParametersSize(v.Optional), true
			}
		}
	case protocol.CommandDeliverSMResp:
		switch v := body.(type) {
		case nil, protocol.EmptyBody, *protocol.EmptyBody:
			return 1, true // interoperable empty message_id C-Octet String
		case protocol.DeliverSMResp:
			return len(v.MessageID) + 1 + optionalParametersSize(v.Optional), true
		case *protocol.DeliverSMResp:
			if v != nil {
				return len(v.MessageID) + 1 + optionalParametersSize(v.Optional), true
			}
		case protocol.OptionalResponse:
			return optionalParametersSize(v.Optional), true
		case *protocol.OptionalResponse:
			if v != nil {
				return optionalParametersSize(v.Optional), true
			}
		}
	}
	return 0, false
}

func submitSMBodySize(serviceType, sourceAddr, destinationAddr, scheduleDeliveryTime, validityPeriod, shortMessage []byte, optional []protocol.OptionalParameter) int {
	// Five C-Octet terminators + 12 fixed one-octet fields.
	return 17 +
		len(serviceType) +
		len(sourceAddr) +
		len(destinationAddr) +
		len(scheduleDeliveryTime) +
		len(validityPeriod) +
		len(shortMessage) +
		optionalParametersSize(optional)
}

func optionalParametersSize(optional []protocol.OptionalParameter) int {
	n := 0
	for _, parameter := range optional {
		n += TLVHeaderSize + len(parameter.Value)
	}
	return n
}
