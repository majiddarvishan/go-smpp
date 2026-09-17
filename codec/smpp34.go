package codec

import (
	"fmt"

	"github.com/majiddarvishan/go-smpp/protocol"
)

const (
	maxSystemIDLen        = 16
	maxPasswordLen        = 9
	maxSystemTypeLen      = 13
	maxAddressRangeLen    = 41
	maxServiceTypeLen     = 6
	maxAddressLen         = 21
	maxTimeFieldLen       = 17
	maxMessageIDLen       = 65
	maxShortMessageLength = 254
)

// NewSMPP34RegistryBuilder returns a mutable registry builder preloaded with the
// SMPP 3.4 commands implemented by the current core and the TLVs required by
// the initial submit/deliver/bind path. Applications may register vendor
// commands/TLVs before calling Freeze.
func NewSMPP34RegistryBuilder(mode RegistryMode) (*RegistryBuilder, error) {
	builder := NewRegistryBuilder(mode)
	if err := registerSMPP34Commands(builder); err != nil {
		return nil, err
	}
	if err := registerSMPP34TLVs(builder); err != nil {
		return nil, err
	}
	return builder, nil
}

// NewSMPP34Registry returns an immutable SMPP 3.4 registry snapshot suitable
// for concurrent session hot-path use.
func NewSMPP34Registry(mode RegistryMode) (*Registry, error) {
	builder, err := NewSMPP34RegistryBuilder(mode)
	if err != nil {
		return nil, err
	}
	return builder.Freeze()
}

func registerSMPP34Commands(builder *RegistryBuilder) error {
	definitions := []CommandDefinition{
		{ID: protocol.CommandGenericNACK, Name: "generic_nack", Decode: decodeResponseEmpty, Encode: encodeEmpty},
		{ID: protocol.CommandBindReceiver, Name: "bind_receiver", Decode: decodeBindRequest, Encode: encodeBindRequest},
		{ID: protocol.CommandBindReceiverResp, Name: "bind_receiver_resp", Decode: decodeBindResponse, Encode: encodeBindResponse},
		{ID: protocol.CommandBindTransmitter, Name: "bind_transmitter", Decode: decodeBindRequest, Encode: encodeBindRequest},
		{ID: protocol.CommandBindTransmitterResp, Name: "bind_transmitter_resp", Decode: decodeBindResponse, Encode: encodeBindResponse},
		{ID: protocol.CommandQuerySM, Name: "query_sm", Decode: decodeQuerySM, Encode: encodeQuerySM},
		{ID: protocol.CommandQuerySMResp, Name: "query_sm_resp", Decode: decodeQuerySMResp, Encode: encodeQuerySMResp},
		{ID: protocol.CommandSubmitSM, Name: "submit_sm", Decode: decodeSubmitSM, Encode: encodeSubmitSM},
		{ID: protocol.CommandSubmitSMResp, Name: "submit_sm_resp", Decode: decodeSubmitSMResp, Encode: encodeSubmitSMResp},
		{ID: protocol.CommandDeliverSM, Name: "deliver_sm", Decode: decodeDeliverSM, Encode: encodeDeliverSM},
		{ID: protocol.CommandDeliverSMResp, Name: "deliver_sm_resp", Decode: decodeDeliverSMResp, Encode: encodeDeliverSMResp},
		{ID: protocol.CommandUnbind, Name: "unbind", Decode: decodeRequestEmpty, Encode: encodeEmpty},
		{ID: protocol.CommandUnbindResp, Name: "unbind_resp", Decode: decodeResponseEmpty, Encode: encodeEmpty},
		{ID: protocol.CommandReplaceSM, Name: "replace_sm", Decode: decodeReplaceSM, Encode: encodeReplaceSM},
		{ID: protocol.CommandReplaceSMResp, Name: "replace_sm_resp", Decode: decodeResponseEmpty, Encode: encodeEmpty},
		{ID: protocol.CommandCancelSM, Name: "cancel_sm", Decode: decodeCancelSM, Encode: encodeCancelSM},
		{ID: protocol.CommandCancelSMResp, Name: "cancel_sm_resp", Decode: decodeResponseEmpty, Encode: encodeEmpty},
		{ID: protocol.CommandBindTransceiver, Name: "bind_transceiver", Decode: decodeBindRequest, Encode: encodeBindRequest},
		{ID: protocol.CommandBindTransceiverResp, Name: "bind_transceiver_resp", Decode: decodeBindResponse, Encode: encodeBindResponse},
		{ID: protocol.CommandOutbind, Name: "outbind", Decode: decodeOutbind, Encode: encodeOutbind},
		{ID: protocol.CommandEnquireLink, Name: "enquire_link", Decode: decodeRequestEmpty, Encode: encodeEmpty},
		{ID: protocol.CommandEnquireLinkResp, Name: "enquire_link_resp", Decode: decodeResponseEmpty, Encode: encodeEmpty},
		{ID: protocol.CommandSubmitMulti, Name: "submit_multi", Decode: decodeSubmitMulti, Encode: encodeSubmitMulti},
		{ID: protocol.CommandSubmitMultiResp, Name: "submit_multi_resp", Decode: decodeSubmitMultiResp, Encode: encodeSubmitMultiResp},
		{ID: protocol.CommandAlertNotification, Name: "alert_notification", Decode: decodeAlertNotification, Encode: encodeAlertNotification},
		{ID: protocol.CommandDataSM, Name: "data_sm", Decode: decodeDataSM, Encode: encodeDataSM},
		{ID: protocol.CommandDataSMResp, Name: "data_sm_resp", Decode: decodeDataSMResp, Encode: encodeDataSMResp},
	}
	for _, def := range definitions {
		if err := builder.RegisterCommand(def); err != nil {
			return err
		}
	}
	return nil
}

