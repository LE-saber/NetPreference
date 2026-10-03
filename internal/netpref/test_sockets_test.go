package netpref

import (
	"errors"
	"fmt"
	"net"
	"syscall"
	"testing"
)

const fixtureBindAttempts = 16

// Test-only allocation: TCP and UDP have separate port namespaces. Reserve the
// TCP ephemeral port first, then bind UDP without releasing that reservation.
// Retry only EADDRINUSE during setup; never retry queries or their assertions.
func fixtureSocketPair(openTCP func() (net.Listener, error), openUDP func(net.Addr) (*net.UDPConn, error)) (*net.UDPConn, net.Listener, error) {
	for i := 0; i < fixtureBindAttempts; i++ {
		tcp, err := openTCP()
		if err != nil {
			return nil, nil, err
		}
		udp, err := openUDP(tcp.Addr())
		if err == nil {
			return udp, tcp, nil
		}
		tcp.Close()
		if !errors.Is(err, syscall.EADDRINUSE) {
			return nil, nil, err
		}
	}
	return nil, nil, fmt.Errorf("fixture exhausted %d port reservations: %w", fixtureBindAttempts, syscall.EADDRINUSE)
}

func listenFixtureUpstream() (*net.UDPConn, net.Listener, error) {
	return fixtureSocketPair(func() (net.Listener, error) {
		return net.Listen("tcp4", "127.0.0.1:0")
	}, func(addr net.Addr) (*net.UDPConn, error) {
		a, err := net.ResolveUDPAddr("udp4", addr.String())
		if err != nil {
			return nil, err
		}
		return net.ListenUDP("udp4", a)
	})
}

// The production listener deliberately fails on a occupied fixed port. Tests
// request port 0 instead, so only transient allocation collisions may be retried.
func listenFixtureDNS(addr string, e *Engine, identify func(net.IP) string) (*DNSService, error) {
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port != "0" {
		return nil, fmt.Errorf("fixture listener requires an ephemeral port: %q", addr)
	}
	for i := 0; i < fixtureBindAttempts; i++ {
		s, err := ListenDNS(addr, e, identify)
		if !errors.Is(err, syscall.EADDRINUSE) {
			return s, err
		}
	}
	return nil, fmt.Errorf("fixture DNS exhausted port reservations: %w", syscall.EADDRINUSE)
}

type reservationFixture struct{ closed bool }

func (*reservationFixture) Accept() (net.Conn, error) { return nil, errors.New("unused accept") }
func (r *reservationFixture) Close() error            { r.closed = true; return nil }
func (*reservationFixture) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}
}

func TestFixturePortCollisionReleasesReservation(t *testing.T) {
	var reservations []*reservationFixture
	udp := new(net.UDPConn) // Sentinel; this allocation test does not perform IO.
	u, tcp, err := fixtureSocketPair(func() (net.Listener, error) {
		r := new(reservationFixture)
		reservations = append(reservations, r)
		return r, nil
	}, func(net.Addr) (*net.UDPConn, error) {
		if len(reservations) == 1 {
			return nil, &net.OpError{Op: "listen", Net: "udp4", Err: syscall.EADDRINUSE}
		}
		return udp, nil
	})
	if err != nil || u != udp || len(reservations) != 2 || tcp != reservations[1] {
		t.Fatalf("wrong retry result: %v, reservations=%d", err, len(reservations))
	}
	if !reservations[0].closed || reservations[1].closed {
		t.Fatal("failed reservation leaked, or successful reservation closed early")
	}
	tcp.Close()
}

func TestFixturePortAllocationBoundsAndOtherErrors(t *testing.T) {
	for _, failure := range []error{syscall.EADDRINUSE, syscall.EACCES} {
		var reservations []*reservationFixture
		u, tcp, err := fixtureSocketPair(func() (net.Listener, error) {
			r := new(reservationFixture)
			reservations = append(reservations, r)
			return r, nil
		}, func(net.Addr) (*net.UDPConn, error) { return nil, failure })
		want := 1
		if failure == syscall.EADDRINUSE {
			want = fixtureBindAttempts
		}
		if u != nil || tcp != nil || !errors.Is(err, failure) || len(reservations) != want {
			t.Fatalf("unexpected retry/return: %v, reservations=%d", err, len(reservations))
		}
		for _, r := range reservations {
			if !r.closed {
				t.Fatal("failed socket reservation leaked")
			}
		}
	}
	_, _, err := fixtureSocketPair(func() (net.Listener, error) { return nil, syscall.EACCES },
		func(net.Addr) (*net.UDPConn, error) { t.Fatal("UDP called after TCP failure"); return nil, nil })
	if !errors.Is(err, syscall.EACCES) {
		t.Fatal(err)
	}
}

func TestFixtureServiceRejectsFixedPort(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:1053", "invalid"} {
		if s, err := listenFixtureDNS(addr, nil, nil); err == nil || s != nil {
			t.Fatal("test-only retry helper must not hide fixed port conflicts", addr)
		}
	}
}
