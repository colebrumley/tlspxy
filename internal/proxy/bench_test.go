package proxy_test

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/colebrumley/tlspxy/internal/loadtest"
)

// benchmarkRoot finds the project root by looking for go.mod (for *testing.B).
func benchmarkRoot(b *testing.B) string {
	b.Helper()
	dir, err := os.Getwd()
	if err != nil {
		b.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			b.Fatal("could not find project root")
		}
		dir = parent
	}
}

func BenchmarkTCP_Plaintext(b *testing.B) {
	certDir := filepath.Join(benchmarkRoot(b), "contrib", "testdata", "certs")
	harness, err := loadtest.NewHarness(loadtest.HarnessConfig{
		Mode:    "tcp",
		CertDir: certDir,
	})
	if err != nil {
		b.Fatalf("NewHarness: %v", err)
	}

	payload := make([]byte, 1024)
	for i := range payload {
		payload[i] = 'A'
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn, err := net.Dial("tcp", harness.ProxyAddr)
		if err != nil {
			b.Fatalf("Dial: %v", err)
		}
		if _, err := conn.Write(payload); err != nil {
			conn.Close()
			b.Fatalf("Write: %v", err)
		}
		buf := make([]byte, len(payload))
		if _, err := io.ReadFull(conn, buf); err != nil {
			conn.Close()
			b.Fatalf("ReadFull: %v", err)
		}
		conn.Close()
	}
	b.StopTimer()
	harness.Close()
}

func BenchmarkTCP_TLS(b *testing.B) {
	certDir := filepath.Join(benchmarkRoot(b), "contrib", "testdata", "certs")
	harness, err := loadtest.NewHarness(loadtest.HarnessConfig{
		Mode:      "tcp",
		ServerTLS: true,
		CertDir:   certDir,
	})
	if err != nil {
		b.Fatalf("NewHarness: %v", err)
	}

	payload := make([]byte, 1024)
	for i := range payload {
		payload[i] = 'A'
	}

	dialer := &net.Dialer{Timeout: 5 * time.Second}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn, err := tls.DialWithDialer(dialer, "tcp", harness.ProxyAddr, harness.ClientTLSConf)
		if err != nil {
			b.Fatalf("TLS Dial: %v", err)
		}
		if _, err := conn.Write(payload); err != nil {
			conn.Close()
			b.Fatalf("Write: %v", err)
		}
		buf := make([]byte, len(payload))
		if _, err := io.ReadFull(conn, buf); err != nil {
			conn.Close()
			b.Fatalf("ReadFull: %v", err)
		}
		conn.Close()
	}
	b.StopTimer()
	harness.Close()
}

func BenchmarkHTTP_TLS(b *testing.B) {
	certDir := filepath.Join(benchmarkRoot(b), "contrib", "testdata", "certs")
	harness, err := loadtest.NewHarness(loadtest.HarnessConfig{
		Mode:      "http",
		ServerTLS: true,
		CertDir:   certDir,
	})
	if err != nil {
		b.Fatalf("NewHarness: %v", err)
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: harness.ClientTLSConf,
		},
	}

	url := fmt.Sprintf("https://%s/", harness.ProxyAddr)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := client.Get(url)
		if err != nil {
			b.Fatalf("HTTP GET: %v", err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	b.StopTimer()
	harness.Close()
}
