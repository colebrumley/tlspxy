package main

import (
	"crypto/tls"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
)

// ProxyCounter counts the proxy's traffic
type ProxyCounter struct {
	to, from uint64
}

// To adds bytes to the to counter
func (p *ProxyCounter) To(b uint64) {
	atomic.AddUint64(&p.to, b)
}

// From adds bytes to the from counter
func (p *ProxyCounter) From(b uint64) {
	atomic.AddUint64(&p.from, b)
}

// Total returns the count
func (p *ProxyCounter) Total() (to, from uint64) {
	to = atomic.LoadUint64(&p.to)
	from = atomic.LoadUint64(&p.from)
	return
}

// InterruptHandler writes info when an os signal is encountered.
func (p *ProxyCounter) InterruptHandler() {
	to, from := p.Total()
	log.WithFields(log.Fields{
		"component": "tcp",
		"sent":      to,
		"received":  from,
	}).Info("Proxy shutting down")
}

// TCPProxy is the wrapper object for a proxy connection. It tracks the amount
// of data sent and received, local and remote server settings, TLS config,
// and any connection errors.
type TCPProxy struct {
	Counter                *ProxyCounter
	ServerAddr, RemoteAddr string
	ServerConn, RemoteConn net.Conn
	RemoteTLSConf          *tls.Config
	ErrorSignal            chan bool
	closeOnce              sync.Once
	connID                 int
	showContent            bool
	log                    *log.Entry
}

func (p *TCPProxy) err(s string, err error) {
	if err != io.EOF {
		p.log.Warningf(s, err)
	}
	p.closeOnce.Do(func() { p.ErrorSignal <- true })
}

func (p *TCPProxy) start() {
	defer p.ServerConn.Close()
	var (
		rConn net.Conn
		err   error
		isTLS bool
	)

	if p.RemoteTLSConf != nil {
		isTLS = true
		p.log.Debug("Dialing remote")
		rConn, err = tls.DialWithDialer(&net.Dialer{Timeout: 30 * time.Second}, "tcp", p.RemoteAddr, p.RemoteTLSConf)
	} else {
		isTLS = false
		rConn, err = net.DialTimeout("tcp", p.RemoteAddr, 30*time.Second)
	}
	if err != nil {
		p.err("Remote connection failed: %s", err)
		return
	}
	p.RemoteConn = rConn
	defer p.RemoteConn.Close()

	p.log.WithFields(log.Fields{
		"src": p.ServerConn.RemoteAddr().String(),
		"dst": p.RemoteConn.RemoteAddr().String(),
		"tls": isTLS,
	}).Info("Connection opened")

	go p.pipe(p.ServerConn, p.RemoteConn)
	go p.pipe(p.RemoteConn, p.ServerConn)
	<-p.ErrorSignal
	p.log.Info("Connection closed")
}

func (p *TCPProxy) pipe(src, dst net.Conn) {
	islocal := src == p.ServerConn
	direction := "recv"
	if islocal {
		direction = "send"
	}

	buff := make([]byte, 0xffff)
	for {
		n, err := src.Read(buff)
		if err != nil {
			p.err("Read failed '%s'", err)
			return
		}
		b := buff[:n]

		entry := p.log.WithFields(log.Fields{
			"direction": direction,
			"bytes":     n,
		})
		if p.showContent {
			entry.Debugf("%s", string(b))
		} else {
			entry.Debug("Data transferred")
		}

		n, err = dst.Write(b)
		if err != nil {
			p.err("Write failed '%s'", err)
			return
		}
		if islocal {
			p.Counter.To(uint64(n))
		} else {
			p.Counter.From(uint64(n))
		}
	}
}
