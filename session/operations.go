package session

import (
	"context"
	"fmt"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
)

func (s *Session) BindTransmitter(ctx context.Context, request protocol.BindRequest) (protocol.BindResponse, error) {
	return s.bind(ctx, protocol.CommandBindTransmitter, request)
}

func (s *Session) BindReceiver(ctx context.Context, request protocol.BindRequest) (protocol.BindResponse, error) {
	return s.bind(ctx, protocol.CommandBindReceiver, request)
}

func (s *Session) BindTransceiver(ctx context.Context, request protocol.BindRequest) (protocol.BindResponse, error) {
	return s.bind(ctx, protocol.CommandBindTransceiver, request)
}

func (s *Session) bind(ctx context.Context, command protocol.CommandID, request protocol.BindRequest) (protocol.BindResponse, error) {
	pdu, err := s.Request(ctx, command, request)
	if err != nil {
		return protocol.BindResponse{}, err
	}
	if err := responseError(pdu); err != nil {
		return protocol.BindResponse{}, err
	}
	body, ok := pdu.Body.(protocol.BindResponse)
	if !ok {
		return protocol.BindResponse{}, fmt.Errorf("%w: bind response body %T", ErrUnexpectedPDU, pdu.Body)
	}
	return body, nil
}

func (s *Session) SubmitSM(ctx context.Context, request protocol.SubmitSM) (protocol.SubmitSMResp, error) {
	pdu, err := s.Request(ctx, protocol.CommandSubmitSM, request)
	if err != nil {
		return protocol.SubmitSMResp{}, err
	}
	if err := responseError(pdu); err != nil {
		return protocol.SubmitSMResp{}, err
	}
	body, ok := pdu.Body.(protocol.SubmitSMResp)
	if !ok {
		return protocol.SubmitSMResp{}, fmt.Errorf("%w: submit_sm_resp body %T", ErrUnexpectedPDU, pdu.Body)
	}
	return body, nil
}

func (s *Session) DeliverSM(ctx context.Context, request protocol.DeliverSM) (protocol.DeliverSMResp, error) {
	pdu, err := s.Request(ctx, protocol.CommandDeliverSM, request)
	if err != nil {
		return protocol.DeliverSMResp{}, err
	}
	if err := responseError(pdu); err != nil {
		return protocol.DeliverSMResp{}, err
	}
	body, ok := pdu.Body.(protocol.DeliverSMResp)
	if !ok {
		return protocol.DeliverSMResp{}, fmt.Errorf("%w: deliver_sm_resp body %T", ErrUnexpectedPDU, pdu.Body)
	}
	return body, nil
}

func (s *Session) DataSM(ctx context.Context, request protocol.DataSM) (protocol.DataSMResp, error) {
	pdu, err := s.Request(ctx, protocol.CommandDataSM, request)
	if err != nil {
		return protocol.DataSMResp{}, err
	}
	if err := responseError(pdu); err != nil {
		return protocol.DataSMResp{}, err
	}
	body, ok := pdu.Body.(protocol.DataSMResp)
	if !ok {
		return protocol.DataSMResp{}, fmt.Errorf("%w: data_sm_resp body %T", ErrUnexpectedPDU, pdu.Body)
	}
	return body, nil
}

func (s *Session) SubmitMulti(ctx context.Context, request protocol.SubmitMulti) (protocol.SubmitMultiResp, error) {
	pdu, err := s.Request(ctx, protocol.CommandSubmitMulti, request)
	if err != nil {
		return protocol.SubmitMultiResp{}, err
	}
	if err := responseError(pdu); err != nil {
		return protocol.SubmitMultiResp{}, err
	}
	body, ok := pdu.Body.(protocol.SubmitMultiResp)
	if !ok {
		return protocol.SubmitMultiResp{}, fmt.Errorf("%w: submit_multi_resp body %T", ErrUnexpectedPDU, pdu.Body)
	}
	return body, nil
}

func (s *Session) QuerySM(ctx context.Context, request protocol.QuerySM) (protocol.QuerySMResp, error) {
	pdu, err := s.Request(ctx, protocol.CommandQuerySM, request)
	if err != nil {
		return protocol.QuerySMResp{}, err
	}
	if err := responseError(pdu); err != nil {
		return protocol.QuerySMResp{}, err
	}
	body, ok := pdu.Body.(protocol.QuerySMResp)
	if !ok {
		return protocol.QuerySMResp{}, fmt.Errorf("%w: query_sm_resp body %T", ErrUnexpectedPDU, pdu.Body)
	}
	return body, nil
}

