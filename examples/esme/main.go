package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/majiddarvishan/go-smpp/client"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/session"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	c, err := client.Dial(ctx, client.Config{
		Network: "tcp",
		Address: "127.0.0.1:2775",
		SessionConfig: session.Config{
			Profile:             protocol.SMPP34Profile(),
			ResponseTimeout:     10 * time.Second,
			SessionInitTimeout:  15 * time.Second,
			EnquireLinkInterval: 30 * time.Second,
			EnquireLinkTimeout:  10 * time.Second,
			InactivityTimeout:   2 * time.Minute,
		},
		Reconnect: client.ReconnectPolicy{
			Enabled:        true,
			InitialBackoff: 250 * time.Millisecond,
			MaxBackoff:     5 * time.Second,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	_, err = c.BindTransceiver(ctx, protocol.BindRequest{
		SystemID:         []byte("esme"),
		Password:         []byte("secret"),
		InterfaceVersion: protocol.InterfaceVersion34,
		AddressTON:       protocol.TONUnknown,
		AddressNPI:       protocol.NPIUnknown,
	})
	if err != nil {
		log.Fatal(err)
	}

	resp, err := c.SubmitSM(ctx, protocol.SubmitSM{
		SourceAddrTON:   protocol.TONInternational,
		SourceAddrNPI:   protocol.NPIISDN,
		SourceAddr:      []byte("12025550100"),
		DestAddrTON:     protocol.TONInternational,
		DestAddrNPI:     protocol.NPIISDN,
		DestinationAddr: []byte("12025550101"),
		DataCoding:      protocol.DataCodingSMSCDefault,
		ShortMessage:    []byte("hello from go-smpp"),
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("message_id=%s
", resp.MessageID)
}
