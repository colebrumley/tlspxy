package proxy

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
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
		req.Body.Close()
	}
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

func TestRoundTrip_PreservesBody(t *testing.T) {
	originalBody := "POST body data that must be preserved"

	mock := &mockRoundTripper{
		response: &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("response")),
		},
	}

	transport := &Transport{
		RoundTripper: mock,
	}

	req, _ := http.NewRequest("POST", "http://example.com/api", strings.NewReader(originalBody))

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	defer resp.Body.Close()

	if string(mock.receivedBody) != originalBody {
		t.Errorf("Mock received body = %q, want %q", string(mock.receivedBody), originalBody)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}
	if string(respBody) != "response" {
		t.Errorf("response body = %q, want %q", string(respBody), "response")
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

	transport := &Transport{
		RoundTripper: mock,
	}

	req, _ := http.NewRequest("GET", "http://example.com/", nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	defer resp.Body.Close()

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

	transport := &Transport{
		ShowContent:  true,
		RoundTripper: mock,
	}

	req, _ := http.NewRequest("POST", "http://example.com/data", strings.NewReader(requestBody))
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}

	// Bytes are counted as the body streams through, on Close.
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatalf("reading response body: %v", err)
	}
	resp.Body.Close()

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
	responseBody := "streaming response content that is somewhat large"

	mock := &mockRoundTripper{
		response: &http.Response{
			StatusCode: 200,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(responseBody)),
		},
	}

	transport := &Transport{
		ShowContent:  false,
		RoundTripper: mock,
	}

	req, _ := http.NewRequest("GET", "http://example.com/stream", nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}

	if got := atomic.LoadInt64(&transport.bytesFrom); got != 0 {
		t.Errorf("bytesFrom before read = %d, want 0", got)
	}

	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if string(body) != responseBody {
		t.Errorf("body = %q, want %q", string(body), responseBody)
	}

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
	largeBody := strings.Repeat("x", 1024*1024)

	mock := &mockRoundTripper{
		response: &http.Response{
			StatusCode: 200,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(largeBody)),
		},
	}

	transport := &Transport{
		ShowContent:  false,
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

func TestRoundTrip_ErrorFromUpstream(t *testing.T) {
	mock := &mockRoundTripper{
		err: fmt.Errorf("connection refused"),
	}

	transport := &Transport{
		RoundTripper: mock,
	}

	req, _ := http.NewRequest("GET", "http://example.com/fail", nil)
	resp, err := transport.RoundTrip(req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if resp != nil {
		t.Errorf("expected nil response, got %+v", resp)
	}
	if err.Error() != "connection refused" {
		t.Errorf("error = %q, want %q", err.Error(), "connection refused")
	}
}

func TestRoundTrip_ShowContentTrue_ResponseReadable(t *testing.T) {
	responseBody := "full response that gets read and re-buffered"

	mock := &mockRoundTripper{
		response: &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader(responseBody)),
		},
	}

	transport := &Transport{
		ShowContent:  true,
		RoundTripper: mock,
	}

	req, _ := http.NewRequest("GET", "http://example.com/content", nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}
	resp.Body.Close()
	if string(body) != responseBody {
		t.Errorf("response body = %q, want %q", string(body), responseBody)
	}

	if got := atomic.LoadInt64(&transport.bytesFrom); got != int64(len(responseBody)) {
		t.Errorf("bytesFrom = %d, want %d", got, len(responseBody))
	}
}

// TestRoundTrip_ShowContentLargeBody verifies that with ShowContent enabled a
// response larger than maxLoggedBytes is still forwarded to the client in FULL
// (streaming, not just the logged prefix), and byte accounting counts the whole
// body rather than only the bounded prefix.
func TestRoundTrip_ShowContentLargeBody(t *testing.T) {
	largeBody := strings.Repeat("z", maxLoggedBytes*3+123)

	mock := &mockRoundTripper{
		response: &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/octet-stream"}},
			Body:       io.NopCloser(strings.NewReader(largeBody)),
		},
	}

	transport := &Transport{
		ShowContent:  true,
		RoundTripper: mock,
	}

	req, _ := http.NewRequest("GET", "http://example.com/large", nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}
	resp.Body.Close()

	if len(body) != len(largeBody) {
		t.Fatalf("client received %d bytes, want full body of %d", len(body), len(largeBody))
	}
	if string(body) != largeBody {
		t.Error("client did not receive the full body content unchanged")
	}

	if got := atomic.LoadInt64(&transport.bytesFrom); got != int64(len(largeBody)) {
		t.Errorf("bytesFrom = %d, want full body %d", got, len(largeBody))
	}
}

