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

func run(ctx context.Context, bindAddress, answerAddress string) error {
	if net.ParseIP(bindAddress) == nil {
		return fmt.Errorf("bind address must be an IP address: %q", bindAddress)
	}
	handler, err := dnsforwarder.New(upstreamServer, answerAddress, queryTimeout)
	if err != nil {
		return err
	}
	listenAddress := net.JoinHostPort(bindAddress, "53")
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
	bindAddress := flag.String("bind-address", "", "IP address on which the DNS server listens")
	answerAddress := flag.String("answer-address", "", "IPv4 address returned for overridden A records")
	flag.Parse()
	if *bindAddress == "" {
		log.Fatal("-bind-address is required")
	}
	if *answerAddress == "" {
		log.Fatal("-answer-address is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, *bindAddress, *answerAddress); err != nil {
		log.Fatal(err)
	}
}
