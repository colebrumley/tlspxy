package loadtest

import (
	"context"
	"fmt"
	"time"
)

// Scenario defines a load test configuration including proxy setup, traffic
// generation parameters, and optional post-test validation.
type Scenario struct {
	Name           string
	Description    string
	Mode           string // "tcp" or "http"
	Harness        HarnessConfig
	Concurrency    int
	Duration       time.Duration
	PayloadSize    int
	ReloadInterval time.Duration        // if > 0, trigger cert reload at this interval
	Validate       func(*Result) error  // optional post-test assertion
}

// DefaultScenarios contains the built-in load test scenarios.
var DefaultScenarios = []Scenario{
	{
		Name:        "tcp-plaintext",
		Description: "TCP proxy with no TLS",
		Mode:        "tcp",
		Harness:     HarnessConfig{Mode: "tcp"},
		Concurrency: 50,
		Duration:    10 * time.Second,
		PayloadSize: 1024,
	},
	{
		Name:        "tcp-tls",
		Description: "TCP proxy with server-side TLS",
		Mode:        "tcp",
		Harness:     HarnessConfig{Mode: "tcp", ServerTLS: true},
		Concurrency: 50,
		Duration:    10 * time.Second,
		PayloadSize: 1024,
	},
	{
		Name:        "tcp-tls-both",
		Description: "TCP proxy with TLS on both sides",
		Mode:        "tcp",
		Harness:     HarnessConfig{Mode: "tcp", ServerTLS: true, RemoteTLS: true},
		Concurrency: 50,
		Duration:    10 * time.Second,
		PayloadSize: 1024,
	},
	{
		Name:        "tcp-mtls",
		Description: "TCP proxy with mutual TLS",
		Mode:        "tcp",
		Harness:     HarnessConfig{Mode: "tcp", ServerTLS: true, MutualTLS: true},
		Concurrency: 50,
		Duration:    10 * time.Second,
		PayloadSize: 1024,
	},
	{
		Name:        "tcp-maxconns",
		Description: "TCP proxy with connection limit (50) under high load (200 workers)",
		Mode:        "tcp",
		Harness:     HarnessConfig{Mode: "tcp", ServerTLS: true, MaxConns: 50},
		Concurrency: 200,
		Duration:    10 * time.Second,
		PayloadSize: 1024,
		Validate: func(r *Result) error {
			// With 200 workers and 50 max connections, many connections
			// will be rejected. Allow up to 75% error rate.
			if r.ErrorRate > 0.75 {
				return fmt.Errorf("error rate %.2f%% exceeds 75%% threshold", r.ErrorRate*100)
			}
			return nil
		},
	},
	{
		Name:        "tcp-large-payload",
		Description: "TCP proxy with 1MB payload",
		Mode:        "tcp",
		Harness:     HarnessConfig{Mode: "tcp", ServerTLS: true},
		Concurrency: 20,
		Duration:    10 * time.Second,
		PayloadSize: 1048576,
	},
	{
		Name:        "http-tls",
		Description: "HTTP reverse proxy with TLS",
		Mode:        "http",
		Harness:     HarnessConfig{Mode: "http", ServerTLS: true},
		Concurrency: 50,
		Duration:    10 * time.Second,
		PayloadSize: 0,
	},
	{
		Name:        "http-large-response",
		Description: "HTTP reverse proxy with 1MB response",
		Mode:        "http",
		Harness:     HarnessConfig{Mode: "http", ServerTLS: true, ResponseSize: 1048576},
		Concurrency: 20,
		Duration:    10 * time.Second,
		PayloadSize: 0,
	},
	{
		Name:           "cert-reload-under-load",
		Description:    "TCP proxy with cert reload every 500ms under load",
		Mode:           "tcp",
		Harness:        HarnessConfig{Mode: "tcp", ServerTLS: true},
		Concurrency:    50,
		Duration:       10 * time.Second,
		PayloadSize:    1024,
		ReloadInterval: 500 * time.Millisecond,
	},
}

// RunScenario executes a single load test scenario and returns the result.
func RunScenario(s Scenario, certDir string) (*Result, error) {
	s.Harness.CertDir = certDir

	h, err := NewHarness(s.Harness)
	if err != nil {
		return nil, fmt.Errorf("creating harness: %w", err)
	}
	defer h.Close()

	stats := &Stats{}
	stats.Start()

	var gen interface {
		Run(ctx context.Context, concurrency int)
	}

	switch s.Mode {
	case "tcp":
		gen = &TCPGenerator{
			Addr:        h.ProxyAddr,
			TLSConf:     h.ClientTLSConf,
			PayloadSize: s.PayloadSize,
			Stats:       stats,
		}
	case "http":
		scheme := "http"
		if s.Harness.ServerTLS {
			scheme = "https"
		}
		gen = &HTTPGenerator{
			URL:         scheme + "://" + h.ProxyAddr + "/",
			TLSConf:     h.ClientTLSConf,
			PayloadSize: s.PayloadSize,
			Stats:       stats,
		}
	default:
		return nil, fmt.Errorf("unsupported mode: %q", s.Mode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.Duration)
	defer cancel()

	if s.ReloadInterval > 0 {
		go func() {
			ticker := time.NewTicker(s.ReloadInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					_ = h.TriggerReload()
				}
			}
		}()
	}

	gen.Run(ctx, s.Concurrency)

	result := stats.Snapshot(s.Name, s.Concurrency)

	if s.Validate != nil {
		if err := s.Validate(&result); err != nil {
			return &result, err
		}
	}

	return &result, nil
}

// ListScenarios returns a copy of the default scenario list.
func ListScenarios() []Scenario {
	out := make([]Scenario, len(DefaultScenarios))
	copy(out, DefaultScenarios)
	return out
}