// threadSafeRoundTripper is safe for concurrent use from multiple goroutines.
type threadSafeRoundTripper struct {
	mu           sync.Mutex
	responseBody string
}

func (m *threadSafeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		io.ReadAll(req.Body)
		req.Body.Close()
	}
	m.mu.Lock()
	body := m.responseBody
	m.mu.Unlock()
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

func TestRoundTrip_Concurrent(t *testing.T) {
	const goroutines = 50
	const reqBodySize = 100
	const respBodySize = 200

	transport := &Transport{
		ShowContent: true,
		RoundTripper: &threadSafeRoundTripper{
			responseBody: strings.Repeat("r", respBodySize),
		},
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			body := strings.NewReader(strings.Repeat("q", reqBodySize))
			req, _ := http.NewRequest("POST", "http://example.com/concurrent", body)
			resp, err := transport.RoundTrip(req)
			if err != nil {
				t.Errorf("RoundTrip error: %v", err)
				return
			}
			io.ReadAll(resp.Body)
			resp.Body.Close()
		}()
	}
	wg.Wait()

	wantTo := int64(goroutines * reqBodySize)
	wantFrom := int64(goroutines * respBodySize)

	if got := atomic.LoadInt64(&transport.bytesTo); got != wantTo {
		t.Errorf("bytesTo = %d, want %d", got, wantTo)
	}
	if got := atomic.LoadInt64(&transport.bytesFrom); got != wantFrom {
		t.Errorf("bytesFrom = %d, want %d", got, wantFrom)
	}
}

// makeDirector creates a director function matching main.go's logic for testing.
func makeDirector(target *url.URL, trustXFF bool) func(*http.Request) {
	return func(req *http.Request) {
		req.Host = target.Host
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = SingleJoiningSlash(target.Path, req.URL.Path)
		if clientIP, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
			req.Header.Set("X-Real-IP", clientIP)
			if trustXFF {
				if prior := req.Header.Get("X-Forwarded-For"); prior != "" {
					clientIP = prior + ", " + clientIP
				}
			}
			req.Header.Set("X-Forwarded-For", clientIP)
		}
		if target.RawQuery == "" || req.URL.RawQuery == "" {
			req.URL.RawQuery = target.RawQuery + req.URL.RawQuery
		} else {
			req.URL.RawQuery = target.RawQuery + "&" + req.URL.RawQuery
		}
		if _, ok := req.Header["User-Agent"]; !ok {
			req.Header.Set("User-Agent", "")
		}
	}
}

func TestHTTPDirector_XForwardedFor(t *testing.T) {
	target, _ := url.Parse("http://backend.local:9090/base")

	director := makeDirector(target, false)

	t.Run("sets X-Forwarded-For and X-Real-IP", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "http://proxy.local/hello", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		director(req)

		if got := req.Header.Get("X-Forwarded-For"); got != "192.168.1.1" {
			t.Errorf("X-Forwarded-For = %q, want %q", got, "192.168.1.1")
		}
		if got := req.Header.Get("X-Real-IP"); got != "192.168.1.1" {
			t.Errorf("X-Real-IP = %q, want %q", got, "192.168.1.1")
		}
	})

	t.Run("resets existing X-Forwarded-For by default", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "http://proxy.local/hello", nil)
		req.RemoteAddr = "10.0.0.2:54321"
		req.Header.Set("X-Forwarded-For", "203.0.113.50")
		director(req)

		want := "10.0.0.2"
		if got := req.Header.Get("X-Forwarded-For"); got != want {
			t.Errorf("X-Forwarded-For = %q, want %q", got, want)
		}
		if got := req.Header.Get("X-Real-IP"); got != "10.0.0.2" {
			t.Errorf("X-Real-IP = %q, want %q", got, "10.0.0.2")
		}
	})

	t.Run("appends to existing X-Forwarded-For when trusted", func(t *testing.T) {
		d := makeDirector(target, true)
		req, _ := http.NewRequest("GET", "http://proxy.local/hello", nil)
		req.RemoteAddr = "10.0.0.2:54321"
		req.Header.Set("X-Forwarded-For", "203.0.113.50")
		d(req)

		want := "203.0.113.50, 10.0.0.2"
		if got := req.Header.Get("X-Forwarded-For"); got != want {
			t.Errorf("X-Forwarded-For = %q, want %q", got, want)
		}
	})

	t.Run("rewrites URL", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "http://proxy.local/api/resource", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		director(req)

		if req.URL.Scheme != "http" {
			t.Errorf("scheme = %q, want %q", req.URL.Scheme, "http")
		}
		if req.URL.Host != "backend.local:9090" {
			t.Errorf("host = %q, want %q", req.URL.Host, "backend.local:9090")
		}
		want := "/base/api/resource"
		if req.URL.Path != want {
			t.Errorf("path = %q, want %q", req.URL.Path, want)
		}
	})

	t.Run("sets empty User-Agent when absent", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "http://proxy.local/", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		delete(req.Header, "User-Agent")
		director(req)

		if got := req.Header.Get("User-Agent"); got != "" {
			t.Errorf("User-Agent = %q, want empty", got)
		}
	})

	t.Run("query string merging", func(t *testing.T) {
		targetWithQuery, _ := url.Parse("http://backend.local:9090/base?key=val")
		d := makeDirector(targetWithQuery, false)

		req, _ := http.NewRequest("GET", "http://proxy.local/path?foo=bar", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		d(req)

		if req.URL.RawQuery != "key=val&foo=bar" {
			t.Errorf("RawQuery = %q, want %q", req.URL.RawQuery, "key=val&foo=bar")
		}
	})
}

