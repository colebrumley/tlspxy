package proxy

import (
	"encoding/binary"
	"fmt"
	"net"
)

// v2Signature is the 12-byte PROXY protocol v2 signature that prefixes every
// v2 header: "\r\n\r\n\x00\r\nQUIT\n".
var v2Signature = []byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A}

// ProxyHeader builds a HAProxy PROXY protocol header for the given version
// ("v1" or "v2") describing a connection from src to dst. For any other version
// string it returns nil (PROXY protocol disabled). If the addresses cannot be
// resolved to TCP addresses it emits the protocol's UNKNOWN/UNSPEC form so the
// receiver still sees a valid header.
func ProxyHeader(version string, src, dst net.Addr) ([]byte, error) {
	switch version {
	case "v1":
		return proxyHeaderV1(src, dst), nil
	case "v2":
		return proxyHeaderV2(src, dst), nil
	case "":
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown proxy protocol version %q", version)
	}
}

// tcpAddr extracts the IP and port from a net.Addr if it is TCP-like.
func tcpAddr(a net.Addr) (net.IP, int, bool) {
	if ta, ok := a.(*net.TCPAddr); ok {
		return ta.IP, ta.Port, true
	}
	// Fall back to parsing the string form (host:port).
	host, portStr, err := net.SplitHostPort(a.String())
	if err != nil {
		return nil, 0, false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil, 0, false
	}
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		return nil, 0, false
	}
	return ip, port, true
}

// proxyHeaderV1 builds the human-readable v1 header.
func proxyHeaderV1(src, dst net.Addr) []byte {
	sIP, sPort, sOK := tcpAddr(src)
	dIP, dPort, dOK := tcpAddr(dst)
	if !sOK || !dOK {
		return []byte("PROXY UNKNOWN\r\n")
	}

	s4 := sIP.To4()
	d4 := dIP.To4()
	if s4 != nil && d4 != nil {
		return []byte(fmt.Sprintf("PROXY TCP4 %s %s %d %d\r\n", s4.String(), d4.String(), sPort, dPort))
	}
	// If either side is IPv6 (and neither is a mismatched family), emit TCP6.
	if s4 == nil && d4 == nil {
		return []byte(fmt.Sprintf("PROXY TCP6 %s %s %d %d\r\n", sIP.String(), dIP.String(), sPort, dPort))
	}
	// Mixed families cannot be represented; fall back to UNKNOWN.
	return []byte("PROXY UNKNOWN\r\n")
}

// proxyHeaderV2 builds the binary v2 header.
func proxyHeaderV2(src, dst net.Addr) []byte {
	sIP, sPort, sOK := tcpAddr(src)
	dIP, dPort, dOK := tcpAddr(dst)

	// Base header: 12-byte signature + version/command + family/protocol +
	// 2-byte address length.
	hdr := make([]byte, 0, 16+36)
	hdr = append(hdr, v2Signature...)
	// Version 2 (0x2) + command PROXY (0x1) = 0x21.
	hdr = append(hdr, 0x21)

	if !sOK || !dOK {
		// Unparseable: UNSPEC family/protocol with zero address length.
		hdr = append(hdr, 0x00) // AF_UNSPEC
		hdr = append(hdr, 0x00, 0x00)
		return hdr
	}

	s4 := sIP.To4()
	d4 := dIP.To4()
	if s4 != nil && d4 != nil {
		// TCP over IPv4: family/protocol 0x11, address block 12 bytes.
		hdr = append(hdr, 0x11)
		hdr = append(hdr, 0x00, 0x0C) // length = 12
		hdr = append(hdr, s4...)
		hdr = append(hdr, d4...)
		hdr = binary.BigEndian.AppendUint16(hdr, uint16(sPort))
		hdr = binary.BigEndian.AppendUint16(hdr, uint16(dPort))
		return hdr
	}
	if s4 == nil && d4 == nil {
		// TCP over IPv6: family/protocol 0x21, address block 36 bytes.
		hdr = append(hdr, 0x21)
		hdr = append(hdr, 0x00, 0x24) // length = 36
		hdr = append(hdr, sIP.To16()...)
		hdr = append(hdr, dIP.To16()...)
		hdr = binary.BigEndian.AppendUint16(hdr, uint16(sPort))
		hdr = binary.BigEndian.AppendUint16(hdr, uint16(dPort))
		return hdr
	}

	// Mixed families: emit UNSPEC.
	hdr = append(hdr, 0x00)
	hdr = append(hdr, 0x00, 0x00)
	return hdr
}
