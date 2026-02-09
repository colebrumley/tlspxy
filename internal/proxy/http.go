package proxy

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/colebrumley/tlspxy/internal/metrics"
)

// Transport is a custom http.Transport for use in the HTTP reverse proxy
type Transport struct {
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

var httpLog = slog.With("component", "http")

// InterruptHandler writes info when an os signal is encountered.
func (t *Transport) InterruptHandler() {
	httpLog.Info("Proxy shutting down",
		"sent", atomic.LoadInt64(&t.bytesTo),
		"received", atomic.LoadInt64(&t.bytesFrom),
	)
}

// RoundTrip invokes the underlying RoundTripper and captures data about the call
// on its way back to the client.
func (t *Transport) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	entry := httpLog.With("url", req.URL.String())
	entry.Debug("Calling remote")

	if req.Body != nil {
		req.Body = &countingReadCloser{
			ReadCloser: req.Body,
			onClose: func(n int64) {
				atomic.AddInt64(&t.bytesTo, n)
				if metrics.Enabled.Load() {
					metrics.BytesSentTotal.Add(float64(n))
				}
			},
		}
	}
	resp, err = t.RoundTripper.RoundTrip(req)
	if err != nil {
		if metrics.Enabled.Load() {
			metrics.ErrorsTotal.WithLabelValues("http").Inc()
		}
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
		entry.Debug("Response received",
			"status", resp.StatusCode,
			"content_type", resp.Header.Get("Content-Type"),
			"bytes", len(b),
		)
		entry.Debug(fmt.Sprintf("Content: %s", string(b)))
		atomic.AddInt64(&t.bytesFrom, int64(len(b)))
		if metrics.Enabled.Load() {
			metrics.BytesReceivedTotal.Add(float64(len(b)))
		}
		resp.Body = io.NopCloser(bytes.NewReader(b))
	} else {
		entry.Debug("Response received",
			"status", resp.StatusCode,
			"content_type", resp.Header.Get("Content-Type"),
		)
		resp.Body = &countingReadCloser{
			ReadCloser: resp.Body,
			onClose: func(n int64) {
				atomic.AddInt64(&t.bytesFrom, n)
				if metrics.Enabled.Load() {
					metrics.BytesReceivedTotal.Add(float64(n))
				}
			},
		}
	}

	return
}
