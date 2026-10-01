package netpref

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"time"
)

type DNSService struct {
	Engine   *Engine
	Identify func(net.IP) string
	udp      *net.UDPConn
	tcp      net.Listener
	slots    chan struct{}
	mu       sync.Mutex
	perIP    map[string]int
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
}

func ListenDNS(addr string, e *Engine, identify func(net.IP) string) (*DNSService, error) {
	udpaddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	u, err := net.ListenUDP("udp", udpaddr)
	if err != nil {
		return nil, err
	}
	// Port 0 is used by integration tests; TCP must bind the chosen UDP port.
	t, err := net.Listen("tcp", u.LocalAddr().String())
	if err != nil {
		u.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &DNSService{Engine: e, Identify: identify, udp: u, tcp: t, slots: make(chan struct{}, 256), perIP: map[string]int{}, ctx: ctx, cancel: cancel}
	s.wg.Add(2)
	go s.udpLoop()
	go s.tcpLoop()
	return s, nil
}
func (s *DNSService) Addr() string { return s.udp.LocalAddr().String() }
func (s *DNSService) Close()       { s.cancel(); s.udp.Close(); s.tcp.Close(); s.wg.Wait() }
func (s *DNSService) acquire(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.perIP[ip] >= 32 {
		return false
	}
	select {
	case s.slots <- struct{}{}:
		s.perIP[ip]++
		return true
	default:
		return false
	}
}
func (s *DNSService) release(ip string) {
	s.mu.Lock()
	s.perIP[ip]--
	if s.perIP[ip] == 0 {
		delete(s.perIP, ip)
	}
	s.mu.Unlock()
	<-s.slots
}
func (s *DNSService) udpLoop() {
	defer s.wg.Done()
	buf := make([]byte, 65535)
	for {
		n, addr, err := s.udp.ReadFromUDP(buf)
		if err != nil {
			return
		}
		raw := append([]byte(nil), buf[:n]...)
		if !s.acquire(addr.IP.String()) {
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer s.release(addr.IP.String())
			ctx, cancel := context.WithTimeout(s.ctx, 11*time.Second)
			defer cancel()
			ans := s.Engine.Handle(ctx, raw, s.Identify(addr.IP))
			q, err := ValidateQuery(raw)
			if err == nil && len(ans) > q.UDPLimit {
				ans = TruncateReply(q, ans)
			}
			_, _ = s.udp.WriteToUDP(ans, addr)
		}()
	}
}
func (s *DNSService) tcpLoop() {
	defer s.wg.Done()
	for {
		conn, err := s.tcp.Accept()
		if err != nil {
			return
		}
		addr, ok := conn.RemoteAddr().(*net.TCPAddr)
		if !ok || !s.acquire(addr.IP.String()) {
			conn.Close()
			continue
		}
		s.wg.Add(1)
		go s.tcpConnection(conn, addr)
	}
}
func (s *DNSService) tcpConnection(conn net.Conn, addr *net.TCPAddr) {
	defer s.wg.Done()
	defer s.release(addr.IP.String())
	defer conn.Close()
	done := make(chan struct{})
	go func() {
		select {
		case <-s.ctx.Done():
			conn.Close()
		case <-done:
		}
	}()
	defer close(done)
	var writes sync.Mutex
	var pending sync.WaitGroup
	// RFC 7766 permits out-of-order responses matched by ID. Serial processing
	// would put an AAAA query behind a delayed A on the same TCP connection.
	defer pending.Wait()
	write := func(ans []byte) {
		writes.Lock()
		defer writes.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
		var h [2]byte
		binary.BigEndian.PutUint16(h[:], uint16(len(ans)))
		_, _ = conn.Write(append(h[:], ans...))
	}
	for i := 0; i < 64; i++ {
		_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
		var h [2]byte
		if _, err := io.ReadFull(conn, h[:]); err != nil {
			return
		}
		n := int(binary.BigEndian.Uint16(h[:]))
		if n < 12 {
			return
		}
		raw := make([]byte, n)
		if _, err := io.ReadFull(conn, raw); err != nil {
			return
		}
		if !s.acquire(addr.IP.String()) {
			write(ErrorReply(raw, 2))
			continue
		}
		pending.Add(1)
		go func(raw []byte) {
			defer pending.Done()
			defer s.release(addr.IP.String())
			ctx, cancel := context.WithTimeout(s.ctx, 11*time.Second)
			defer cancel()
			write(s.Engine.Handle(ctx, raw, s.Identify(addr.IP)))
		}(raw)
	}
}