func registerSMPP34TLVs(builder *RegistryBuilder) error {
	definitions := []TLVDefinition{
		{Tag: protocol.TLVTagDestAddrSubunit, Name: "dest_addr_subunit"},
		{Tag: protocol.TLVTagDestNetworkType, Name: "dest_network_type"},
		{Tag: protocol.TLVTagDestBearerType, Name: "dest_bearer_type"},
		{Tag: protocol.TLVTagDestTelematicsID, Name: "dest_telematics_id"},
		{Tag: protocol.TLVTagSourceAddrSubunit, Name: "source_addr_subunit"},
		{Tag: protocol.TLVTagSourceNetworkType, Name: "source_network_type"},
		{Tag: protocol.TLVTagSourceBearerType, Name: "source_bearer_type"},
		{Tag: protocol.TLVTagSourceTelematicsID, Name: "source_telematics_id"},
		{Tag: protocol.TLVTagQOSTimeToLive, Name: "qos_time_to_live"},
		{Tag: protocol.TLVTagPayloadType, Name: "payload_type"},
		{Tag: protocol.TLVTagAdditionalStatusInfoText, Name: "additional_status_info_text"},
		{Tag: protocol.TLVTagReceiptedMessageID, Name: "receipted_message_id"},
		{Tag: protocol.TLVTagMSMsgWaitFacilities, Name: "ms_msg_wait_facilities"},
		{Tag: protocol.TLVTagPrivacyIndicator, Name: "privacy_indicator"},
		{Tag: protocol.TLVTagSourceSubaddress, Name: "source_subaddress"},
		{Tag: protocol.TLVTagDestSubaddress, Name: "dest_subaddress"},
		{Tag: protocol.TLVTagUserMessageReference, Name: "user_message_reference"},
		{Tag: protocol.TLVTagUserResponseCode, Name: "user_response_code"},
		{Tag: protocol.TLVTagSourcePort, Name: "source_port"},
		{Tag: protocol.TLVTagDestinationPort, Name: "destination_port"},
		{Tag: protocol.TLVTagSARMsgRefNum, Name: "sar_msg_ref_num"},
		{Tag: protocol.TLVTagLanguageIndicator, Name: "language_indicator"},
		{Tag: protocol.TLVTagSARTotalSegments, Name: "sar_total_segments"},
		{Tag: protocol.TLVTagSARSegmentSeqnum, Name: "sar_segment_seqnum"},
		{Tag: protocol.TLVTagSCInterfaceVersion, Name: "sc_interface_version"},
		{Tag: protocol.TLVTagCallbackNumPresInd, Name: "callback_num_pres_ind"},
		{Tag: protocol.TLVTagCallbackNumAtag, Name: "callback_num_atag"},
		{Tag: protocol.TLVTagNumberOfMessages, Name: "number_of_messages"},
		{Tag: protocol.TLVTagCallbackNum, Name: "callback_num"},
		{Tag: protocol.TLVTagDPFResult, Name: "dpf_result"},
		{Tag: protocol.TLVTagSetDPF, Name: "set_dpf"},
		{Tag: protocol.TLVTagMSAvailabilityStatus, Name: "ms_availability_status"},
		{Tag: protocol.TLVTagNetworkErrorCode, Name: "network_error_code"},
		{Tag: protocol.TLVTagMessagePayload, Name: "message_payload"},
		{Tag: protocol.TLVTagDeliveryFailureReason, Name: "delivery_failure_reason"},
		{Tag: protocol.TLVTagMoreMessagesToSend, Name: "more_messages_to_send"},
		{Tag: protocol.TLVTagMessageState, Name: "message_state"},
		{Tag: protocol.TLVTagUSSDServiceOp, Name: "ussd_service_op"},
		{Tag: protocol.TLVTagDisplayTime, Name: "display_time"},
		{Tag: protocol.TLVTagSMSSignal, Name: "sms_signal"},
		{Tag: protocol.TLVTagMSValidity, Name: "ms_validity"},
		{Tag: protocol.TLVTagAlertOnMessageDelivery, Name: "alert_on_message_delivery"},
		{Tag: protocol.TLVTagITSReplyType, Name: "its_reply_type"},
		{Tag: protocol.TLVTagITSSessionInfo, Name: "its_session_info"},
	}
	for _, def := range definitions {
		if err := builder.RegisterTLV(def); err != nil {
			return err
		}
	}
	return nil
}

