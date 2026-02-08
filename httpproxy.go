package main

import (
	"bytes"
	"io"
	"net/http"
	"sync/atomic"

	log "github.com/sirupsen/logrus"
)

// ProxyTransport is a custom http.Transport for use in the HTTP reverse proxy
type ProxyTransport struct {
	bytesTo, bytesFrom int64
	ShowContent        bool
	http.RoundTripper
}

// countingReadCloser wraps an io.ReadCloser and counts bytes as they stream through.
// On Close, it calls the onClose callback with the total bytes read.
type countingReadCloser struct {
	io.ReadCloser
	bytesRead int64
	onClose   func(int64)
}

func (c *countingReadCloser) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	c.bytesRead += int64(n)
	return n, err
}

func (c *countingReadCloser) Close() error {
	err := c.ReadCloser.Close()
	if c.onClose != nil {
		c.onClose(c.bytesRead)
	}
	return err
}

// InterruptHandler writes info when an os signal is encountered.
func (t *ProxyTransport) InterruptHandler() {
	log.Infof("HTTP proxy sent %v bytes and received %v bytes",
		atomic.LoadInt64(&t.bytesTo), atomic.LoadInt64(&t.bytesFrom))
}

// RoundTrip invokes the underlying RoundTripper and captures data about the call
// on its way back to the client.
func (t *ProxyTransport) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	log.Debugf("Calling: %s", req.URL.String())
	if req.Body != nil {
		cl, err := io.ReadAll(req.Body)
		if err != nil {
			log.Errorf("%v", err)
		}
		atomic.AddInt64(&t.bytesTo, int64(len(cl)))
		req.Body = io.NopCloser(bytes.NewReader(cl))
	}
	resp, err = t.RoundTripper.RoundTrip(req)
	if err != nil {
		resp = nil
		return
	}

	if t.ShowContent {
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			resp = nil
			return resp, err
		}
		err = resp.Body.Close()
		if err != nil {
			resp = nil
			return resp, err
		}
		log.Debugf("Response: code=%v content-length=%v content-type=%v", resp.StatusCode, len(b), resp.Header["Content-Type"])
		log.Debugf("Content: %s", string(b))
		atomic.AddInt64(&t.bytesFrom, int64(len(b)))
		resp.Body = io.NopCloser(bytes.NewReader(b))
	} else {
		log.Debugf("Response: code=%v content-type=%v", resp.StatusCode, resp.Header["Content-Type"])
		resp.Body = &countingReadCloser{
			ReadCloser: resp.Body,
			onClose: func(n int64) {
				atomic.AddInt64(&t.bytesFrom, n)
			},
		}
	}

	return
}
