package loadtest

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	defaultPayloadSize = 1024
	dialTimeout        = 5 * time.Second
)

// TCPGenerator sends TCP connections to a target address and records stats.
type TCPGenerator struct {
	Addr        string
	TLSConf     *tls.Config
	PayloadSize int
	Stats       *Stats
}

// Run launches concurrency workers that continuously dial, write, and read
// until ctx is cancelled. It blocks until all workers have finished.
func (g *TCPGenerator) Run(ctx context.Context, concurrency int) {
	size := g.PayloadSize
	if size <= 0 {
		size = defaultPayloadSize
	}

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			payload := bytes.Repeat([]byte("A"), size)
			readBuf := make([]byte, size)
			g.tcpWorker(ctx, payload, readBuf)
		}()
	}
	wg.Wait()
}

func (g *TCPGenerator) tcpWorker(ctx context.Context, payload, readBuf []byte) {
	dialer := &net.Dialer{Timeout: dialTimeout}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		connectStart := time.Now()
		var conn net.Conn
		var err error
		if g.TLSConf != nil {
			conn, err = tls.DialWithDialer(dialer, "tcp", g.Addr, g.TLSConf)
		} else {
			conn, err = net.DialTimeout("tcp", g.Addr, dialTimeout)
		}
		if err != nil {
			g.Stats.RecordError(fmt.Sprintf("dial: %s", err))
			continue
		}
		g.Stats.RecordConnect(time.Since(connectStart))

		rtStart := time.Now()
		_, err = conn.Write(payload)
		if err != nil {
			g.Stats.RecordError(fmt.Sprintf("write: %s", err))
			conn.Close()
			continue
		}

		received := 0
		for received < len(payload) {
			n, readErr := conn.Read(readBuf[received:])
			received += n
			if readErr != nil {
				err = readErr
				break
			}
		}
		if err != nil {
			g.Stats.RecordError(fmt.Sprintf("read: %s", err))
			conn.Close()
			continue
		}

		g.Stats.RecordRoundTrip(time.Since(rtStart))
		g.Stats.RecordBytes(int64(len(payload)), int64(received))
		g.Stats.RecordSuccess()
		conn.Close()
	}
}

// HTTPGenerator sends HTTP requests to a target URL and records stats.
type HTTPGenerator struct {
	URL         string
	TLSConf     *tls.Config
	PayloadSize int
	Stats       *Stats
}

// Run launches concurrency workers that continuously send HTTP requests
// until ctx is cancelled. It blocks until all workers have finished.
func (g *HTTPGenerator) Run(ctx context.Context, concurrency int) {
	transport := &http.Transport{
		TLSClientConfig:     g.TLSConf,
		MaxIdleConns:        concurrency,
		MaxIdleConnsPerHost: concurrency,
		IdleConnTimeout:     30 * time.Second,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	size := g.PayloadSize
	if size < 0 {
		size = 0
	}

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var payload []byte
			if size > 0 {
				payload = bytes.Repeat([]byte("B"), size)
			}
			g.httpWorker(ctx, client, payload)
		}()
	}
	wg.Wait()
}

func (g *HTTPGenerator) httpWorker(ctx context.Context, client *http.Client, payload []byte) {
	method := http.MethodGet
	if len(payload) > 0 {
		method = http.MethodPost
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		var body io.Reader
		if len(payload) > 0 {
			body = bytes.NewReader(payload)
		}

		req, err := http.NewRequestWithContext(ctx, method, g.URL, body)
		if err != nil {
			g.Stats.RecordError(fmt.Sprintf("request: %s", err))
			continue
		}

		rtStart := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			// Don't record context cancellation as an error.
			if ctx.Err() != nil {
				return
			}
			g.Stats.RecordError(fmt.Sprintf("do: %s", err))
			continue
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			g.Stats.RecordError(fmt.Sprintf("read body: %s", err))
			continue
		}

		g.Stats.RecordRoundTrip(time.Since(rtStart))
		g.Stats.RecordConnect(time.Since(rtStart)) // approximate connect as part of round-trip

		sent := int64(len(payload))
		g.Stats.RecordBytes(sent, int64(len(respBody)))
		g.Stats.RecordSuccess()
	}
}
