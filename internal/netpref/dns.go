package netpref

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"time"
)

const (
	TypeA    uint16 = 1
	TypeAAAA uint16 = 28
)

type Question struct {
	ID, Flags, Type, Class uint16
	Name                   string
	End                    int
	Labels                 []string
	UDPLimit               int
}

func u16(b []byte, i int) uint16      { return binary.BigEndian.Uint16(b[i : i+2]) }
func put16(b []byte, i int, v uint16) { binary.BigEndian.PutUint16(b[i:i+2], v) }
func nameAt(b []byte, off int) ([]string, int, error) {
	labels := []string{}
	end := -1
	total := 0
	seen := map[int]bool{}
	for hops := 0; hops < 128; hops++ {
		if off < 0 || off >= len(b) || seen[off] {
			return nil, 0, errors.New("invalid DNS name/pointer")
		}
		seen[off] = true
		n := int(b[off])
		off++
		if n == 0 {
			if end < 0 {
				end = off
			}
			return labels, end, nil
		}
		if n&0xc0 == 0xc0 {
			if off >= len(b) {
				return nil, 0, io.ErrUnexpectedEOF
			}
			p := ((n & 0x3f) << 8) | int(b[off])
			if end < 0 {
				end = off + 1
			}
			off = p
			continue
		}
		if n&0xc0 != 0 || n > 63 || off+n > len(b) {
			return nil, 0, errors.New("invalid label")
		}
		total += n + 1
		if total > 254 {
			return nil, 0, errors.New("name too long")
		}
		labels = append(labels, string(b[off:off+n]))
		off += n
	}
	return nil, 0, errors.New("too many compression pointers")
}
func ParseQuestion(b []byte) (Question, error) {
	q := Question{UDPLimit: 512}
	if len(b) < 12 {
		return q, io.ErrUnexpectedEOF
	}
	q.ID = u16(b, 0)
	q.Flags = u16(b, 2)
	if u16(b, 4) != 1 {
		return q, errors.New("exactly one DNS question is required")
	}
	labs, end, e := nameAt(b, 12)
	if e != nil {
		return q, e
	}
	if end+4 > len(b) {
		return q, io.ErrUnexpectedEOF
	}
	q.Labels = labs
	q.Name = strings.ToLower(strings.Join(labs, "."))
	q.Type = u16(b, end)
	q.Class = u16(b, end+2)
	q.End = end + 4
	return q, nil
}

// ValidateQuery bounds all sections before forwarding. Other opcodes, AXFR/IXFR and TSIG
// are intentionally unsupported by this policy resolver.
func ValidateQuery(b []byte) (Question, error) {
	q, e := ParseQuestion(b)
	if e != nil {
		return q, e
	}
	if q.Flags&0xf800 != 0 || q.Class != 1 || q.Type == 251 || q.Type == 252 {
		return q, errors.New("unsupported query/opcode/class")
	}
	if u16(b, 6) != 0 || u16(b, 8) != 0 {
		return q, errors.New("query may not contain answer/authority records")
	}
	pos := q.End
	for i := 0; i < int(u16(b, 10)); i++ {
		_, end, e := nameAt(b, pos)
		if e != nil {
			return q, e
		}
		if end+10 > len(b) {
			return q, io.ErrUnexpectedEOF
		}
		typ := u16(b, end)
		n := int(u16(b, end+8))
		if end+10+n > len(b) {
			return q, io.ErrUnexpectedEOF
		}
		if typ == 250 {
			return q, errors.New("TSIG unsupported")
		}
		if typ == 41 {
			q.UDPLimit = int(u16(b, end+2))
			if q.UDPLimit < 512 {
				q.UDPLimit = 512
			}
			if q.UDPLimit > 4096 {
				q.UDPLimit = 4096
			}
		}
		pos = end + 10 + n
	}
	if pos != len(b) {
		return q, errors.New("trailing DNS data")
	}
	return q, nil
}
func questionWire(q Question) []byte {
	b := make([]byte, 12)
	put16(b, 0, q.ID)
	put16(b, 4, 1)
	for _, l := range q.Labels {
		b = append(b, byte(len(l)))
		b = append(b, []byte(l)...)
	}
	b = append(b, 0, byte(q.Type>>8), byte(q.Type), byte(q.Class>>8), byte(q.Class))
	return b
}
func MakeQuery(name string, typ uint16) []byte {
	var id [2]byte
	_, _ = rand.Read(id[:])
	q := Question{ID: binary.BigEndian.Uint16(id[:]), Type: typ, Class: 1}
	name = strings.TrimSuffix(name, ".")
	if name != "" {
		q.Labels = strings.Split(name, ".")
	}
	b := questionWire(q)
	put16(b, 2, 0x0100)
	return b
}
func Reply(q Question, rcode int, addresses []netip.Addr, ttl int) []byte {
	b := questionWire(q)
	put16(b, 2, 0x8080|(q.Flags&0x0110)|uint16(rcode&15)) // QR,RA,RD,CD; never falsely assert AD/AA
	n := 0
	for _, a := range addresses {
		if q.Type != TypeA && q.Type != TypeAAAA {
			continue
		}
		if q.Type == TypeA && !a.Is4() || q.Type == TypeAAAA && (!a.Is6() || a.Is4In6()) {
			continue
		}
		raw := a.AsSlice()
		rr := make([]byte, 12)
		rr[0] = 0xc0
		rr[1] = 0x0c
		put16(rr, 2, q.Type)
		put16(rr, 4, 1)
		binary.BigEndian.PutUint32(rr[6:10], uint32(ttl))
		put16(rr, 10, uint16(len(raw)))
		b = append(b, rr...)
		b = append(b, raw...)
		n++
	}
	put16(b, 6, uint16(n))
	return b
}
func ErrorReply(raw []byte, rcode int) []byte {
	if q, e := ParseQuestion(raw); e == nil {
		return Reply(q, rcode, nil, 0)
	}
	b := make([]byte, 12)
	if len(raw) >= 2 {
		copy(b[:2], raw[:2])
	}
	put16(b, 2, 0x8080|uint16(rcode))
	return b
}
func TruncateReply(q Question, response []byte) []byte {
	b := Reply(q, 0, nil, 0)
	if len(response) >= 4 {
		put16(b, 2, (u16(response, 2)|0x0200)&^0x0020)
	}
	return b
}
func ValidateResponse(b []byte, q Question) error {
	r, e := ParseQuestion(b)
	if e != nil {
		return e
	}
	if r.ID != q.ID || r.Flags&0x8000 == 0 || r.Flags&0x7800 != 0 || r.Name != q.Name || r.Type != q.Type || r.Class != q.Class {
		return errors.New("DNS transaction/question mismatch")
	}
	pos := r.End
	count := int(u16(b, 6)) + int(u16(b, 8)) + int(u16(b, 10))
	for i := 0; i < count; i++ {
		_, end, e := nameAt(b, pos)
		if e != nil {
			return e
		}
		if end+10 > len(b) {
			return io.ErrUnexpectedEOF
		}
		n := int(u16(b, end+8))
		pos = end + 10 + n
		if pos > len(b) {
			return io.ErrUnexpectedEOF
		}
	}
	if pos != len(b) {
		return errors.New("response trailing data")
	}
	return nil
}

