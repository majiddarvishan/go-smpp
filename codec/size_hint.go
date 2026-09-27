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

// EncodedPDUSizeHint returns an estimate of the fully encoded frame size
// (header plus body) for command with the given body value, and whether an
// estimate is available at all. It exists for callers that pre-size a
// reusable buffer — for example, a per-session outbound frame pool — so a
// brand-new buffer does not start undersized and force an immediate regrowth
// on its first use. EncodePDU does not depend on this hint for correctness:
// it grows its destination as needed regardless of whether a hint was used to
// size it, and regardless of whether the hint (when available) undershoots
// the PDU's actual encoded size.
func EncodedPDUSizeHint(command protocol.CommandID, body any) (int, bool) {
	bodyLen, ok := encodedBodySizeHint(command, body)
	if !ok {
		return 0, false
	}
	return HeaderSize + bodyLen, true
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
	case protocol.CommandBindReceiver, protocol.CommandBindTransmitter, protocol.CommandBindTransceiver:
		switch v := body.(type) {
		case protocol.BindRequest:
			return bindRequestBodySize(v.SystemID, v.Password, v.SystemType, v.AddressRange), true
		case *protocol.BindRequest:
			if v != nil {
				return bindRequestBodySize(v.SystemID, v.Password, v.SystemType, v.AddressRange), true
			}
		}
	case protocol.CommandBindReceiverResp, protocol.CommandBindTransmitterResp, protocol.CommandBindTransceiverResp:
		switch v := body.(type) {
		case nil, protocol.EmptyBody, *protocol.EmptyBody:
			return 0, true
		case protocol.BindResponse:
			return len(v.SystemID) + 1 + optionalParametersSize(v.Optional), true
		case *protocol.BindResponse:
			if v != nil {
				return len(v.SystemID) + 1 + optionalParametersSize(v.Optional), true
			}
		case protocol.OptionalResponse:
			return optionalParametersSize(v.Optional), true
		case *protocol.OptionalResponse:
			if v != nil {
				return optionalParametersSize(v.Optional), true
			}
		}
	case protocol.CommandDataSM:
		switch v := body.(type) {
		case protocol.DataSM:
			return dataSMBodySize(v.ServiceType, v.SourceAddr, v.DestinationAddr, v.Optional), true
		case *protocol.DataSM:
			if v != nil {
				return dataSMBodySize(v.ServiceType, v.SourceAddr, v.DestinationAddr, v.Optional), true
			}
		}
	case protocol.CommandDataSMResp:
		switch v := body.(type) {
		case nil, protocol.EmptyBody, *protocol.EmptyBody:
			return 0, true
		case protocol.DataSMResp:
			return len(v.MessageID) + 1 + optionalParametersSize(v.Optional), true
		case *protocol.DataSMResp:
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
	case protocol.CommandBroadcastSM:
		switch v := body.(type) {
		case protocol.BroadcastSM:
			return broadcastSMBodySize(v.ServiceType, v.SourceAddr, v.MessageID, v.ScheduleDeliveryTime, v.ValidityPeriod, v.Optional), true
		case *protocol.BroadcastSM:
			if v != nil {
				return broadcastSMBodySize(v.ServiceType, v.SourceAddr, v.MessageID, v.ScheduleDeliveryTime, v.ValidityPeriod, v.Optional), true
			}
		}
	case protocol.CommandBroadcastSMResp:
		switch v := body.(type) {
		case nil, protocol.EmptyBody, *protocol.EmptyBody:
			return 0, true
		case protocol.BroadcastSMResp:
			return len(v.MessageID) + 1 + optionalParametersSize(v.Optional), true
		case *protocol.BroadcastSMResp:
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
	case protocol.CommandQueryBroadcastSM:
		switch v := body.(type) {
		case protocol.QueryBroadcastSM:
			return queryBroadcastSMBodySize(v.MessageID, v.SourceAddr, v.Optional), true
		case *protocol.QueryBroadcastSM:
			if v != nil {
				return queryBroadcastSMBodySize(v.MessageID, v.SourceAddr, v.Optional), true
			}
		}
	case protocol.CommandQueryBroadcastSMResp:
		switch v := body.(type) {
		case nil, protocol.EmptyBody, *protocol.EmptyBody:
			return 0, true
		case protocol.QueryBroadcastSMResp:
			return len(v.MessageID) + 1 + optionalParametersSize(v.Optional), true
		case *protocol.QueryBroadcastSMResp:
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
	case protocol.CommandCancelBroadcastSM:
		switch v := body.(type) {
		case protocol.CancelBroadcastSM:
			return cancelBroadcastSMBodySize(v.ServiceType, v.MessageID, v.SourceAddr, v.Optional), true
		case *protocol.CancelBroadcastSM:
			if v != nil {
				return cancelBroadcastSMBodySize(v.ServiceType, v.MessageID, v.SourceAddr, v.Optional), true
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

// bindRequestBodySize covers bind_receiver/bind_transmitter/bind_transceiver,
// which all share protocol.BindRequest's wire layout: SystemID, Password,
// SystemType, and AddressRange are each a C-Octet String (four terminators);
// InterfaceVersion, AddressTON, and AddressNPI are three fixed one-octet
// fields. See encodeBindRequest.
func bindRequestBodySize(systemID, password, systemType, addressRange []byte) int {
	return 7 + len(systemID) + len(password) + len(systemType) + len(addressRange)
}

// dataSMBodySize covers data_sm: ServiceType, SourceAddr, and DestinationAddr
// are each a C-Octet String (three terminators); SourceAddrTON, SourceAddrNPI,
// DestAddrTON, DestAddrNPI, ESMClass, RegisteredDelivery, and DataCoding are
// seven fixed one-octet fields. See encodeDataSM.
func dataSMBodySize(serviceType, sourceAddr, destinationAddr []byte, optional []protocol.OptionalParameter) int {
	return 10 + len(serviceType) + len(sourceAddr) + len(destinationAddr) + optionalParametersSize(optional)
}

// broadcastSMBodySize covers broadcast_sm: ServiceType, SourceAddr, MessageID,
// ScheduleDeliveryTime, and ValidityPeriod are each a C-Octet String (five
// terminators); SourceAddrTON, SourceAddrNPI, PriorityFlag,
// ReplaceIfPresentFlag, DataCoding, and SMDefaultMsgID are six fixed
// one-octet fields. See encodeBroadcastSM.
func broadcastSMBodySize(serviceType, sourceAddr, messageID, scheduleDeliveryTime, validityPeriod []byte, optional []protocol.OptionalParameter) int {
	return 11 +
		len(serviceType) +
		len(sourceAddr) +
		len(messageID) +
		len(scheduleDeliveryTime) +
		len(validityPeriod) +
		optionalParametersSize(optional)
}

// queryBroadcastSMBodySize covers query_broadcast_sm: MessageID and
// SourceAddr are each a C-Octet String (two terminators); SourceAddrTON and
// SourceAddrNPI are two fixed one-octet fields. See encodeQueryBroadcastSM.
func queryBroadcastSMBodySize(messageID, sourceAddr []byte, optional []protocol.OptionalParameter) int {
	return 4 + len(messageID) + len(sourceAddr) + optionalParametersSize(optional)
}

// cancelBroadcastSMBodySize covers cancel_broadcast_sm: ServiceType,
// MessageID, and SourceAddr are each a C-Octet String (three terminators);
// SourceAddrTON and SourceAddrNPI are two fixed one-octet fields. See
// encodeCancelBroadcastSM.
func cancelBroadcastSMBodySize(serviceType, messageID, sourceAddr []byte, optional []protocol.OptionalParameter) int {
	return 5 + len(serviceType) + len(messageID) + len(sourceAddr) + optionalParametersSize(optional)
}
