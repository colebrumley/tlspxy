package metrics

import (
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func TestMetrics_Registration(t *testing.T) {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		ConnectionsActive,
		ConnectionsTotal,
		BytesSentTotal,
		BytesReceivedTotal,
		ErrorsTotal,
	)

	// Initialize a label so the CounterVec produces output
	ErrorsTotal.WithLabelValues("test").Add(0)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	expected := map[string]bool{
		"tlspxy_connections_active":   false,
		"tlspxy_connections_total":    false,
		"tlspxy_bytes_sent_total":     false,
		"tlspxy_bytes_received_total": false,
		"tlspxy_errors_total":         false,
	}
	for _, mf := range families {
		if _, ok := expected[mf.GetName()]; ok {
			expected[mf.GetName()] = true
		}
	}
	for name, found := range expected {
		if !found {
			t.Errorf("Metric %q was not gathered", name)
		}
	}
}

func TestMetrics_HTTPEndpoint(t *testing.T) {
	// Use a dedicated registry and handler for this test
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		ConnectionsActive,
		ConnectionsTotal,
		BytesSentTotal,
		BytesReceivedTotal,
		ErrorsTotal,
	)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	addr := ln.Addr().String()

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	// Wait for server to be ready
	var resp *http.Response
	for i := 0; i < 20; i++ {
		resp, err = http.Get("http://" + addr + "/metrics")
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("Failed to reach metrics endpoint: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	bodyStr := string(body)
	for _, name := range []string{
		"tlspxy_connections_active",
		"tlspxy_connections_total",
		"tlspxy_bytes_sent_total",
		"tlspxy_bytes_received_total",
	} {
		if !strings.Contains(bodyStr, name) {
			t.Errorf("Metric %q not found in /metrics response", name)
		}
	}
}

func TestMetrics_IncrementDecrement(t *testing.T) {
	Enabled.Store(true)
	defer Enabled.Store(false)

	ConnectionsActive.Inc()
	ConnectionsActive.Inc()
	ConnectionsActive.Dec()

	ConnectionsTotal.Inc()
	BytesSentTotal.Add(1024)
	BytesReceivedTotal.Add(2048)
	ErrorsTotal.WithLabelValues("connection").Inc()
	ErrorsTotal.WithLabelValues("http").Inc()
}
