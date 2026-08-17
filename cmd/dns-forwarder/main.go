package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	dnsforwarder "github.com/kota-yata/clifford/dns-forwarder"
	"github.com/miekg/dns"
)

const (
	listenAddress  = ":53"
	upstreamServer = "8.8.8.8:53"
	queryTimeout   = 5 * time.Second
)

func run(ctx context.Context) error {
	handler := dnsforwarder.New(upstreamServer, queryTimeout)
	servers := []*dns.Server{
		{Addr: listenAddress, Net: "udp", Handler: handler},
		{Addr: listenAddress, Net: "tcp", Handler: handler},
	}

	serverErrors := make(chan error, len(servers))
	for _, server := range servers {
		server := server
		go func() {
			serverErrors <- server.ListenAndServe()
		}()
	}

	var runErr error
	select {
	case err := <-serverErrors:
		runErr = fmt.Errorf("start DNS server: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()
	for _, server := range servers {
		if err := server.ShutdownContext(shutdownCtx); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("shut down DNS server: %w", err))
		}
	}
	return runErr
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}
