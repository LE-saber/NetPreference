package netpref

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fixtureUpstream uses real UDP and TCP sockets; tests do not use public DNS.
func fixtureUpstream(t *testing.T, fn func([]byte, string) []byte) string {
	t.Helper()
	u, e := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if e != nil {
		t.Fatal(e)
	}
	tcp, e := net.Listen("tcp4", u.LocalAddr().String())
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		b := make([]byte, 65535)
		for {
			n, a, e := u.ReadFromUDP(b)
			if e != nil {
				return
			}
			raw := append([]byte(nil), b[:n]...)
			wg.Add(1)
			go func() {
				defer wg.Done()
				ans := fn(raw, "udp")
				if ans != nil {
					_, _ = u.WriteToUDP(ans, a)
				}
			}()
		}
	}()
	go func() {
		defer wg.Done()
		for {
			c, e := tcp.Accept()
			if e != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(3 * time.Second))
				var h [2]byte
				if _, e := io.ReadFull(c, h[:]); e != nil {
					return
				}
				b := make([]byte, int(binary.BigEndian.Uint16(h[:])))
				if _, e := io.ReadFull(c, b); e != nil {
					return
				}
				ans := fn(b, "tcp")
				if ans != nil {
					binary.BigEndian.PutUint16(h[:], uint16(len(ans)))
					_, _ = c.Write(append(h[:], ans...))
				}
			}()
		}
	}()
	t.Cleanup(func() { u.Close(); tcp.Close(); wg.Wait() })
	return u.LocalAddr().String()
}
func normalAnswer(raw []byte, _ string) []byte {
	q, e := ValidateQuery(raw)
	if e != nil {
		return ErrorReply(raw, 1)
	}
	var ips []netip.Addr
	if q.Type == 1 {
		ips = []netip.Addr{netip.MustParseAddr("198.51.100.7")}
	} else if q.Type == 28 {
		ips = []netip.Addr{netip.MustParseAddr("2001:db8::7")}
	}
	return Reply(q, 0, ips, 60)
}
func TestDNSWireRoundTrip(t *testing.T) {
	for _, typ := range []uint16{1, 28, 65} {
		raw := MakeQuery("Mixed.Example", typ)
		q, e := ValidateQuery(raw)
		if e != nil || q.Name != "mixed.example" {
			t.Fatal(q, e)
		}
		b := normalAnswer(raw, "udp")
		if e = ValidateResponse(b, q); e != nil {
			t.Fatal(e)
		}
		found, _ := AddressEvidence(b, q)
		if found != (typ == 1 || typ == 28) {
			t.Fatal("address evidence mismatch")
		}
	}
}
func TestDNSMalformedAndCompression(t *testing.T) {
	raw := MakeQuery("example.com", 1)
	q, _ := ValidateQuery(raw)
	bad := append([]byte(nil), raw...)
	bad[12] = 0xc0
	bad[13] = 12
	if _, e := ValidateQuery(bad); e == nil {
		t.Fatal("compression cycle accepted")
	}
	bad = append([]byte(nil), raw...)
	put16(bad, 4, 2)
	if _, e := ValidateQuery(bad); e == nil {
		t.Fatal("multi-question accepted")
	}
	reply := normalAnswer(raw, "udp")
	reply[0] ^= 1
	if e := ValidateResponse(reply, q); e == nil {
		t.Fatal("wrong ID accepted")
	}
	reply = normalAnswer(raw, "udp")
	put16(reply, 6, 65535)
	if e := ValidateResponse(reply, q); e == nil {
		t.Fatal("truncated RRs accepted")
	}
}
func TestEDNSAndTruncation(t *testing.T) {
	raw := MakeQuery("large.example", 1)
	put16(raw, 10, 1)
	raw = append(raw, 0, 0, 41, 0x10, 0, 0, 0, 0x80, 0, 0, 0)
	q, e := ValidateQuery(raw)
	if e != nil || q.UDPLimit != 4096 {
		t.Fatal(q, e)
	}
	b := TruncateReply(q, Reply(q, 0, nil, 60))
	if u16(b, 2)&0x0200 == 0 || u16(b, 6) != 0 {
		t.Fatal("bad TC reply")
	}
}
func TestUDPToTCPFallback(t *testing.T) {
	var tcpCount atomic.Int64
	up := fixtureUpstream(t, func(raw []byte, proto string) []byte {
		if proto == "udp" {
			q, _ := ValidateQuery(raw)
			return TruncateReply(q, Reply(q, 0, nil, 0))
		}
		tcpCount.Add(1)
		return normalAnswer(raw, proto)
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	raw := MakeQuery("tcp.example", 1)
	b, e := Exchange(ctx, raw, up)
	if e != nil {
		t.Fatal(e)
	}
	if u16(b, 6) != 1 || tcpCount.Load() != 1 {
		t.Fatal("TCP fallback missing")
	}
}
func TestDNSSECPassThroughAndLocalAD(t *testing.T) {
	raw := MakeQuery("signed.example", 1)
	q, _ := ValidateQuery(raw)
	q.Flags |= 0x0020
	b := Reply(q, 0, []netip.Addr{netip.MustParseAddr("192.0.2.1")}, 60)
	if u16(b, 2)&0x0020 != 0 {
		t.Fatal("local answer falsely authenticated")
	}
	up := fixtureUpstream(t, func(raw []byte, p string) []byte { b := normalAnswer(raw, p); put16(b, 2, u16(b, 2)|0x0020); return b })
	c := testConfig()
	c.Upstream = up
	e := NewEngine(c)
	b = e.Handle(context.Background(), raw, testMAC)
	if u16(b, 2)&0x0020 == 0 {
		t.Fatal("unmodified upstream AD lost")
	}
}
func TestActualUDPAndTCPService(t *testing.T) {
	up := fixtureUpstream(t, normalAnswer)
	c := testConfig()
	c.Upstream = up
	e := NewEngine(c)
	s, err := ListenDNS("127.0.0.1:0", e, func(net.IP) string { return testMAC })
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, prefix := range []string{"udp://", "tcp://"} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		b, err := Exchange(ctx, MakeQuery("service.example", 28), prefix+s.Addr())
		cancel()
		if err != nil || u16(b, 6) != 1 {
			t.Fatal(prefix, err)
		}
	}
}
func TestTCPPipeliningPreferredFirst(t *testing.T) {
	up := fixtureUpstream(t, normalAnswer)
	c := testConfig()
	c.Upstream = up
	c.Devices[0].Mode = "ipv6"
	c.Devices[0].WaitMS = 150
	c.Devices[0].DelayMS = 100
	c.Devices[0].Probe = false
	e := NewEngine(c)
	s, err := ListenDNS("127.0.0.1:0", e, func(net.IP) string { return testMAC })
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	conn, err := net.Dial("tcp", s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	a := MakeQuery("pipelined.example", 1)
	aaaa := MakeQuery("pipelined.example", 28)
	put16(a, 0, 100)
	put16(aaaa, 0, 101)
	for _, raw := range [][]byte{a, aaaa} {
		h := []byte{byte(len(raw) >> 8), byte(len(raw))}
		if _, err = conn.Write(append(h, raw...)); err != nil {
			t.Fatal(err)
		}
	}
	read := func() []byte {
		var h [2]byte
		if _, e := io.ReadFull(conn, h[:]); e != nil {
			t.Fatal(e)
		}
		b := make([]byte, int(u16(h[:], 0)))
		if _, e := io.ReadFull(conn, b); e != nil {
			t.Fatal(e)
		}
		return b
	}
	first := read()
	second := read()
	if u16(first, 0) != 101 || u16(second, 0) != 100 {
		t.Fatalf("AAAA must precede delayed A even on one TCP stream, got %d then %d", u16(first, 0), u16(second, 0))
	}
}
func TestServiceStopsIdleTCP(t *testing.T) {
	e := NewEngine(testConfig())
	s, err := ListenDNS("127.0.0.1:0", e, func(net.IP) string { return testMAC })
	if err != nil {
		t.Fatal(err)
	}
	c, err := net.Dial("tcp", s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	time.Sleep(10 * time.Millisecond)
	done := make(chan struct{})
	go func() { s.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown stalled on idle TCP")
	}
}
func FuzzDNSWire(f *testing.F) {
	f.Add(MakeQuery("example.com", 1))
	f.Add([]byte{0, 1, 2})
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 65535 {
			return
		}
		q, e := ValidateQuery(b)
		if e == nil {
			_ = Reply(q, 0, nil, 0)
			_ = ValidateResponse(b, q)
			_, _ = AddressEvidence(b, q)
		}
		_ = ErrorReply(b, 1)
	})
}
