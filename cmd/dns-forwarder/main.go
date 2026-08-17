package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	dnsforwarder "github.com/kota-yata/clifford/dns-forwarder"
	"github.com/miekg/dns"
)

const (
	upstreamServer = "8.8.8.8:53"
	queryTimeout   = 5 * time.Second
)

func run(ctx context.Context, localAddress string) error {
	handler, err := dnsforwarder.New(upstreamServer, localAddress, queryTimeout)
	if err != nil {
		return err
	}
	listenAddress := net.JoinHostPort(localAddress, "53")
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
	localAddress := flag.String("local-address", "", "IPv4 address to listen on and return for local A records")
	flag.Parse()
	if *localAddress == "" {
		log.Fatal("-local-address is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, *localAddress); err != nil {
		log.Fatal(err)
	}
}
