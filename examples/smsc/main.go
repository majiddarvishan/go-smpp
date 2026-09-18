package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync/atomic"
	"time"

	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/server"
	"github.com/majiddarvishan/go-smpp/session"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var ids atomic.Uint64
	cfg := server.Config{
		Network:     "tcp",
		Address:     ":2775",
		MaxSessions: 1000,
		SessionConfig: session.Config{
			Profile:             protocol.SMPP34Profile(),
			ResponseTimeout:     10 * time.Second,
			SessionInitTimeout:  15 * time.Second,
			EnquireLinkInterval: 30 * time.Second,
			InactivityTimeout:   2 * time.Minute,
		},
		Authenticator: server.AuthenticatorFunc(func(_ context.Context, _ *session.Session, _ session.BindMode, req protocol.BindRequest) (server.BindResult, error) {
			if !bytes.Equal(req.SystemID, []byte("esme")) || !bytes.Equal(req.Password, []byte("secret")) {
				return server.BindResult{Status: protocol.StatusBindFailed}, nil
			}
			return server.BindResult{Status: protocol.StatusOK, SystemID: []byte("go-smpp-smsc")}, nil
		}),
		SubmitHandler: server.SubmitHandlerFunc(func(_ context.Context, _ *session.Session, req protocol.SubmitSM) (server.SubmitResult, error) {
			id := ids.Add(1)
			fmt.Printf("submit from=%s to=%s bytes=%d\n", req.SourceAddr, req.DestinationAddr, len(req.ShortMessage))
			return server.SubmitResult{
				Status:    protocol.StatusOK,
				MessageID: []byte(fmt.Sprintf("%d", id)),
			}, nil
		}),
	}

	if err := server.ListenAndServe(ctx, cfg); err != nil {
		log.Fatal(err)
	}
}