type bodyReader struct {
	src []byte
	off int
}

func (r *bodyReader) cString(maxEncodedLen int) ([]byte, error) {
	value, n, err := ReadCString(r.src[r.off:], maxEncodedLen)
	if err != nil {
		return nil, err
	}
	r.off += n
	return value, nil
}

func (r *bodyReader) uint8() (uint8, error) {
	value, n, err := ReadUint8(r.src[r.off:])
	if err != nil {
		return 0, err
	}
	r.off += n
	return value, nil
}

func (r *bodyReader) octets(n int) ([]byte, error) {
	value, consumed, err := ReadOctets(r.src[r.off:], n)
	if err != nil {
		return nil, err
	}
	r.off += consumed
	return value, nil
}

func (r *bodyReader) optional() ([]protocol.OptionalParameter, error) {
	if r.off == len(r.src) {
		return nil, nil
	}
	remaining := r.src[r.off:]
	capacity := len(remaining) / TLVHeaderSize
	if capacity > 8 {
		capacity = 8
	}
	parameters := make([]protocol.OptionalParameter, 0, capacity)
	if err := ScanTLVs(remaining, func(tlv TLV) bool {
		parameters = append(parameters, protocol.OptionalParameter{Tag: tlv.Tag, Value: tlv.Value})
		return true
	}); err != nil {
		return nil, err
	}
	r.off = len(r.src)
	return parameters, nil
}

func (r *bodyReader) requireDone() error {
	if r.off != len(r.src) {
		return fmt.Errorf("%w: %d trailing body octets", ErrInvalidPDUValue, len(r.src)-r.off)
	}
	return nil
}

func requestStatusMustBeZero(header Header) error {
	if !header.CommandStatus.OK() {
		return fmt.Errorf("%w: request command_status must be zero", ErrInvalidPDUValue)
	}
	return nil
}

func decodeBindRequest(header Header, body []byte) (any, error) {
	if err := requestStatusMustBeZero(header); err != nil {
		return nil, err
	}
	r := bodyReader{src: body}
	systemID, err := r.cString(maxSystemIDLen)
	if err != nil {
		return nil, err
	}
	password, err := r.cString(maxPasswordLen)
	if err != nil {
		return nil, err
	}
	systemType, err := r.cString(maxSystemTypeLen)
	if err != nil {
		return nil, err
	}
	interfaceVersion, err := r.uint8()
	if err != nil {
		return nil, err
	}
	ton, err := r.uint8()
	if err != nil {
		return nil, err
	}
	npi, err := r.uint8()
	if err != nil {
		return nil, err
	}
	addressRange, err := r.cString(maxAddressRangeLen)
	if err != nil {
		return nil, err
	}
	if err := r.requireDone(); err != nil {
		return nil, err
	}
	return protocol.BindRequest{
		SystemID: systemID, Password: password, SystemType: systemType,
		InterfaceVersion: protocol.InterfaceVersion(interfaceVersion),
		AddressTON:       protocol.TON(ton), AddressNPI: protocol.NPI(npi), AddressRange: addressRange,
	}, nil
}

