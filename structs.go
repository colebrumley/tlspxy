package main

import (
	"crypto/tls"
	"net"
)

//A proxy represents a pair of connections and their state
type Proxy struct {
	SentBytes              uint64
	ReceivedBytes          uint64
	ServerAddr, RemoteAddr *net.TCPAddr
	ServerConn, RemoteConn net.Conn
	RemoteTlsConf          *tls.Config
	ErrorState             bool
	ErrorSignal            chan bool
	prefix                 string
	showContent            bool
	matcher                func([]byte)
	replacer               func([]byte) []byte
}
