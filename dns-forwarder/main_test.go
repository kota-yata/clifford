package main

import (
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestLocalResponse(t *testing.T) {
	for _, name := range []string{
		"blog.kota-yata.com.",
		"www.kota-yata.com.",
		"WWW.KOTA-YATA.COM.",
	} {
		t.Run(name, func(t *testing.T) {
			request := new(dns.Msg)
			request.SetQuestion(name, dns.TypeA)

			response := localResponse(request)
			if response == nil {
				t.Fatal("localResponse() returned nil")
			}
			if len(response.Answer) != 1 {
				t.Fatalf("answer count = %d, want 1", len(response.Answer))
			}
			answer, ok := response.Answer[0].(*dns.A)
			if !ok {
				t.Fatalf("answer type = %T, want *dns.A", response.Answer[0])
			}
			if got := answer.A.String(); got != localAddress {
				t.Fatalf("address = %s, want %s", got, localAddress)
			}
		})
	}
}

func TestNonMatchingQueriesAreForwarded(t *testing.T) {
	var requests atomic.Int64
	upstream := startTestUpstream(t, dns.HandlerFunc(func(w dns.ResponseWriter, request *dns.Msg) {
		requests.Add(1)
		response := new(dns.Msg)
		response.SetReply(request)
		response.Answer = []dns.RR{&dns.A{
			Hdr: dns.RR_Header{
				Name:   request.Question[0].Name,
				Rrtype: dns.TypeA,
				Class:  dns.ClassINET,
				Ttl:    300,
			},
			A: net.ParseIP("198.51.100.7").To4(),
		}}
		if err := w.WriteMsg(response); err != nil {
			t.Errorf("write upstream response: %v", err)
		}
	}))

	handler := &forwarder{upstream: upstream, timeout: time.Second}
	for _, test := range []struct {
		name  string
		qtype uint16
	}{
		{name: "other.kota-yata.com.", qtype: dns.TypeA},
		{name: "www.kota-yata.com.", qtype: dns.TypeAAAA},
	} {
		request := new(dns.Msg)
		request.SetQuestion(test.name, test.qtype)
		writer := &messageWriter{remote: &net.UDPAddr{IP: net.ParseIP("127.0.0.1")}}

		handler.ServeDNS(writer, request)
		if writer.message == nil {
			t.Fatal("forwarder wrote no response")
		}
		if writer.message.Rcode != dns.RcodeSuccess {
			t.Fatalf("response code = %s", dns.RcodeToString[writer.message.Rcode])
		}
	}

	if got := requests.Load(); got != 2 {
		t.Fatalf("upstream request count = %d, want 2", got)
	}
}

func startTestUpstream(t *testing.T, handler dns.Handler) string {
	t.Helper()

	packetConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for test upstream: %v", err)
	}
	server := &dns.Server{PacketConn: packetConn, Handler: handler}
	go func() {
		if err := server.ActivateAndServe(); err != nil {
			t.Errorf("serve test upstream: %v", err)
		}
	}()
	t.Cleanup(func() {
		if err := server.Shutdown(); err != nil {
			t.Errorf("shut down test upstream: %v", err)
		}
	})
	return packetConn.LocalAddr().String()
}

type messageWriter struct {
	message *dns.Msg
	remote  net.Addr
}

func (w *messageWriter) LocalAddr() net.Addr            { return &net.UDPAddr{} }
func (w *messageWriter) RemoteAddr() net.Addr           { return w.remote }
func (w *messageWriter) Close() error                   { return nil }
func (w *messageWriter) TsigStatus() error              { return nil }
func (w *messageWriter) TsigTimersOnly(bool)            {}
func (w *messageWriter) Hijack()                        {}
func (w *messageWriter) Write(data []byte) (int, error) { return len(data), nil }
func (w *messageWriter) WriteMsg(message *dns.Msg) error {
	w.message = message.Copy()
	return nil
}