func encodeBindRequest(dst []byte, value any) ([]byte, error) {
	var v protocol.BindRequest
	switch typed := value.(type) {
	case protocol.BindRequest:
		v = typed
	case *protocol.BindRequest:
		if typed == nil {
			return dst, fmt.Errorf("%w: nil BindRequest", ErrInvalidPDUValue)
		}
		v = *typed
	default:
		return dst, fmt.Errorf("%w: expected protocol.BindRequest, got %T", ErrInvalidPDUValue, value)
	}
	var err error
	if dst, err = AppendCString(dst, v.SystemID, maxSystemIDLen); err != nil {
		return dst, err
	}
	if dst, err = AppendCString(dst, v.Password, maxPasswordLen); err != nil {
		return dst, err
	}
	if dst, err = AppendCString(dst, v.SystemType, maxSystemTypeLen); err != nil {
		return dst, err
	}
	dst = append(dst, byte(v.InterfaceVersion), byte(v.AddressTON), byte(v.AddressNPI))
	if dst, err = AppendCString(dst, v.AddressRange, maxAddressRangeLen); err != nil {
		return dst, err
	}
	return dst, nil
}

func decodeBindResponse(header Header, body []byte) (any, error) {
	if !header.CommandStatus.OK() {
		if len(body) != 0 {
			return nil, fmt.Errorf("%w: unsuccessful bind response must not contain a body", ErrInvalidPDUValue)
		}
		return protocol.BindResponse{}, nil
	}
	r := bodyReader{src: body}
	systemID, err := r.cString(maxSystemIDLen)
	if err != nil {
		return nil, err
	}
	optional, err := r.optional()
	if err != nil {
		return nil, err
	}
	for _, parameter := range optional {
		if parameter.Tag == protocol.TLVTagSCInterfaceVersion && len(parameter.Value) != 1 {
			return nil, fmt.Errorf("%w: sc_interface_version length must be 1", ErrInvalidPDUValue)
		}
	}
	return protocol.BindResponse{SystemID: systemID, Optional: optional}, nil
}

func encodeBindResponse(dst []byte, value any) ([]byte, error) {
	switch typed := value.(type) {
	case nil, protocol.EmptyBody, *protocol.EmptyBody:
		return dst, nil
	case protocol.BindResponse:
		return appendBindResponse(dst, typed)
	case *protocol.BindResponse:
		if typed == nil {
			return dst, fmt.Errorf("%w: nil BindResponse", ErrInvalidPDUValue)
		}
		return appendBindResponse(dst, *typed)
	default:
		return dst, fmt.Errorf("%w: expected protocol.BindResponse or EmptyBody, got %T", ErrInvalidPDUValue, value)
	}
}

func appendBindResponse(dst []byte, v protocol.BindResponse) ([]byte, error) {
	var err error
	if dst, err = AppendCString(dst, v.SystemID, maxSystemIDLen); err != nil {
		return dst, err
	}
	return appendOptional(dst, v.Optional)
}

func decodeRequestEmpty(header Header, body []byte) (any, error) {
	if err := requestStatusMustBeZero(header); err != nil {
		return nil, err
	}
	return decodeEmpty(body)
}

func decodeResponseEmpty(_ Header, body []byte) (any, error) {
	return decodeEmpty(body)
}

func decodeEmpty(body []byte) (any, error) {
	if len(body) != 0 {
		return nil, fmt.Errorf("%w: header-only PDU has %d body octets", ErrInvalidPDUValue, len(body))
	}
	return protocol.EmptyBody{}, nil
}

func encodeEmpty(dst []byte, value any) ([]byte, error) {
	switch value.(type) {
	case nil, protocol.EmptyBody, *protocol.EmptyBody:
		return dst, nil
	default:
		return dst, fmt.Errorf("%w: expected EmptyBody, got %T", ErrInvalidPDUValue, value)
	}
}

type shortMessageBody struct {
	ServiceType          []byte
	SourceAddrTON        protocol.TON
	SourceAddrNPI        protocol.NPI
	SourceAddr           []byte
	DestAddrTON          protocol.TON
	DestAddrNPI          protocol.NPI
	DestinationAddr      []byte
	ESMClass             uint8
	ProtocolID           uint8
	PriorityFlag         uint8
	ScheduleDeliveryTime []byte
	ValidityPeriod       []byte
	RegisteredDelivery   uint8
	ReplaceIfPresentFlag uint8
	DataCoding           protocol.DataCoding
	SMDefaultMsgID       uint8
	ShortMessage         []byte
	Optional             []protocol.OptionalParameter
}

