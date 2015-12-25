package main

import (
	// "fmt"
	"crypto/tls"
	log "github.com/Sirupsen/logrus"
	"io"
	"net"
	// "net/http"
)

func (p *Proxy) err(s string, err error) {
	if p.ErrorState {
		return
	}
	if err != io.EOF {
		log.Warningf(p.prefix+s, err)
	}
	p.ErrorSignal <- true
	p.ErrorState = true
}

func (p *Proxy) start() {
	defer p.ServerConn.Close()
	//connect to remote
	var (
		rConn net.Conn
		err   error
		isTLS bool
	)

	if p.RemoteTlsConf != nil {
		isTLS = true
		rConn, err = tls.Dial("tcp", p.RemoteAddr.String(), p.RemoteTlsConf)
	} else {
		isTLS = false
		rConn, err = net.DialTCP("tcp", nil, p.RemoteAddr)
	}
	if err != nil {
		p.err("Remote connection failed: %s", err)
		return
	}
	p.RemoteConn = rConn
	defer p.RemoteConn.Close()
	//display both ends
	log.Infof("%sOpened %s >>> %s TLS=%v", p.prefix, p.ServerConn.RemoteAddr().String(), p.RemoteConn.RemoteAddr().String(), isTLS)
	//bidirectional copy
	go p.pipe(p.ServerConn, p.RemoteConn)
	go p.pipe(p.RemoteConn, p.ServerConn)
	//wait for close...
	<-p.ErrorSignal
	log.Infof("%s Closed (%d bytes sent, %d bytes recieved)", p.prefix, p.SentBytes, p.ReceivedBytes)
}

func (p *Proxy) pipe(src, dst net.Conn) {
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
		//execute match
		if p.matcher != nil {
			p.matcher(b)
		}
		//execute replace
		if p.replacer != nil {
			b = p.replacer(b)
		}

		//show output
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
			p.SentBytes += uint64(n)
		} else {
			p.ReceivedBytes += uint64(n)
		}
	}
}
