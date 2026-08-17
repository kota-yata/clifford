package dnsforwarder

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"
)

const localTTL = 60

var localNames = map[string]struct{}{
	"blog.kota-yata.com.": {},
	"www.kota-yata.com.":  {},
}

type Forwarder struct {
	upstream     string
	localAddress net.IP
	timeout      time.Duration
}

func New(upstream, localAddress string, timeout time.Duration) (*Forwarder, error) {
	ip := net.ParseIP(localAddress)
	if ip == nil || ip.To4() == nil {
		return nil, fmt.Errorf("local address must be an IPv4 address: %q", localAddress)
	}
	return &Forwarder{upstream: upstream, localAddress: ip.To4(), timeout: timeout}, nil
}

func (f *Forwarder) ServeDNS(w dns.ResponseWriter, request *dns.Msg) {
	if response := f.localResponse(request); response != nil {
		writeResponse(w, response)
		return
	}

	network := "udp"
	if _, ok := w.RemoteAddr().(*net.TCPAddr); ok {
		network = "tcp"
	}

	response, err := f.exchange(request, network)
	if err != nil {
		log.Printf("forward DNS query to %s: %v", f.upstream, err)
		response = new(dns.Msg)
		response.SetRcode(request, dns.RcodeServerFailure)
		response.RecursionAvailable = true
	}
	writeResponse(w, response)
}

func (f *Forwarder) exchange(request *dns.Msg, network string) (*dns.Msg, error) {
	ctx, cancel := context.WithTimeout(context.Background(), f.timeout)
	defer cancel()

	client := &dns.Client{Net: network, Timeout: f.timeout}
	response, _, err := client.ExchangeContext(ctx, request.Copy(), f.upstream)
	if err != nil {
		return nil, err
	}

	if network == "udp" && response.Truncated {
		tcpClient := &dns.Client{Net: "tcp", Timeout: f.timeout}
		response, _, err = tcpClient.ExchangeContext(ctx, request.Copy(), f.upstream)
		if err != nil {
			return nil, err
		}
	}

	return response, nil
}

func (f *Forwarder) localResponse(request *dns.Msg) *dns.Msg {
	if len(request.Question) != 1 {
		return nil
	}

	question := request.Question[0]
	if question.Qclass != dns.ClassINET || question.Qtype != dns.TypeA {
		return nil
	}
	if _, ok := localNames[strings.ToLower(dns.Fqdn(question.Name))]; !ok {
		return nil
	}

	response := new(dns.Msg)
	response.SetReply(request)
	response.RecursionAvailable = true
	response.Answer = []dns.RR{&dns.A{
		Hdr: dns.RR_Header{
			Name:   question.Name,
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    localTTL,
		},
		A: f.localAddress,
	}}
	return response
}

func writeResponse(w dns.ResponseWriter, response *dns.Msg) {
	if err := w.WriteMsg(response); err != nil {
		log.Printf("write DNS response: %v", err)
	}
}
