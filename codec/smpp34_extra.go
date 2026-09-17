package codec

import (
	"encoding/binary"
	"fmt"

	"github.com/majiddarvishan/go-smpp/protocol"
)

const (
	maxDataAddressLen = 65
	maxDLNameLen      = 21
)

func (r *bodyReader) uint32() (uint32, error) {
	value, n, err := ReadUint32(r.src[r.off:])
	if err != nil {
		return 0, err
	}
	r.off += n
	return value, nil
}

func decodeDataSM(header Header, body []byte) (any, error) {
	if err := requestStatusMustBeZero(header); err != nil {
		return nil, err
	}
	r := bodyReader{src: body}
	var out protocol.DataSM
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
	if out.SourceAddr, err = r.cString(maxDataAddressLen); err != nil {
		return nil, err
	}
	ton, err = r.uint8()
	if err != nil {
		return nil, err
	}
	out.DestAddrTON = protocol.TON(ton)
	npi, err = r.uint8()
	if err != nil {
		return nil, err
	}
	out.DestAddrNPI = protocol.NPI(npi)
	if out.DestinationAddr, err = r.cString(maxDataAddressLen); err != nil {
		return nil, err
	}
	if out.ESMClass, err = r.uint8(); err != nil {
		return nil, err
	}
	if out.RegisteredDelivery, err = r.uint8(); err != nil {
		return nil, err
	}
	coding, err := r.uint8()
	if err != nil {
		return nil, err
	}
	out.DataCoding = protocol.DataCoding(coding)
	if out.Optional, err = r.optional(); err != nil {
		return nil, err
	}
	return out, nil
}

func encodeDataSM(dst []byte, value any) ([]byte, error) {
	var v protocol.DataSM
	switch typed := value.(type) {
	case protocol.DataSM:
		v = typed
	case *protocol.DataSM:
		if typed == nil {
			return dst, fmt.Errorf("%w: nil DataSM", ErrInvalidPDUValue)
		}
		v = *typed
	default:
		return dst, fmt.Errorf("%w: expected protocol.DataSM, got %T", ErrInvalidPDUValue, value)
	}
	var err error
	if dst, err = AppendCString(dst, v.ServiceType, maxServiceTypeLen); err != nil {
		return dst, err
	}
	dst = append(dst, byte(v.SourceAddrTON), byte(v.SourceAddrNPI))
	if dst, err = AppendCString(dst, v.SourceAddr, maxDataAddressLen); err != nil {
		return dst, err
	}
	dst = append(dst, byte(v.DestAddrTON), byte(v.DestAddrNPI))
	if dst, err = AppendCString(dst, v.DestinationAddr, maxDataAddressLen); err != nil {
		return dst, err
	}
	dst = append(dst, v.ESMClass, v.RegisteredDelivery, byte(v.DataCoding))
	return appendOptional(dst, v.Optional)
}

