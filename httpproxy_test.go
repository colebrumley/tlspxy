package main

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// mockRoundTripper records the request it received and returns a canned response
type mockRoundTripper struct {
	receivedBody []byte
	response     *http.Response
	err          error
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		m.receivedBody, _ = io.ReadAll(req.Body)
	}
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

func TestRoundTrip_PreservesBody(t *testing.T) {
	// This is the critical test: the original bug consumed req.Body without
	// reconstructing it. The fix reads the body, counts bytes, then puts
	// a new reader back on req.Body.
	originalBody := "POST body data that must be preserved"

	mock := &mockRoundTripper{
		response: &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("response")),
		},
	}

	transport := &ProxyTransport{
		RoundTripper: mock,
	}

	req, _ := http.NewRequest("POST", "http://example.com/api", strings.NewReader(originalBody))

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	defer resp.Body.Close()

	// The mock should have received the full original body
	if string(mock.receivedBody) != originalBody {
		t.Errorf("Mock received body = %q, want %q", string(mock.receivedBody), originalBody)
	}
}

func TestRoundTrip_NilBody(t *testing.T) {
	mock := &mockRoundTripper{
		response: &http.Response{
			StatusCode: 200,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("")),
		},
	}

	transport := &ProxyTransport{
		RoundTripper: mock,
	}

	req, _ := http.NewRequest("GET", "http://example.com/", nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	defer resp.Body.Close()

	// bytesTo should be 0 for nil body
	if got := atomic.LoadInt64(&transport.bytesTo); got != 0 {
		t.Errorf("bytesTo = %d, want 0", got)
	}
}

func TestRoundTrip_CountsBytes(t *testing.T) {
	requestBody := "request-payload-12345"
	responseBody := "response-data-67890"

	mock := &mockRoundTripper{
		response: &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(responseBody)),
		},
	}

	transport := &ProxyTransport{
		ShowContent: true, // force immediate read of response body
		RoundTripper: mock,
	}

	req, _ := http.NewRequest("POST", "http://example.com/data", strings.NewReader(requestBody))
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	defer resp.Body.Close()

	bytesTo := atomic.LoadInt64(&transport.bytesTo)
	bytesFrom := atomic.LoadInt64(&transport.bytesFrom)

	if bytesTo != int64(len(requestBody)) {
		t.Errorf("bytesTo = %d, want %d", bytesTo, len(requestBody))
	}
	if bytesFrom != int64(len(responseBody)) {
		t.Errorf("bytesFrom = %d, want %d", bytesFrom, len(responseBody))
	}
}

func TestRoundTrip_StreamingResponse(t *testing.T) {
	// When ShowContent is false, the response body should be wrapped in a
	// countingReadCloser that counts bytes on the fly as the caller reads.
	responseBody := "streaming response content that is somewhat large"

	mock := &mockRoundTripper{
		response: &http.Response{
			StatusCode: 200,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(responseBody)),
		},
	}

	transport := &ProxyTransport{
		ShowContent:  false,
		RoundTripper: mock,
	}

	req, _ := http.NewRequest("GET", "http://example.com/stream", nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}

	// Before reading, bytesFrom should be 0
	if got := atomic.LoadInt64(&transport.bytesFrom); got != 0 {
		t.Errorf("bytesFrom before read = %d, want 0", got)
	}

	// Read and close the body
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if string(body) != responseBody {
		t.Errorf("body = %q, want %q", string(body), responseBody)
	}

	// After close, bytesFrom should be updated
	if got := atomic.LoadInt64(&transport.bytesFrom); got != int64(len(responseBody)) {
		t.Errorf("bytesFrom after close = %d, want %d", got, len(responseBody))
	}
}

func TestCountingReadCloser(t *testing.T) {
	data := "hello counting world"
	var callbackCount int64

	crc := &countingReadCloser{
		ReadCloser: io.NopCloser(strings.NewReader(data)),
		onClose: func(n int64) {
			callbackCount = n
		},
	}

	// Read in small chunks
	buf := make([]byte, 5)
	var total int
	for {
		n, err := crc.Read(buf)
		total += n
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read error: %v", err)
		}
	}

	if total != len(data) {
		t.Errorf("total bytes read = %d, want %d", total, len(data))
	}

	// Close should trigger the callback
	if err := crc.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	if callbackCount != int64(len(data)) {
		t.Errorf("callback received %d bytes, want %d", callbackCount, len(data))
	}
}

func TestCountingReadCloser_Empty(t *testing.T) {
	var callbackCount int64

	crc := &countingReadCloser{
		ReadCloser: io.NopCloser(bytes.NewReader(nil)),
		onClose: func(n int64) {
			callbackCount = n
		},
	}

	_, _ = io.ReadAll(crc)
	crc.Close()

	if callbackCount != 0 {
		t.Errorf("callback received %d bytes, want 0", callbackCount)
	}
}

func TestRoundTrip_LargeResponse(t *testing.T) {
	// Test streaming a large response body
	largeBody := strings.Repeat("x", 1024*1024) // 1 MB

	mock := &mockRoundTripper{
		response: &http.Response{
			StatusCode: 200,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(largeBody)),
		},
	}

	transport := &ProxyTransport{
		ShowContent:  false, // streaming mode
		RoundTripper: mock,
	}

	req, _ := http.NewRequest("GET", "http://example.com/large", nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}

	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if len(body) != len(largeBody) {
		t.Errorf("body length = %d, want %d", len(body), len(largeBody))
	}

	if got := atomic.LoadInt64(&transport.bytesFrom); got != int64(len(largeBody)) {
		t.Errorf("bytesFrom = %d, want %d", got, len(largeBody))
	}
}
