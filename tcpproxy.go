package main

import (
	"crypto/tls"
	"io"
	"net"

	log "github.com/Sirupsen/logrus"
)

// TCPProxy is the wrapper object for a proxy connection. It tracks the amount
// of data sent and received, local and remote server settings, TLS config,
// and any connection errors.
type TCPProxy struct {
	bytesTo                uint64
	bytesFrom              uint64
	ServerAddr, RemoteAddr string
	ServerConn, RemoteConn net.Conn
	RemoteTLSConf          *tls.Config
	ErrorState             bool
	ErrorSignal            chan bool
	prefix                 string
	showContent            bool
}

// InterruptHandler writes info when an os signal is encountered.
func (p *TCPProxy) InterruptHandler() {
	log.Infof("TCP proxy sent %v bytes and received %v bytes", p.bytesTo, p.bytesFrom)
}

func (p *TCPProxy) err(s string, err error) {
	if p.ErrorState {
		return
	}
	if err != io.EOF {
		log.Warningf(p.prefix+s, err)
	}
	p.ErrorSignal <- true
	p.ErrorState = true
}

func (p *TCPProxy) start() {
	defer p.ServerConn.Close()
	//connect to remote
	var (
		rConn net.Conn
		err   error
		isTLS bool
	)

	if p.RemoteTLSConf != nil {
		isTLS = true
		p.RemoteTLSConf.BuildNameToCertificate()
		log.Debugf("Dialing %s", p.RemoteAddr)
		rConn, err = tls.Dial("tcp", p.RemoteAddr, p.RemoteTLSConf)
	} else {
		isTLS = false
		rConn, err = net.Dial("tcp", p.RemoteAddr)
	}
	if err != nil {
		p.err("Remote connection failed: %s", err)
		return
	}
	p.RemoteConn = rConn
	defer p.RemoteConn.Close()

	// Log info about both ends of the conn
	log.Infof("%sOpened connection %s >>> %s TLS=%v", p.prefix,
		p.ServerConn.RemoteAddr().String(),
		p.RemoteConn.RemoteAddr().String(), isTLS)

	//bidirectional copy in separate goroutines
	go p.pipe(p.ServerConn, p.RemoteConn)
	go p.pipe(p.RemoteConn, p.ServerConn)
	//wait for close...
	<-p.ErrorSignal
	log.Infof("%s Closed (%d bytes sent, %d bytes recieved)",
		p.prefix, p.bytesTo, p.bytesFrom)
}

func (p *TCPProxy) pipe(src, dst net.Conn) {
	//data direction
	var f string
	islocal := src == p.ServerConn
	if islocal {
		f = p.prefix + " >>> %d bytes sent%s"
	} else {
		f = p.prefix + " <<< %d bytes recieved%s"
	}

	//directional copy (64k buffer)
	buff := make([]byte, 0xffff)
	for {
		n, err := src.Read(buff)
		if err != nil {
			p.err("Read failed '%s'", err)
			return
		}
		b := buff[:n]

		//show output if necessary
		if p.showContent {
			log.Debugf(f, n, "\n"+string(b))
		} else {
			log.Debugf(f, n, "")
		}

		//write out result
		n, err = dst.Write(b)
		if err != nil {
			p.err("Write failed '%s'", err)
			return
		}
		if islocal {
			p.bytesTo += uint64(n)
		} else {
			p.bytesFrom += uint64(n)
		}
	}
}
