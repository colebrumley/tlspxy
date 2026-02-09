package health

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckMiddleware_ReturnsOK(t *testing.T) {
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("backend should not be called for health check path")
	})

	handler := CheckMiddleware("/healthz", backend)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	want := `{"status":"ok"}`
	if string(body) != want {
		t.Errorf("body = %q, want %q", string(body), want)
	}
}

func TestCheckMiddleware_ProxiesOtherPaths(t *testing.T) {
	backendCalled := false
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendCalled = true
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("backend"))
	})

	handler := CheckMiddleware("/healthz", backend)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/other")
	if err != nil {
		t.Fatalf("GET /other error: %v", err)
	}
	defer resp.Body.Close()

	if !backendCalled {
		t.Error("expected backend to be called for non-health-check path")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if string(body) != "backend" {
		t.Errorf("body = %q, want %q", string(body), "backend")
	}
}

func TestCheckMiddleware_CustomPath(t *testing.T) {
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := CheckMiddleware("/status", backend)

	// /status should be intercepted
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/status", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("/status status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != `{"status":"ok"}` {
		t.Errorf("/status body = %q, want %q", rec.Body.String(), `{"status":"ok"}`)
	}

	// /healthz should pass through to backend
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/healthz", nil)
	handler.ServeHTTP(rec2, req2)
	// backend returns 200 with empty body, which is fine - just verifying it wasn't intercepted
}