func decodeShortMessageBody(header Header, body []byte) (shortMessageBody, error) {
	if err := requestStatusMustBeZero(header); err != nil {
		return shortMessageBody{}, err
	}
	r := bodyReader{src: body}
	var out shortMessageBody
	var err error
	if out.ServiceType, err = r.cString(maxServiceTypeLen); err != nil {
		return out, err
	}
	ton, err := r.uint8()
	if err != nil {
		return out, err
	}
	out.SourceAddrTON = protocol.TON(ton)
	npi, err := r.uint8()
	if err != nil {
		return out, err
	}
	out.SourceAddrNPI = protocol.NPI(npi)
	if out.SourceAddr, err = r.cString(maxAddressLen); err != nil {
		return out, err
	}
	ton, err = r.uint8()
	if err != nil {
		return out, err
	}
	out.DestAddrTON = protocol.TON(ton)
	npi, err = r.uint8()
	if err != nil {
		return out, err
	}
	out.DestAddrNPI = protocol.NPI(npi)
	if out.DestinationAddr, err = r.cString(maxAddressLen); err != nil {
		return out, err
	}
	if out.ESMClass, err = r.uint8(); err != nil {
		return out, err
	}
	if out.ProtocolID, err = r.uint8(); err != nil {
		return out, err
	}
	if out.PriorityFlag, err = r.uint8(); err != nil {
		return out, err
	}
	if out.ScheduleDeliveryTime, err = r.cString(maxTimeFieldLen); err != nil {
		return out, err
	}
	if out.ValidityPeriod, err = r.cString(maxTimeFieldLen); err != nil {
		return out, err
	}
	if out.RegisteredDelivery, err = r.uint8(); err != nil {
		return out, err
	}
	if out.ReplaceIfPresentFlag, err = r.uint8(); err != nil {
		return out, err
	}
	coding, err := r.uint8()
	if err != nil {
		return out, err
	}
	out.DataCoding = protocol.DataCoding(coding)
	if out.SMDefaultMsgID, err = r.uint8(); err != nil {
		return out, err
	}
	smLength, err := r.uint8()
	if err != nil {
		return out, err
	}
	if smLength == 255 {
		return out, fmt.Errorf("%w: sm_length 255 is not allowed in SMPP 3.4", ErrInvalidPDUValue)
	}
	if out.ShortMessage, err = r.octets(int(smLength)); err != nil {
		return out, err
	}
	if out.Optional, err = r.optional(); err != nil {
		return out, err
	}
	if hasNonEmptyMessagePayload(out.Optional) && len(out.ShortMessage) != 0 {
		return out, ErrConflictingMessageData
	}
	return out, nil
}

func appendShortMessageBody(dst []byte, v shortMessageBody) ([]byte, error) {
	if len(v.ShortMessage) > maxShortMessageLength {
		return dst, fmt.Errorf("%w: short_message length %d exceeds 254", ErrFieldTooLong, len(v.ShortMessage))
	}
	if hasNonEmptyMessagePayload(v.Optional) && len(v.ShortMessage) != 0 {
		return dst, ErrConflictingMessageData
	}
	var err error
	if dst, err = AppendCString(dst, v.ServiceType, maxServiceTypeLen); err != nil {
		return dst, err
	}
	dst = append(dst, byte(v.SourceAddrTON), byte(v.SourceAddrNPI))
	if dst, err = AppendCString(dst, v.SourceAddr, maxAddressLen); err != nil {
		return dst, err
	}
	dst = append(dst, byte(v.DestAddrTON), byte(v.DestAddrNPI))
	if dst, err = AppendCString(dst, v.DestinationAddr, maxAddressLen); err != nil {
		return dst, err
	}
	dst = append(dst, v.ESMClass, v.ProtocolID, v.PriorityFlag)
	if dst, err = AppendCString(dst, v.ScheduleDeliveryTime, maxTimeFieldLen); err != nil {
		return dst, err
	}
	if dst, err = AppendCString(dst, v.ValidityPeriod, maxTimeFieldLen); err != nil {
		return dst, err
	}
	dst = append(dst, v.RegisteredDelivery, v.ReplaceIfPresentFlag, byte(v.DataCoding), v.SMDefaultMsgID, byte(len(v.ShortMessage)))
	dst = append(dst, v.ShortMessage...)
	return appendOptional(dst, v.Optional)
}

