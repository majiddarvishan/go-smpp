package codec

import (
	"fmt"

	"github.com/majiddarvishan/go-smpp/protocol"
)

// NewSMPP50RegistryBuilder returns a mutable registry containing the complete
// SMPP 3.4 command/TLV set plus the SMPP 5.0 additions. The same codec and
// session core is shared between versions; only the registry/capability surface
// is extended.
func NewSMPP50RegistryBuilder(mode RegistryMode) (*RegistryBuilder, error) {
	builder, err := NewSMPP34RegistryBuilder(mode)
	if err != nil {
		return nil, err
	}
	if err := registerSMPP50Commands(builder); err != nil {
		return nil, err
	}
	if err := registerSMPP50TLVs(builder); err != nil {
		return nil, err
	}
	return builder, nil
}

// NewSMPP50Registry returns an immutable SMPP 5.0-aware registry snapshot.
func NewSMPP50Registry(mode RegistryMode) (*Registry, error) {
	builder, err := NewSMPP50RegistryBuilder(mode)
	if err != nil {
		return nil, err
	}
	return builder.Freeze()
}

func registerSMPP50Commands(builder *RegistryBuilder) error {
	definitions := []CommandDefinition{
		{ID: protocol.CommandBroadcastSM, Name: "broadcast_sm", Decode: decodeBroadcastSM, Encode: encodeBroadcastSM},
		{ID: protocol.CommandBroadcastSMResp, Name: "broadcast_sm_resp", Decode: decodeBroadcastSMResp, Encode: encodeBroadcastSMResp},
		{ID: protocol.CommandQueryBroadcastSM, Name: "query_broadcast_sm", Decode: decodeQueryBroadcastSM, Encode: encodeQueryBroadcastSM},
		{ID: protocol.CommandQueryBroadcastSMResp, Name: "query_broadcast_sm_resp", Decode: decodeQueryBroadcastSMResp, Encode: encodeQueryBroadcastSMResp},
		{ID: protocol.CommandCancelBroadcastSM, Name: "cancel_broadcast_sm", Decode: decodeCancelBroadcastSM, Encode: encodeCancelBroadcastSM},
		{ID: protocol.CommandCancelBroadcastSMResp, Name: "cancel_broadcast_sm_resp", Decode: decodeResponseEmpty, Encode: encodeResponseEmpty},
	}
	for _, def := range definitions {
		if err := builder.RegisterCommand(def); err != nil {
			return err
		}
	}
	return nil
}

func registerSMPP50TLVs(builder *RegistryBuilder) error {
	definitions := []TLVDefinition{
		{Tag: protocol.TLVTagCongestionState, Name: "congestion_state"},
		{Tag: protocol.TLVTagBroadcastChannelIndicator, Name: "broadcast_channel_indicator"},
		{Tag: protocol.TLVTagBroadcastContentType, Name: "broadcast_content_type"},
		{Tag: protocol.TLVTagBroadcastContentTypeInfo, Name: "broadcast_content_type_info"},
		{Tag: protocol.TLVTagBroadcastMessageClass, Name: "broadcast_message_class"},
		{Tag: protocol.TLVTagBroadcastRepNum, Name: "broadcast_rep_num"},
		{Tag: protocol.TLVTagBroadcastFrequencyInterval, Name: "broadcast_frequency_interval"},
		{Tag: protocol.TLVTagBroadcastAreaIdentifier, Name: "broadcast_area_identifier"},
		{Tag: protocol.TLVTagBroadcastErrorStatus, Name: "broadcast_error_status"},
		{Tag: protocol.TLVTagBroadcastAreaSuccess, Name: "broadcast_area_success"},
		{Tag: protocol.TLVTagBroadcastEndTime, Name: "broadcast_end_time"},
		{Tag: protocol.TLVTagBroadcastServiceGroup, Name: "broadcast_service_group"},
		{Tag: protocol.TLVTagBillingIdentification, Name: "billing_identification"},
		{Tag: protocol.TLVTagSourceNetworkID, Name: "source_network_id"},
		{Tag: protocol.TLVTagDestNetworkID, Name: "dest_network_id"},
		{Tag: protocol.TLVTagSourceNodeID, Name: "source_node_id"},
		{Tag: protocol.TLVTagDestNodeID, Name: "dest_node_id"},
		{Tag: protocol.TLVTagDestAddrNPResolution, Name: "dest_addr_np_resolution"},
		{Tag: protocol.TLVTagDestAddrNPInformation, Name: "dest_addr_np_information"},
		{Tag: protocol.TLVTagDestAddrNPCountry, Name: "dest_addr_np_country"},
	}
	for _, def := range definitions {
		if err := builder.RegisterTLV(def); err != nil {
			return err
		}
	}
	return nil
}

