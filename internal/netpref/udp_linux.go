//go:build linux

package netpref

import (
	"errors"
	"net"
	"syscall"
	"unsafe"
)

// Wildcard UDP sockets must reply from the destination address of the query.
// Otherwise Linux may choose a different LAN address and connected clients (or
// conntrack reverse NAT) discard the answer. See Linux IP_PKTINFO/in6_pktinfo.
func enablePacketInfo(conn *net.UDPConn) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var sockErr error
	err = raw.Control(func(fd uintptr) {
		sockErr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_PKTINFO, 1)
		if sockErr == nil && conn.LocalAddr().(*net.UDPAddr).IP.To4() == nil {
			sockErr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IPV6, syscall.IPV6_RECVPKTINFO, 1)
		}
	})
	return errors.Join(err, sockErr)
}

func packetInfoReply(oob []byte, client net.IP) []byte {
	messages, err := syscall.ParseSocketControlMessage(oob)
	if err != nil {
		return nil
	}
	for _, message := range messages {
		v4 := client.To4() != nil
		match4 := message.Header.Level == syscall.IPPROTO_IP && message.Header.Type == syscall.IP_PKTINFO && len(message.Data) == 12
		match6 := message.Header.Level == syscall.IPPROTO_IPV6 && message.Header.Type == syscall.IPV6_PKTINFO && len(message.Data) == 20
		if !(v4 && match4 || !v4 && match6) {
			continue
		}
		data := append([]byte(nil), message.Data...)
		if v4 {
			// ipi_spec_dst controls the source; do not let a received interface's
			// primary address override a secondary local destination.
			copy(data[4:8], data[8:12])
			for i := 0; i < 4; i++ {
				data[i] = 0
			}
		}
		result := make([]byte, syscall.CmsgSpace(len(data)))
		header := (*syscall.Cmsghdr)(unsafe.Pointer(&result[0]))
		header.Level = message.Header.Level
		header.Type = message.Header.Type
		header.SetLen(syscall.CmsgLen(len(data)))
		copy(result[syscall.CmsgLen(0):], data)
		return result
	}
	return nil
}