func decodeSubmitSM(header Header, body []byte) (any, error) {
	decoded, err := decodeShortMessageBody(header, body)
	if err != nil {
		return nil, err
	}
	return protocol.SubmitSM{
		ServiceType: decoded.ServiceType, SourceAddrTON: decoded.SourceAddrTON, SourceAddrNPI: decoded.SourceAddrNPI,
		SourceAddr: decoded.SourceAddr, DestAddrTON: decoded.DestAddrTON, DestAddrNPI: decoded.DestAddrNPI,
		DestinationAddr: decoded.DestinationAddr, ESMClass: decoded.ESMClass, ProtocolID: decoded.ProtocolID,
		PriorityFlag: decoded.PriorityFlag, ScheduleDeliveryTime: decoded.ScheduleDeliveryTime, ValidityPeriod: decoded.ValidityPeriod,
		RegisteredDelivery: decoded.RegisteredDelivery, ReplaceIfPresentFlag: decoded.ReplaceIfPresentFlag,
		DataCoding: decoded.DataCoding, SMDefaultMsgID: decoded.SMDefaultMsgID, ShortMessage: decoded.ShortMessage, Optional: decoded.Optional,
	}, nil
}

func encodeSubmitSM(dst []byte, value any) ([]byte, error) {
	var v protocol.SubmitSM
	switch typed := value.(type) {
	case protocol.SubmitSM:
		v = typed
	case *protocol.SubmitSM:
		if typed == nil {
			return dst, fmt.Errorf("%w: nil SubmitSM", ErrInvalidPDUValue)
		}
		v = *typed
	default:
		return dst, fmt.Errorf("%w: expected protocol.SubmitSM, got %T", ErrInvalidPDUValue, value)
	}
	return appendShortMessageBody(dst, shortMessageBody{
		ServiceType: v.ServiceType, SourceAddrTON: v.SourceAddrTON, SourceAddrNPI: v.SourceAddrNPI, SourceAddr: v.SourceAddr,
		DestAddrTON: v.DestAddrTON, DestAddrNPI: v.DestAddrNPI, DestinationAddr: v.DestinationAddr,
		ESMClass: v.ESMClass, ProtocolID: v.ProtocolID, PriorityFlag: v.PriorityFlag,
		ScheduleDeliveryTime: v.ScheduleDeliveryTime, ValidityPeriod: v.ValidityPeriod,
		RegisteredDelivery: v.RegisteredDelivery, ReplaceIfPresentFlag: v.ReplaceIfPresentFlag,
		DataCoding: v.DataCoding, SMDefaultMsgID: v.SMDefaultMsgID, ShortMessage: v.ShortMessage, Optional: v.Optional,
	})
}

func decodeDeliverSM(header Header, body []byte) (any, error) {
	decoded, err := decodeShortMessageBody(header, body)
	if err != nil {
		return nil, err
	}
	return protocol.DeliverSM{
		ServiceType: decoded.ServiceType, SourceAddrTON: decoded.SourceAddrTON, SourceAddrNPI: decoded.SourceAddrNPI,
		SourceAddr: decoded.SourceAddr, DestAddrTON: decoded.DestAddrTON, DestAddrNPI: decoded.DestAddrNPI,
		DestinationAddr: decoded.DestinationAddr, ESMClass: decoded.ESMClass, ProtocolID: decoded.ProtocolID,
		PriorityFlag: decoded.PriorityFlag, ScheduleDeliveryTime: decoded.ScheduleDeliveryTime, ValidityPeriod: decoded.ValidityPeriod,
		RegisteredDelivery: decoded.RegisteredDelivery, ReplaceIfPresentFlag: decoded.ReplaceIfPresentFlag,
		DataCoding: decoded.DataCoding, SMDefaultMsgID: decoded.SMDefaultMsgID, ShortMessage: decoded.ShortMessage, Optional: decoded.Optional,
	}, nil
}