func decodeBroadcastSM(header Header, body []byte) (any, error) {
	if err := requestStatusMustBeZero(header); err != nil {
		return nil, err
	}
	r := bodyReader{src: body}
	var out protocol.BroadcastSM
	var err error
	if out.ServiceType, err = r.cString(maxServiceTypeLen); err != nil {
		return nil, err
	}
	ton, err := r.uint8()
	if err != nil {
		return nil, err
	}
	out.SourceAddrTON = protocol.TON(ton)
	npi, err := r.uint8()
	if err != nil {
		return nil, err
	}
	out.SourceAddrNPI = protocol.NPI(npi)
	if out.SourceAddr, err = r.cString(maxAddressLen); err != nil {
		return nil, err
	}
	if out.MessageID, err = r.cString(maxMessageIDLen); err != nil {
		return nil, err
	}
	if out.PriorityFlag, err = r.uint8(); err != nil {
		return nil, err
	}
	if out.ScheduleDeliveryTime, err = r.cString(maxTimeFieldLen); err != nil {
		return nil, err
	}
	if out.ValidityPeriod, err = r.cString(maxTimeFieldLen); err != nil {
		return nil, err
	}
	if out.ReplaceIfPresentFlag, err = r.uint8(); err != nil {
		return nil, err
	}
	coding, err := r.uint8()
	if err != nil {
		return nil, err
	}
	out.DataCoding = protocol.DataCoding(coding)
	if out.SMDefaultMsgID, err = r.uint8(); err != nil {
		return nil, err
	}
	if out.Optional, err = r.optional(); err != nil {
		return nil, err
	}
	return out, nil
}

func encodeBroadcastSM(dst []byte, value any) ([]byte, error) {
	var v protocol.BroadcastSM
	switch typed := value.(type) {
	case protocol.BroadcastSM:
		v = typed
	case *protocol.BroadcastSM:
		if typed == nil {
			return dst, fmt.Errorf("%w: nil BroadcastSM", ErrInvalidPDUValue)
		}
		v = *typed
	default:
		return dst, fmt.Errorf("%w: expected protocol.BroadcastSM, got %T", ErrInvalidPDUValue, value)
	}
	var err error
	if dst, err = AppendCString(dst, v.ServiceType, maxServiceTypeLen); err != nil {
		return dst, err
	}
	dst = append(dst, byte(v.SourceAddrTON), byte(v.SourceAddrNPI))
	if dst, err = AppendCString(dst, v.SourceAddr, maxAddressLen); err != nil {
		return dst, err
	}
	if dst, err = AppendCString(dst, v.MessageID, maxMessageIDLen); err != nil {
		return dst, err
	}
	dst = append(dst, v.PriorityFlag)
	if dst, err = AppendCString(dst, v.ScheduleDeliveryTime, maxTimeFieldLen); err != nil {
		return dst, err
	}
	if dst, err = AppendCString(dst, v.ValidityPeriod, maxTimeFieldLen); err != nil {
		return dst, err
	}
	dst = append(dst, v.ReplaceIfPresentFlag, byte(v.DataCoding), v.SMDefaultMsgID)
	return appendOptional(dst, v.Optional)
}