// AddressEvidence accepts address records along a verified CNAME chain only.
func AddressEvidence(b []byte, q Question) (bool, time.Duration) {
	parsed, err := ParseQuestion(b)
	if err != nil {
		return false, time.Second
	}
	q.End = parsed.End

	if len(b) < 12 || u16(b, 2)&0x020f != 0 {
		return false, time.Second
	}
	type rr struct {
		owner    string
		typ      uint16
		ttl      uint32
		start, n int
	}
	pos := q.End
	var rs []rr
	for i := 0; i < int(u16(b, 6)); i++ {
		labs, end, e := nameAt(b, pos)
		if e != nil || end+10 > len(b) {
			return false, time.Second
		}
		n := int(u16(b, end+8))
		if end+10+n > len(b) {
			return false, time.Second
		}
		if u16(b, end+2) == 1 {
			rs = append(rs, rr{strings.ToLower(strings.Join(labs, ".")), u16(b, end), binary.BigEndian.Uint32(b[end+4 : end+8]), end + 10, n})
		}
		pos = end + 10 + n
	}
	allowed := map[string]bool{q.Name: true}
	ttl := uint32(10)
	for h := 0; h < 16; h++ {
		changed := false
		for _, r := range rs {
			if r.typ == 5 && allowed[r.owner] {
				labs, _, e := nameAt(b, r.start)
				if e == nil {
					target := strings.ToLower(strings.Join(labs, "."))
					if !allowed[target] {
						allowed[target] = true
						changed = true
					}
					if r.ttl < ttl {
						ttl = r.ttl
					}
				}
			}
		}
		if !changed {
			break
		}
	}
	found := false
	for _, r := range rs {
		if allowed[r.owner] && r.typ == q.Type && (r.typ == TypeA && r.n == 4 || r.typ == TypeAAAA && r.n == 16) {
			found = true
			if r.ttl < ttl {
				ttl = r.ttl
			}
		}
	}
	if ttl == 0 {
		ttl = 1
	}
	return found, time.Duration(ttl) * time.Second
}

type ExchangeFunc func(context.Context, []byte, string) ([]byte, error)

func Exchange(ctx context.Context, raw []byte, upstream string) ([]byte, error) {
	q, e := ValidateQuery(raw)
	if e != nil {
		return nil, e
	}
	network, addr, e := Endpoint(upstream)
	if e != nil {
		return nil, e
	}
	b, e := exchangeTransport(ctx, raw, addr, network, q)
	if e != nil {
		return nil, e
	}
	if network == "udp" && u16(b, 2)&0x0200 != 0 {
		return exchangeTransport(ctx, raw, addr, "tcp", q)
	}
	return b, nil
}
func exchangeTransport(ctx context.Context, raw []byte, addr, network string, q Question) ([]byte, error) {
	d := net.Dialer{}
	conn, e := d.DialContext(ctx, network, addr)
	if e != nil {
		return nil, e
	}
	defer conn.Close()
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(2 * time.Second)
	}
	_ = conn.SetDeadline(deadline)
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()
	defer close(done)
	if network == "tcp" {
		head := []byte{byte(len(raw) >> 8), byte(len(raw))}
		if _, e = conn.Write(append(head, raw...)); e != nil {
			return nil, e
		}
		if _, e = io.ReadFull(conn, head); e != nil {
			return nil, e
		}
		n := int(u16(head, 0))
		if n < 12 {
			return nil, errors.New("short TCP DNS message")
		}
		b := make([]byte, n)
		if _, e = io.ReadFull(conn, b); e != nil {
			return nil, e
		}
		if e = ValidateResponse(b, q); e != nil {
			return nil, e
		}
		return b, nil
	}
	if _, e = conn.Write(raw); e != nil {
		return nil, e
	}
	b := make([]byte, 65535)
	for tries := 0; tries < 4; tries++ {
		n, err := conn.Read(b)
		if err != nil {
			return nil, err
		}
		ans := b[:n]
		if err = ValidateResponse(ans, q); err != nil {
			continue
		}
		return append([]byte(nil), ans...), nil
	}
	return nil, fmt.Errorf("upstream sent mismatched DNS responses")
}
