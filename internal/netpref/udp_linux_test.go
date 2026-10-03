//go:build linux

package netpref

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestWildcardUDPReplySource(t *testing.T) {
	up := fixtureUpstream(t, normalAnswer)
	c := testConfig()
	c.Upstream = up
	for _, bind := range []string{"0.0.0.0:0", "[::]:0"} {
		t.Run(bind, func(t *testing.T) {
			s, err := listenFixtureDNS(bind, NewEngine(c), func(net.IP) string { return testMAC })
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			_, port, _ := net.SplitHostPort(s.Addr())
			hosts := []string{"127.0.0.1", "127.0.0.2"}
			if bind == "[::]:0" {
				hosts = append(hosts, "::1")
			}
			for _, host := range hosts {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				b, err := Exchange(ctx, MakeQuery("source.example", 28), net.JoinHostPort(host, port))
				cancel()
				if err != nil {
					t.Fatalf("reply to %s on %s: %v", host, bind, err)
				}
				if u16(b, 6) != 1 {
					t.Fatal("missing answer")
				}
			}
		})
	}
}

func TestPacketInfoRejectsMissingAndMalformed(t *testing.T) {
	for _, raw := range [][]byte{nil, {1}, make([]byte, 20)} {
		if packetInfoReply(raw, net.IPv4(127, 0, 0, 1)) != nil {
			t.Fatal("malformed packet info accepted")
		}
	}
}
