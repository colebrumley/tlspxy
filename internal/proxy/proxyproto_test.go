package proxy

import (
	"bytes"
	"net"
	"testing"
)

func mustTCP(s string) *net.TCPAddr {
	a, err := net.ResolveTCPAddr("tcp", s)
	if err != nil {
		panic(err)
	}
	return a
}

func TestProxyHeaderV1(t *testing.T) {
	tests := []struct {
		name string
		src  net.Addr
		dst  net.Addr
		want string
	}{
		{
			name: "IPv4",
			src:  mustTCP("192.168.0.1:56324"),
			dst:  mustTCP("192.168.0.11:443"),
			want: "PROXY TCP4 192.168.0.1 192.168.0.11 56324 443\r\n",
		},
		{
			name: "IPv6",
			src:  mustTCP("[2001:db8::1]:56324"),
			dst:  mustTCP("[2001:db8::2]:443"),
			want: "PROXY TCP6 2001:db8::1 2001:db8::2 56324 443\r\n",
		},
		{
			name: "unknown non-tcp addr",
			src:  &net.UnixAddr{Name: "/tmp/foo", Net: "unix"},
			dst:  mustTCP("192.168.0.11:443"),
			want: "PROXY UNKNOWN\r\n",
		},
		{
			name: "mixed families",
			src:  mustTCP("192.168.0.1:1000"),
			dst:  mustTCP("[2001:db8::2]:443"),
			want: "PROXY UNKNOWN\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ProxyHeader("v1", tt.src, tt.dst)
			if err != nil {
				t.Fatalf("ProxyHeader v1 error: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("v1 header = %q, want %q", string(got), tt.want)
			}
		})
	}
}

func TestProxyHeaderV2(t *testing.T) {
	sig := []byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A}

	tests := []struct {
		name string
		src  net.Addr
		dst  net.Addr
		want []byte
	}{
		{
			name: "IPv4",
			src:  mustTCP("192.168.0.1:56324"),
			dst:  mustTCP("192.168.0.11:443"),
			want: append(append([]byte{}, sig...),
				0x21,       // version 2 + PROXY
				0x11,       // AF_INET + STREAM
				0x00, 0x0C, // len 12
				192, 168, 0, 1, // src IP
				192, 168, 0, 11, // dst IP
				0xDC, 0x04, // src port 56324
				0x01, 0xBB, // dst port 443
			),
		},
		{
			name: "IPv6",
			src:  mustTCP("[2001:db8::1]:56324"),
			dst:  mustTCP("[2001:db8::2]:443"),
			want: append(append([]byte{}, sig...),
				0x21,       // version 2 + PROXY
				0x21,       // AF_INET6 + STREAM
				0x00, 0x24, // len 36
				0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x01, // src
				0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x02, // dst
				0xDC, 0x04, // src port
				0x01, 0xBB, // dst port
			),
		},
		{
			name: "unspec non-tcp",
			src:  &net.UnixAddr{Name: "/tmp/foo", Net: "unix"},
			dst:  mustTCP("192.168.0.11:443"),
			want: append(append([]byte{}, sig...),
				0x21,       // version 2 + PROXY
				0x00,       // AF_UNSPEC
				0x00, 0x00, // len 0
			),
		},
		{
			name: "mixed families -> unspec",
			src:  mustTCP("192.168.0.1:1000"),
			dst:  mustTCP("[2001:db8::2]:443"),
			want: append(append([]byte{}, sig...),
				0x21,
				0x00,
				0x00, 0x00,
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ProxyHeader("v2", tt.src, tt.dst)
			if err != nil {
				t.Fatalf("ProxyHeader v2 error: %v", err)
			}
			if !bytes.Equal(got, tt.want) {
				t.Errorf("v2 header =\n  %v\nwant\n  %v", got, tt.want)
			}
		})
	}
}

func TestProxyHeader_Disabled(t *testing.T) {
	got, err := ProxyHeader("", mustTCP("192.168.0.1:1000"), mustTCP("192.168.0.2:443"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil header for empty version, got %v", got)
	}
}

func TestProxyHeader_UnknownVersion(t *testing.T) {
	_, err := ProxyHeader("v3", mustTCP("192.168.0.1:1000"), mustTCP("192.168.0.2:443"))
	if err == nil {
		t.Fatal("expected error for unknown version v3")
	}
}