func encodeDeliverSM(dst []byte, value any) ([]byte, error) {
	var v protocol.DeliverSM
	switch typed := value.(type) {
	case protocol.DeliverSM:
		v = typed
	case *protocol.DeliverSM:
		if typed == nil {
			return dst, fmt.Errorf("%w: nil DeliverSM", ErrInvalidPDUValue)
		}
		v = *typed
	default:
		return dst, fmt.Errorf("%w: expected protocol.DeliverSM, got %T", ErrInvalidPDUValue, value)
	}
	return appendShortMessageBody(dst, shortMessageBody{
		ServiceType: v.ServiceType, SourceAddrTON: v.SourceAddrTON, SourceAddrNPI: v.SourceAddrNPI, SourceAddr: v.SourceAddr,
		DestAddrTON: v.DestAddrTON, DestAddrNPI: v.DestAddrNPI, DestinationAddr: v.DestinationAddr,
		ESMClass: v.ESMClass, ProtocolID: v.ProtocolID, PriorityFlag: v.PriorityFlag,
		ScheduleDeliveryTime: v.ScheduleDeliveryTime, ValidityPeriod: v.ValidityPeriod,
		RegisteredDelivery: v.RegisteredDelivery, ReplaceIfPresentFlag: v.ReplaceIfPresentFlag,
		DataCoding: v.DataCoding, SMDefaultMsgID: v.SMDefaultMsgID, ShortMessage: v.ShortMessage, Optional: v.Optional,
	})
}

func decodeSubmitSMResp(header Header, body []byte) (any, error) {
	if !header.CommandStatus.OK() {
		if len(body) != 0 {
			return nil, fmt.Errorf("%w: unsuccessful submit_sm_resp must not contain a body", ErrInvalidPDUValue)
		}
		return protocol.SubmitSMResp{}, nil
	}
	r := bodyReader{src: body}
	messageID, err := r.cString(maxMessageIDLen)
	if err != nil {
		return nil, err
	}
	if err := r.requireDone(); err != nil {
		return nil, err
	}
	return protocol.SubmitSMResp{MessageID: messageID}, nil
}

func encodeSubmitSMResp(dst []byte, value any) ([]byte, error) {
	switch typed := value.(type) {
	case nil, protocol.EmptyBody, *protocol.EmptyBody:
		return dst, nil
	case protocol.SubmitSMResp:
		return AppendCString(dst, typed.MessageID, maxMessageIDLen)
	case *protocol.SubmitSMResp:
		if typed == nil {
			return dst, fmt.Errorf("%w: nil SubmitSMResp", ErrInvalidPDUValue)
		}
		return AppendCString(dst, typed.MessageID, maxMessageIDLen)
	default:
		return dst, fmt.Errorf("%w: expected protocol.SubmitSMResp or EmptyBody, got %T", ErrInvalidPDUValue, value)
	}
}

func decodeDeliverSMResp(_ Header, body []byte) (any, error) {
	// SMPP 3.4 specifies a one-octet NULL message_id. Accept a header-only
	// response too for deployed-peer interoperability, while still validating a
	// present C-Octet String structurally.
	if len(body) == 0 {
		return protocol.DeliverSMResp{}, nil
	}
	r := bodyReader{src: body}
	messageID, err := r.cString(maxMessageIDLen)
	if err != nil {
		return nil, err
	}
	if err := r.requireDone(); err != nil {
		return nil, err
	}
	return protocol.DeliverSMResp{MessageID: messageID}, nil
}

func encodeDeliverSMResp(dst []byte, value any) ([]byte, error) {
	var messageID []byte
	switch typed := value.(type) {
	case nil, protocol.EmptyBody, *protocol.EmptyBody:
		messageID = nil
	case protocol.DeliverSMResp:
		messageID = typed.MessageID
	case *protocol.DeliverSMResp:
		if typed == nil {
			return dst, fmt.Errorf("%w: nil DeliverSMResp", ErrInvalidPDUValue)
		}
		messageID = typed.MessageID
	default:
		return dst, fmt.Errorf("%w: expected protocol.DeliverSMResp, got %T", ErrInvalidPDUValue, value)
	}
	return AppendCString(dst, messageID, maxMessageIDLen)
}

func appendOptional(dst []byte, optional []protocol.OptionalParameter) ([]byte, error) {
	var err error
	for _, parameter := range optional {
		dst, err = AppendTLV(dst, parameter.Tag, parameter.Value)
		if err != nil {
			return dst, err
		}
	}
	return dst, nil
}

func hasNonEmptyMessagePayload(optional []protocol.OptionalParameter) bool {
	for _, parameter := range optional {
		if parameter.Tag == protocol.TLVTagMessagePayload && len(parameter.Value) != 0 {
			return true
		}
	}
	return false
}