func decodeBroadcastSMResp(header Header, body []byte) (any, error) {
	if optional, handled, err := decodeErrorResponseOptional(header, body); handled {
		if err != nil {
			return nil, err
		}
		if optional == nil {
			return protocol.BroadcastSMResp{}, nil
		}
		return protocol.OptionalResponse{Optional: optional}, nil
	}
	r := bodyReader{src: body}
	messageID, err := r.cString(maxMessageIDLen)
	if err != nil {
		return nil, err
	}
	optional, err := r.optional()
	if err != nil {
		return nil, err
	}
	return protocol.BroadcastSMResp{MessageID: messageID, Optional: optional}, nil
}

func encodeBroadcastSMResp(dst []byte, value any) ([]byte, error) {
	var v protocol.BroadcastSMResp
	switch typed := value.(type) {
	case nil, protocol.EmptyBody, *protocol.EmptyBody:
		return dst, nil
	case protocol.BroadcastSMResp:
		v = typed
	case protocol.OptionalResponse:
		return appendOptional(dst, typed.Optional)
	case *protocol.OptionalResponse:
		if typed == nil {
			return dst, fmt.Errorf("%w: nil OptionalResponse", ErrInvalidPDUValue)
		}
		return appendOptional(dst, typed.Optional)
	case *protocol.BroadcastSMResp:
		if typed == nil {
			return dst, fmt.Errorf("%w: nil BroadcastSMResp", ErrInvalidPDUValue)
		}
		v = *typed
	default:
		return dst, fmt.Errorf("%w: expected protocol.BroadcastSMResp, OptionalResponse or EmptyBody, got %T", ErrInvalidPDUValue, value)
	}
	var err error
	if dst, err = AppendCString(dst, v.MessageID, maxMessageIDLen); err != nil {
		return dst, err
	}
	return appendOptional(dst, v.Optional)
}

func decodeQueryBroadcastSM(header Header, body []byte) (any, error) {
	if err := requestStatusMustBeZero(header); err != nil {
		return nil, err
	}
	r := bodyReader{src: body}
	var out protocol.QueryBroadcastSM
	var err error
	if out.MessageID, err = r.cString(maxMessageIDLen); err != nil {
		return nil, err
	}
	ton, err := r.uint8()
	if err != nil {
		return nil, err
	}
	out.SourceAddrTON = protocol.TON(ton)
	npi, err := r.uint8()
	if err != nil {
		return nil, err
	}
	out.SourceAddrNPI = protocol.NPI(npi)
	if out.SourceAddr, err = r.cString(maxAddressLen); err != nil {
		return nil, err
	}
	if out.Optional, err = r.optional(); err != nil {
		return nil, err
	}
	return out, nil
}

func encodeQueryBroadcastSM(dst []byte, value any) ([]byte, error) {
	var v protocol.QueryBroadcastSM
	switch typed := value.(type) {
	case protocol.QueryBroadcastSM:
		v = typed
	case *protocol.QueryBroadcastSM:
		if typed == nil {
			return dst, fmt.Errorf("%w: nil QueryBroadcastSM", ErrInvalidPDUValue)
		}
		v = *typed
	default:
		return dst, fmt.Errorf("%w: expected protocol.QueryBroadcastSM, got %T", ErrInvalidPDUValue, value)
	}
	var err error
	if dst, err = AppendCString(dst, v.MessageID, maxMessageIDLen); err != nil {
		return dst, err
	}
	dst = append(dst, byte(v.SourceAddrTON), byte(v.SourceAddrNPI))
	if dst, err = AppendCString(dst, v.SourceAddr, maxAddressLen); err != nil {
		return dst, err
	}
	return appendOptional(dst, v.Optional)
}