func (s *Session) CancelSM(ctx context.Context, request protocol.CancelSM) error {
	pdu, err := s.Request(ctx, protocol.CommandCancelSM, request)
	if err != nil {
		return err
	}
	return responseError(pdu)
}

func (s *Session) ReplaceSM(ctx context.Context, request protocol.ReplaceSM) error {
	pdu, err := s.Request(ctx, protocol.CommandReplaceSM, request)
	if err != nil {
		return err
	}
	return responseError(pdu)
}

func (s *Session) BroadcastSM(ctx context.Context, request protocol.BroadcastSM) (protocol.BroadcastSMResp, error) {
	pdu, err := s.Request(ctx, protocol.CommandBroadcastSM, request)
	if err != nil {
		return protocol.BroadcastSMResp{}, err
	}
	if err := responseError(pdu); err != nil {
		return protocol.BroadcastSMResp{}, err
	}
	body, ok := pdu.Body.(protocol.BroadcastSMResp)
	if !ok {
		return protocol.BroadcastSMResp{}, fmt.Errorf("%w: broadcast_sm_resp body %T", ErrUnexpectedPDU, pdu.Body)
	}
	return body, nil
}

func (s *Session) QueryBroadcastSM(ctx context.Context, request protocol.QueryBroadcastSM) (protocol.QueryBroadcastSMResp, error) {
	pdu, err := s.Request(ctx, protocol.CommandQueryBroadcastSM, request)
	if err != nil {
		return protocol.QueryBroadcastSMResp{}, err
	}
	if err := responseError(pdu); err != nil {
		return protocol.QueryBroadcastSMResp{}, err
	}
	body, ok := pdu.Body.(protocol.QueryBroadcastSMResp)
	if !ok {
		return protocol.QueryBroadcastSMResp{}, fmt.Errorf("%w: query_broadcast_sm_resp body %T", ErrUnexpectedPDU, pdu.Body)
	}
	return body, nil
}

func (s *Session) CancelBroadcastSM(ctx context.Context, request protocol.CancelBroadcastSM) error {
	pdu, err := s.Request(ctx, protocol.CommandCancelBroadcastSM, request)
	if err != nil {
		return err
	}
	return responseError(pdu)
}

func (s *Session) AlertNotification(ctx context.Context, notification protocol.AlertNotification) error {
	return s.SendOneWay(ctx, protocol.CommandAlertNotification, notification)
}

func (s *Session) Outbind(ctx context.Context, request protocol.Outbind) error {
	return s.SendOneWay(ctx, protocol.CommandOutbind, request)
}

func (s *Session) EnquireLink(ctx context.Context) error {
	pdu, err := s.Request(ctx, protocol.CommandEnquireLink, protocol.EmptyBody{})
	if err != nil {
		return err
	}
	return responseError(pdu)
}

// tryEnquireLink is the liveness-supervision variant of EnquireLink. It never
// waits for window capacity: the supervisor must not be parked behind
// application traffic, because a keepalive that blocks cannot detect the very
// stall it exists to detect.
func (s *Session) tryEnquireLink(ctx context.Context) error {
	pdu, err := s.TryRequest(ctx, protocol.CommandEnquireLink, protocol.EmptyBody{})
	if err != nil {
		return err
	}
	return responseError(pdu)
}

func (s *Session) Unbind(ctx context.Context) error {
	pdu, err := s.Request(ctx, protocol.CommandUnbind, protocol.EmptyBody{})
	if err != nil {
		return err
	}
	if err := responseError(pdu); err != nil {
		return err
	}
	return nil
}

func responseError(pdu codec.DecodedPDU) error {
	if pdu.Header.CommandID == protocol.CommandGenericNACK || !pdu.Header.CommandStatus.OK() {
		return &ResponseError{Command: pdu.Header.CommandID, Status: pdu.Header.CommandStatus, Sequence: pdu.Header.SequenceNumber}
	}
	return nil
}
