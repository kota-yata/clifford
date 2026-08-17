package dnsforwarder

import (
	"context"
	"log"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"
)

const (
	localAddress = "192.168.11.48"
	localTTL     = 60
)

var localNames = map[string]struct{}{
	"blog.kota-yata.com.": {},
	"www.kota-yata.com.":  {},
}

type Forwarder struct {
	upstream string
	timeout  time.Duration
}

func New(upstream string, timeout time.Duration) *Forwarder {
	return &Forwarder{upstream: upstream, timeout: timeout}
}

func (f *Forwarder) ServeDNS(w dns.ResponseWriter, request *dns.Msg) {
	if response := localResponse(request); response != nil {
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

func localResponse(request *dns.Msg) *dns.Msg {
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
		A: net.ParseIP(localAddress).To4(),
	}}
	return response
}

func writeResponse(w dns.ResponseWriter, response *dns.Msg) {
	if err := w.WriteMsg(response); err != nil {
		log.Printf("write DNS response: %v", err)
	}
}
