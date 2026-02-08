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

var httpLog = log.WithField("component", "http")

// InterruptHandler writes info when an os signal is encountered.
func (t *ProxyTransport) InterruptHandler() {
	httpLog.WithFields(log.Fields{
		"sent":     atomic.LoadInt64(&t.bytesTo),
		"received": atomic.LoadInt64(&t.bytesFrom),
	}).Info("Proxy shutting down")
}

// RoundTrip invokes the underlying RoundTripper and captures data about the call
// on its way back to the client.
func (t *ProxyTransport) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	entry := httpLog.WithField("url", req.URL.String())
	entry.Debug("Calling remote")

	if req.Body != nil {
		cl, err := io.ReadAll(req.Body)
		if err != nil {
			entry.Errorf("Failed to read request body: %v", err)
		}
		atomic.AddInt64(&t.bytesTo, int64(len(cl)))
		req.Body = io.NopCloser(bytes.NewReader(cl))
	}
	resp, err = t.RoundTripper.RoundTrip(req)
	if err != nil {
		resp = nil
		return
	}

	respEntry := entry.WithFields(log.Fields{
		"status":       resp.StatusCode,
		"content_type": resp.Header.Get("Content-Type"),
	})

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
		respEntry.WithField("bytes", len(b)).Debug("Response received")
		respEntry.Debugf("Content: %s", string(b))
		atomic.AddInt64(&t.bytesFrom, int64(len(b)))
		resp.Body = io.NopCloser(bytes.NewReader(b))
	} else {
		respEntry.Debug("Response received")
		resp.Body = &countingReadCloser{
			ReadCloser: resp.Body,
			onClose: func(n int64) {
				atomic.AddInt64(&t.bytesFrom, n)
			},
		}
	}

	return
}
