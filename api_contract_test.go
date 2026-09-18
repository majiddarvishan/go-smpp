package smpp_test

import (
	"context"
	"net"

	"github.com/majiddarvishan/go-smpp/client"
	"github.com/majiddarvishan/go-smpp/codec"
	smppencoding "github.com/majiddarvishan/go-smpp/encoding"
	"github.com/majiddarvishan/go-smpp/message"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/server"
	"github.com/majiddarvishan/go-smpp/session"
)

var (
	_ func(context.Context, client.Config) (*client.Client, error) = client.Dial
	_ func(net.Conn, session.Config) (*client.Client, error)       = client.New

	_ = (*client.Client).BindTransceiver
	_ = (*client.Client).SubmitSM
	_ = (*client.Client).Close

	_ func(context.Context, server.Config) (*server.Server, error) = server.Listen
	_ func(context.Context, server.Config) error                  = server.ListenAndServe
	_ = (*server.Server).Serve
	_ = (*server.Server).DeliverSM
	_ = (*server.Server).Close

	_ func(net.Conn, session.Config) (*session.Session, error) = session.New
	_ = (*session.Session).Request
	_ = (*session.Session).TryRequest
	_ = (*session.Session).SubmitSM
	_ = (*session.Session).DeliverSM
	_ = (*session.Session).Metrics
	_ = (*session.Session).Close

	_ = codec.NewSMPP34RegistryBuilder
	_ = codec.NewSMPP50RegistryBuilder
	_ = (*codec.RegistryBuilder).RegisterCommand
	_ = (*codec.RegistryBuilder).RegisterTLV
	_ = (*codec.RegistryBuilder).Freeze

	_ = smppencoding.EncodeGSM7
	_ = smppencoding.EncodeUCS2
	_ = smppencoding.EncodeUTF16BE
	_ = message.EncodeText
	_ = message.SegmentTextUDH

	_ protocol.CommandID
)