func decodeQueryBroadcastSMResp(header Header, body []byte) (any, error) {
	if optional, handled, err := decodeErrorResponseOptional(header, body); handled {
		if err != nil {
			return nil, err
		}
		if optional == nil {
			return protocol.QueryBroadcastSMResp{}, nil
		}
		return protocol.OptionalResponse{Optional: optional}, nil
	}
	r := bodyReader{src: body}
	messageID, err := r.cString(maxMessageIDLen)
	if err != nil {
		return nil, err
	}
	optional, err := r.optional()
	if err != nil {
		return nil, err
	}
	return protocol.QueryBroadcastSMResp{MessageID: messageID, Optional: optional}, nil
}

func encodeQueryBroadcastSMResp(dst []byte, value any) ([]byte, error) {
	var v protocol.QueryBroadcastSMResp
	switch typed := value.(type) {
	case nil, protocol.EmptyBody, *protocol.EmptyBody:
		return dst, nil
	case protocol.QueryBroadcastSMResp:
		v = typed
	case protocol.OptionalResponse:
		return appendOptional(dst, typed.Optional)
	case *protocol.OptionalResponse:
		if typed == nil {
			return dst, fmt.Errorf("%w: nil OptionalResponse", ErrInvalidPDUValue)
		}
		return appendOptional(dst, typed.Optional)
	case *protocol.QueryBroadcastSMResp:
		if typed == nil {
			return dst, fmt.Errorf("%w: nil QueryBroadcastSMResp", ErrInvalidPDUValue)
		}
		v = *typed
	default:
		return dst, fmt.Errorf("%w: expected protocol.QueryBroadcastSMResp, OptionalResponse or EmptyBody, got %T", ErrInvalidPDUValue, value)
	}
	var err error
	if dst, err = AppendCString(dst, v.MessageID, maxMessageIDLen); err != nil {
		return dst, err
	}
	return appendOptional(dst, v.Optional)
}

func decodeCancelBroadcastSM(header Header, body []byte) (any, error) {
	if err := requestStatusMustBeZero(header); err != nil {
		return nil, err
	}
	r := bodyReader{src: body}
	var out protocol.CancelBroadcastSM
	var err error
	if out.ServiceType, err = r.cString(maxServiceTypeLen); err != nil {
		return nil, err
	}
	if out.MessageID, err = r.cString(maxMessageIDLen); err != nil {
		return nil, err
	}
	ton, err := r.uint8()
	if err != nil {
		return nil, err
	}
	out.SourceAddrTON = protocol.TON(ton)
	npi, err := r.uint8()
	if err != nil {
		return nil, err
	}
	out.SourceAddrNPI = protocol.NPI(npi)
	if out.SourceAddr, err = r.cString(maxAddressLen); err != nil {
		return nil, err
	}
	if out.Optional, err = r.optional(); err != nil {
		return nil, err
	}
	return out, nil
}

func encodeCancelBroadcastSM(dst []byte, value any) ([]byte, error) {
	var v protocol.CancelBroadcastSM
	switch typed := value.(type) {
	case protocol.CancelBroadcastSM:
		v = typed
	case *protocol.CancelBroadcastSM:
		if typed == nil {
			return dst, fmt.Errorf("%w: nil CancelBroadcastSM", ErrInvalidPDUValue)
		}
		v = *typed
	default:
		return dst, fmt.Errorf("%w: expected protocol.CancelBroadcastSM, got %T", ErrInvalidPDUValue, value)
	}
	var err error
	if dst, err = AppendCString(dst, v.ServiceType, maxServiceTypeLen); err != nil {
		return dst, err
	}
	if dst, err = AppendCString(dst, v.MessageID, maxMessageIDLen); err != nil {
		return dst, err
	}
	dst = append(dst, byte(v.SourceAddrTON), byte(v.SourceAddrNPI))
	if dst, err = AppendCString(dst, v.SourceAddr, maxAddressLen); err != nil {
		return dst, err
	}
	return appendOptional(dst, v.Optional)
}