func decodeDataSMResp(header Header, body []byte) (any, error) {
	if len(body) == 0 && !header.CommandStatus.OK() {
		return protocol.DataSMResp{}, nil
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
	return protocol.DataSMResp{MessageID: messageID, Optional: optional}, nil
}

func encodeDataSMResp(dst []byte, value any) ([]byte, error) {
	switch typed := value.(type) {
	case nil, protocol.EmptyBody, *protocol.EmptyBody:
		return dst, nil
	case protocol.DataSMResp:
		return appendDataSMResp(dst, typed)
	case *protocol.DataSMResp:
		if typed == nil {
			return dst, fmt.Errorf("%w: nil DataSMResp", ErrInvalidPDUValue)
		}
		return appendDataSMResp(dst, *typed)
	default:
		return dst, fmt.Errorf("%w: expected protocol.DataSMResp or EmptyBody, got %T", ErrInvalidPDUValue, value)
	}
}

func appendDataSMResp(dst []byte, v protocol.DataSMResp) ([]byte, error) {
	var err error
	if dst, err = AppendCString(dst, v.MessageID, maxMessageIDLen); err != nil {
		return dst, err
	}
	return appendOptional(dst, v.Optional)
}

func decodeQuerySM(header Header, body []byte) (any, error) {
	if err := requestStatusMustBeZero(header); err != nil {
		return nil, err
	}
	r := bodyReader{src: body}
	messageID, err := r.cString(maxMessageIDLen)
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
	sourceAddr, err := r.cString(maxAddressLen)
	if err != nil {
		return nil, err
	}
	if err := r.requireDone(); err != nil {
		return nil, err
	}
	return protocol.QuerySM{MessageID: messageID, SourceAddrTON: protocol.TON(ton), SourceAddrNPI: protocol.NPI(npi), SourceAddr: sourceAddr}, nil
}

func encodeQuerySM(dst []byte, value any) ([]byte, error) {
	var v protocol.QuerySM
	switch typed := value.(type) {
	case protocol.QuerySM:
		v = typed
	case *protocol.QuerySM:
		if typed == nil {
			return dst, fmt.Errorf("%w: nil QuerySM", ErrInvalidPDUValue)
		}
		v = *typed
	default:
		return dst, fmt.Errorf("%w: expected protocol.QuerySM, got %T", ErrInvalidPDUValue, value)
	}
	var err error
	if dst, err = AppendCString(dst, v.MessageID, maxMessageIDLen); err != nil {
		return dst, err
	}
	dst = append(dst, byte(v.SourceAddrTON), byte(v.SourceAddrNPI))
	return AppendCString(dst, v.SourceAddr, maxAddressLen)
}

func decodeQuerySMResp(header Header, body []byte) (any, error) {
	if len(body) == 0 && !header.CommandStatus.OK() {
		return protocol.QuerySMResp{}, nil
	}
	r := bodyReader{src: body}
	messageID, err := r.cString(maxMessageIDLen)
	if err != nil {
		return nil, err
	}
	finalDate, err := r.cString(maxTimeFieldLen)
	if err != nil {
		return nil, err
	}
	state, err := r.uint8()
	if err != nil {
		return nil, err
	}
	errorCode, err := r.uint8()
	if err != nil {
		return nil, err
	}
	if err := r.requireDone(); err != nil {
		return nil, err
	}
	return protocol.QuerySMResp{MessageID: messageID, FinalDate: finalDate, MessageState: protocol.MessageState(state), ErrorCode: errorCode}, nil
}

func encodeQuerySMResp(dst []byte, value any) ([]byte, error) {
	var v protocol.QuerySMResp
	switch typed := value.(type) {
	case nil, protocol.EmptyBody, *protocol.EmptyBody:
		return dst, nil
	case protocol.QuerySMResp:
		v = typed
	case *protocol.QuerySMResp:
		if typed == nil {
			return dst, fmt.Errorf("%w: nil QuerySMResp", ErrInvalidPDUValue)
		}
		v = *typed
	default:
		return dst, fmt.Errorf("%w: expected protocol.QuerySMResp or EmptyBody, got %T", ErrInvalidPDUValue, value)
	}
	var err error
	if dst, err = AppendCString(dst, v.MessageID, maxMessageIDLen); err != nil {
		return dst, err
	}
	if dst, err = AppendCString(dst, v.FinalDate, maxTimeFieldLen); err != nil {
		return dst, err
	}
	return append(dst, byte(v.MessageState), v.ErrorCode), nil
}

func decodeCancelSM(header Header, body []byte) (any, error) {
	if err := requestStatusMustBeZero(header); err != nil {
		return nil, err
	}
	r := bodyReader{src: body}
	var out protocol.CancelSM
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
	ton, err = r.uint8()
	if err != nil {
		return nil, err
	}
	out.DestAddrTON = protocol.TON(ton)
	npi, err = r.uint8()
	if err != nil {
		return nil, err
	}
	out.DestAddrNPI = protocol.NPI(npi)
	if out.DestinationAddr, err = r.cString(maxAddressLen); err != nil {
		return nil, err
	}
	if err := r.requireDone(); err != nil {
		return nil, err
	}
	return out, nil
}

func encodeCancelSM(dst []byte, value any) ([]byte, error) {
	var v protocol.CancelSM
	switch typed := value.(type) {
	case protocol.CancelSM:
		v = typed
	case *protocol.CancelSM:
		if typed == nil {
			return dst, fmt.Errorf("%w: nil CancelSM", ErrInvalidPDUValue)
		}
		v = *typed
	default:
		return dst, fmt.Errorf("%w: expected protocol.CancelSM, got %T", ErrInvalidPDUValue, value)
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
	dst = append(dst, byte(v.DestAddrTON), byte(v.DestAddrNPI))
	return AppendCString(dst, v.DestinationAddr, maxAddressLen)
}

func decodeReplaceSM(header Header, body []byte) (any, error) {
	if err := requestStatusMustBeZero(header); err != nil {
		return nil, err
	}
	r := bodyReader{src: body}
	var out protocol.ReplaceSM
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
	if out.ScheduleDeliveryTime, err = r.cString(maxTimeFieldLen); err != nil {
		return nil, err
	}
	if out.ValidityPeriod, err = r.cString(maxTimeFieldLen); err != nil {
		return nil, err
	}
	if out.RegisteredDelivery, err = r.uint8(); err != nil {
		return nil, err
	}
	if out.SMDefaultMsgID, err = r.uint8(); err != nil {
		return nil, err
	}
	smLength, err := r.uint8()
	if err != nil {
		return nil, err
	}
	if smLength == 255 {
		return nil, fmt.Errorf("%w: sm_length 255 is not allowed in SMPP 3.4", ErrInvalidPDUValue)
	}
	if out.ShortMessage, err = r.octets(int(smLength)); err != nil {
		return nil, err
	}
	if err := r.requireDone(); err != nil {
		return nil, err
	}
	return out, nil
}

func encodeReplaceSM(dst []byte, value any) ([]byte, error) {
	var v protocol.ReplaceSM
	switch typed := value.(type) {
	case protocol.ReplaceSM:
		v = typed
	case *protocol.ReplaceSM:
		if typed == nil {
			return dst, fmt.Errorf("%w: nil ReplaceSM", ErrInvalidPDUValue)
		}
		v = *typed
	default:
		return dst, fmt.Errorf("%w: expected protocol.ReplaceSM, got %T", ErrInvalidPDUValue, value)
	}
	if len(v.ShortMessage) > maxShortMessageLength {
		return dst, fmt.Errorf("%w: short_message length %d exceeds 254", ErrFieldTooLong, len(v.ShortMessage))
	}
	var err error
	if dst, err = AppendCString(dst, v.MessageID, maxMessageIDLen); err != nil {
		return dst, err
	}
	dst = append(dst, byte(v.SourceAddrTON), byte(v.SourceAddrNPI))
	if dst, err = AppendCString(dst, v.SourceAddr, maxAddressLen); err != nil {
		return dst, err
	}
	if dst, err = AppendCString(dst, v.ScheduleDeliveryTime, maxTimeFieldLen); err != nil {
		return dst, err
	}
	if dst, err = AppendCString(dst, v.ValidityPeriod, maxTimeFieldLen); err != nil {
		return dst, err
	}
	dst = append(dst, v.RegisteredDelivery, v.SMDefaultMsgID, byte(len(v.ShortMessage)))
	return append(dst, v.ShortMessage...), nil
}

func decodeAlertNotification(header Header, body []byte) (any, error) {
	if err := requestStatusMustBeZero(header); err != nil {
		return nil, err
	}
	r := bodyReader{src: body}
	var out protocol.AlertNotification
	var err error
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
	if out.SourceAddr, err = r.cString(maxDataAddressLen); err != nil {
		return nil, err
	}
	ton, err = r.uint8()
	if err != nil {
		return nil, err
	}
	out.ESMEAddrTON = protocol.TON(ton)
	npi, err = r.uint8()
	if err != nil {
		return nil, err
	}
	out.ESMEAddrNPI = protocol.NPI(npi)
	if out.ESMEAddr, err = r.cString(maxDataAddressLen); err != nil {
		return nil, err
	}
	if out.Optional, err = r.optional(); err != nil {
		return nil, err
	}
	return out, nil
}

func encodeAlertNotification(dst []byte, value any) ([]byte, error) {
	var v protocol.AlertNotification
	switch typed := value.(type) {
	case protocol.AlertNotification:
		v = typed
	case *protocol.AlertNotification:
		if typed == nil {
			return dst, fmt.Errorf("%w: nil AlertNotification", ErrInvalidPDUValue)
		}
		v = *typed
	default:
		return dst, fmt.Errorf("%w: expected protocol.AlertNotification, got %T", ErrInvalidPDUValue, value)
	}
	var err error
	dst = append(dst, byte(v.SourceAddrTON), byte(v.SourceAddrNPI))
	if dst, err = AppendCString(dst, v.SourceAddr, maxDataAddressLen); err != nil {
		return dst, err
	}
	dst = append(dst, byte(v.ESMEAddrTON), byte(v.ESMEAddrNPI))
	if dst, err = AppendCString(dst, v.ESMEAddr, maxDataAddressLen); err != nil {
		return dst, err
	}
	return appendOptional(dst, v.Optional)
}

func decodeOutbind(header Header, body []byte) (any, error) {
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
	if err := r.requireDone(); err != nil {
		return nil, err
	}
	return protocol.Outbind{SystemID: systemID, Password: password}, nil
}

func encodeOutbind(dst []byte, value any) ([]byte, error) {
	var v protocol.Outbind
	switch typed := value.(type) {
	case protocol.Outbind:
		v = typed
	case *protocol.Outbind:
		if typed == nil {
			return dst, fmt.Errorf("%w: nil Outbind", ErrInvalidPDUValue)
		}
		v = *typed
	default:
		return dst, fmt.Errorf("%w: expected protocol.Outbind, got %T", ErrInvalidPDUValue, value)
	}
	var err error
	if dst, err = AppendCString(dst, v.SystemID, maxSystemIDLen); err != nil {
		return dst, err
	}
	return AppendCString(dst, v.Password, maxPasswordLen)
}

func decodeSubmitMulti(header Header, body []byte) (any, error) {
	if err := requestStatusMustBeZero(header); err != nil {
		return nil, err
	}
	r := bodyReader{src: body}
	var out protocol.SubmitMulti
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
	count, err := r.uint8()
	if err != nil {
		return nil, err
	}
	if count == 0 || count == 255 {
		return nil, fmt.Errorf("%w: number_of_dests must be 1..254", ErrInvalidPDUValue)
	}
	out.Destinations = make([]protocol.SubmitMultiDestination, 0, int(count))
	for i := 0; i < int(count); i++ {
		flag, err := r.uint8()
		if err != nil {
			return nil, err
		}
		destination := protocol.SubmitMultiDestination{Flag: protocol.DestinationFlag(flag)}
		switch destination.Flag {
		case protocol.DestinationSME:
			ton, err := r.uint8()
			if err != nil {
				return nil, err
			}
			destination.TON = protocol.TON(ton)
			npi, err := r.uint8()
			if err != nil {
				return nil, err
			}
			destination.NPI = protocol.NPI(npi)
			if destination.Address, err = r.cString(maxAddressLen); err != nil {
				return nil, err
			}
		case protocol.DestinationDistributionList:
			if destination.Address, err = r.cString(maxDLNameLen); err != nil {
				return nil, err
			}
		default:
			return nil, &protocol.FatalError{
				Kind:           protocol.FatalFrameBoundaryLost,
				DeclaredLength: header.CommandLength,
				Command:        header.CommandID,
				Sequence:       header.SequenceNumber,
				Reason:         fmt.Sprintf("unknown submit_multi dest_flag %d makes destination structure ambiguous", flag),
			}
		}
		out.Destinations = append(out.Destinations, destination)
	}
	if out.ESMClass, err = r.uint8(); err != nil {
		return nil, err
	}
	if out.ProtocolID, err = r.uint8(); err != nil {
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
	if out.RegisteredDelivery, err = r.uint8(); err != nil {
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
	smLength, err := r.uint8()
	if err != nil {
		return nil, err
	}
	if smLength == 255 {
		return nil, fmt.Errorf("%w: sm_length 255 is not allowed in SMPP 3.4", ErrInvalidPDUValue)
	}
	if out.ShortMessage, err = r.octets(int(smLength)); err != nil {
		return nil, err
	}
	if out.Optional, err = r.optional(); err != nil {
		return nil, err
	}
	if hasNonEmptyMessagePayload(out.Optional) && len(out.ShortMessage) != 0 {
		return nil, ErrConflictingMessageData
	}
	return out, nil
}

func encodeSubmitMulti(dst []byte, value any) ([]byte, error) {
	var v protocol.SubmitMulti
	switch typed := value.(type) {
	case protocol.SubmitMulti:
		v = typed
	case *protocol.SubmitMulti:
		if typed == nil {
			return dst, fmt.Errorf("%w: nil SubmitMulti", ErrInvalidPDUValue)
		}
		v = *typed
	default:
		return dst, fmt.Errorf("%w: expected protocol.SubmitMulti, got %T", ErrInvalidPDUValue, value)
	}
	if len(v.Destinations) == 0 || len(v.Destinations) > 254 {
		return dst, fmt.Errorf("%w: submit_multi destinations must be 1..254", ErrInvalidPDUValue)
	}
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
	dst = append(dst, byte(len(v.Destinations)))
	for _, destination := range v.Destinations {
		dst = append(dst, byte(destination.Flag))
		switch destination.Flag {
		case protocol.DestinationSME:
			dst = append(dst, byte(destination.TON), byte(destination.NPI))
			if dst, err = AppendCString(dst, destination.Address, maxAddressLen); err != nil {
				return dst, err
			}
		case protocol.DestinationDistributionList:
			if dst, err = AppendCString(dst, destination.Address, maxDLNameLen); err != nil {
				return dst, err
			}
		default:
			return dst, fmt.Errorf("%w: invalid submit_multi dest_flag %d", ErrInvalidPDUValue, destination.Flag)
		}
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

func decodeSubmitMultiResp(header Header, body []byte) (any, error) {
	if len(body) == 0 && !header.CommandStatus.OK() {
		return protocol.SubmitMultiResp{}, nil
	}
	r := bodyReader{src: body}
	messageID, err := r.cString(maxMessageIDLen)
	if err != nil {
		return nil, err
	}
	count, err := r.uint8()
	if err != nil {
		return nil, err
	}
	unsuccessful := make([]protocol.UnsuccessfulSME, 0, int(count))
	for i := 0; i < int(count); i++ {
		ton, err := r.uint8()
		if err != nil {
			return nil, err
		}
		npi, err := r.uint8()
		if err != nil {
			return nil, err
		}
		address, err := r.cString(maxAddressLen)
		if err != nil {
			return nil, err
		}
		status, err := r.uint32()
		if err != nil {
			return nil, err
		}
		unsuccessful = append(unsuccessful, protocol.UnsuccessfulSME{
			DestAddrTON: protocol.TON(ton), DestAddrNPI: protocol.NPI(npi), DestinationAddr: address, ErrorStatusCode: protocol.CommandStatus(status),
		})
	}
	if err := r.requireDone(); err != nil {
		return nil, err
	}
	return protocol.SubmitMultiResp{MessageID: messageID, Unsuccessful: unsuccessful}, nil
}

func encodeSubmitMultiResp(dst []byte, value any) ([]byte, error) {
	var v protocol.SubmitMultiResp
	switch typed := value.(type) {
	case nil, protocol.EmptyBody, *protocol.EmptyBody:
		return dst, nil
	case protocol.SubmitMultiResp:
		v = typed
	case *protocol.SubmitMultiResp:
		if typed == nil {
			return dst, fmt.Errorf("%w: nil SubmitMultiResp", ErrInvalidPDUValue)
		}
		v = *typed
	default:
		return dst, fmt.Errorf("%w: expected protocol.SubmitMultiResp or EmptyBody, got %T", ErrInvalidPDUValue, value)
	}
	if len(v.Unsuccessful) > 255 {
		return dst, fmt.Errorf("%w: no_unsuccess exceeds 255", ErrInvalidPDUValue)
	}
	var err error
	if dst, err = AppendCString(dst, v.MessageID, maxMessageIDLen); err != nil {
		return dst, err
	}
	dst = append(dst, byte(len(v.Unsuccessful)))
	for _, unsuccessful := range v.Unsuccessful {
		dst = append(dst, byte(unsuccessful.DestAddrTON), byte(unsuccessful.DestAddrNPI))
		if dst, err = AppendCString(dst, unsuccessful.DestinationAddr, maxAddressLen); err != nil {
			return dst, err
		}
		dst = binary.BigEndian.AppendUint32(dst, uint32(unsuccessful.ErrorStatusCode))
	}
	return dst, nil
}
