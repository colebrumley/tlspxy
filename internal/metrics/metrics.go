package metrics

import (
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// ConnectionsActive tracks the number of currently active proxy connections.
	ConnectionsActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "tlspxy_connections_active",
		Help: "Number of currently active proxy connections.",
	})
	// ConnectionsTotal tracks the total number of proxy connections accepted.
	ConnectionsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "tlspxy_connections_total",
		Help: "Total number of proxy connections accepted.",
	})
	// BytesSentTotal tracks the total bytes sent to the backend.
	BytesSentTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "tlspxy_bytes_sent_total",
		Help: "Total bytes sent to the backend.",
	})
	// BytesReceivedTotal tracks the total bytes received from the backend.
	BytesReceivedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "tlspxy_bytes_received_total",
		Help: "Total bytes received from the backend.",
	})
	// ErrorsTotal tracks the total number of proxy errors by type.
	ErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "tlspxy_errors_total",
		Help: "Total number of proxy errors by type.",
	}, []string{"type"})
)

// Enabled tracks whether metrics instrumentation is active.
var Enabled atomic.Bool

var initOnce sync.Once

// Init registers all metrics with the default prometheus registerer.
// It is safe to call multiple times; registrations happen only once.
func Init() {
	initOnce.Do(func() {
		prometheus.MustRegister(
			ConnectionsActive,
			ConnectionsTotal,
			BytesSentTotal,
			BytesReceivedTotal,
			ErrorsTotal,
		)
		Enabled.Store(true)
	})
}

// StartServer starts the metrics HTTP server. It binds the listener
// synchronously so bind failures (e.g. port already in use) are reported to
// the caller immediately, then serves in a background goroutine. The returned
// *http.Server can be used to perform a graceful shutdown.
func StartServer(addr, path string) (*http.Server, error) {
	mux := http.NewServeMux()
	mux.Handle(path, promhttp.Handler())

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	srv := &http.Server{Handler: mux}
	go func() {
		slog.Info("Starting metrics server", "addr", addr, "path", path)
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("Metrics server error", "error", err)
		}
	}()
	return srv, nil
}