func TestHTTPReverseProxy_EndToEnd(t *testing.T) {
	var (
		mu              sync.Mutex
		capturedMethod  string
		capturedPath    string
		capturedXFF     string
		capturedXRealIP string
		capturedBody    string
	)

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		capturedXFF = r.Header.Get("X-Forwarded-For")
		capturedXRealIP = r.Header.Get("X-Real-IP")
		if r.Body != nil {
			b, _ := io.ReadAll(r.Body)
			capturedBody = string(b)
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(200)
		w.Write([]byte("backend-response"))
	}))
	defer backend.Close()

	backendURL, _ := url.Parse(backend.URL)

	proxy := &Transport{
		ShowContent:  true,
		RoundTripper: http.DefaultTransport,
	}

	rp := &httputil.ReverseProxy{
		Director:  makeDirector(backendURL, false),
		Transport: proxy,
	}

	frontend := httptest.NewServer(rp)
	defer frontend.Close()

	t.Run("GET", func(t *testing.T) {
		resp, err := http.Get(frontend.URL + "/hello")
		if err != nil {
			t.Fatalf("GET error: %v", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if string(body) != "backend-response" {
			t.Errorf("response body = %q, want %q", string(body), "backend-response")
		}

		mu.Lock()
		if capturedMethod != "GET" {
			t.Errorf("backend saw method = %q, want GET", capturedMethod)
		}
		if capturedPath != "/hello" {
			t.Errorf("backend saw path = %q, want /hello", capturedPath)
		}
		if capturedXFF == "" {
			t.Error("backend did not receive X-Forwarded-For header")
		}
		if capturedXRealIP == "" {
			t.Error("backend did not receive X-Real-IP header")
		}
		mu.Unlock()
	})

	t.Run("POST", func(t *testing.T) {
		postBody := "important POST data"
		resp, err := http.Post(frontend.URL+"/submit", "text/plain", strings.NewReader(postBody))
		if err != nil {
			t.Fatalf("POST error: %v", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if string(body) != "backend-response" {
			t.Errorf("response body = %q, want %q", string(body), "backend-response")
		}

		mu.Lock()
		if capturedMethod != "POST" {
			t.Errorf("backend saw method = %q, want POST", capturedMethod)
		}
		if capturedBody != postBody {
			t.Errorf("backend saw body = %q, want %q", capturedBody, postBody)
		}
		mu.Unlock()
	})

	if got := atomic.LoadInt64(&proxy.bytesFrom); got == 0 {
		t.Error("bytesFrom is 0 after e2e requests, expected non-zero")
	}
	if got := atomic.LoadInt64(&proxy.bytesTo); got == 0 {
		t.Error("bytesTo is 0 after e2e POST request, expected non-zero")
	}
}

func TestTransport_InterruptHandler(t *testing.T) {
	transport := &Transport{
		RoundTripper: http.DefaultTransport,
	}

	atomic.StoreInt64(&transport.bytesTo, 12345)
	atomic.StoreInt64(&transport.bytesFrom, 67890)

	transport.InterruptHandler()
}
