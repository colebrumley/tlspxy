package tls

import (
	cryptotls "crypto/tls"
	"fmt"
	"log/slog"
	"sync"

	"github.com/knadh/koanf/v2"
)

// SNICertEntry represents an SNI-based certificate mapping.
type SNICertEntry struct {
	Hostname string
	CertPath string
	KeyPath  string
}

// CertStore manages TLS certificates with support for hot-reload and SNI.
type CertStore struct {
	mu          sync.RWMutex
	defaultCert *cryptotls.Certificate
	certs       map[string]*cryptotls.Certificate
	certPath    string
	keyPath     string
	sniEntries  []SNICertEntry
}

// NewCertStore creates a CertStore and loads the default certificate from disk.
func NewCertStore(certPath, keyPath string) (*CertStore, error) {
	cert, err := cryptotls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("loading certificate: %w", err)
	}
	return &CertStore{
		defaultCert: &cert,
		certs:       make(map[string]*cryptotls.Certificate),
		certPath:    certPath,
		keyPath:     keyPath,
	}, nil
}

// GetCertificate implements the tls.Config.GetCertificate callback.
// It checks the SNI map first, then falls back to the default certificate.
func (cs *CertStore) GetCertificate(hello *cryptotls.ClientHelloInfo) (*cryptotls.Certificate, error) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	if hello != nil && hello.ServerName != "" {
		if cert, ok := cs.certs[hello.ServerName]; ok {
			return cert, nil
		}
	}
	return cs.defaultCert, nil
}

// AddSNICert loads a certificate for a specific hostname.
func (cs *CertStore) AddSNICert(hostname, certPath, keyPath string) error {
	cert, err := cryptotls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return fmt.Errorf("loading SNI cert for %s: %w", hostname, err)
	}
	cs.mu.Lock()
	cs.certs[hostname] = &cert
	cs.sniEntries = append(cs.sniEntries, SNICertEntry{
		Hostname: hostname,
		CertPath: certPath,
		KeyPath:  keyPath,
	})
	cs.mu.Unlock()
	slog.Debug("Added SNI certificate", "hostname", hostname)
	return nil
}

// Reload re-reads the default certificate from disk.
// If loading fails, the old certificate is preserved.
func (cs *CertStore) Reload() error {
	cert, err := cryptotls.LoadX509KeyPair(cs.certPath, cs.keyPath)
	if err != nil {
		return fmt.Errorf("reloading certificate: %w", err)
	}
	cs.mu.Lock()
	cs.defaultCert = &cert
	cs.mu.Unlock()
	return nil
}

// ReloadAll reloads the default certificate and all SNI certificates.
// If any individual reload fails, it continues with the rest and returns the first error.
func (cs *CertStore) ReloadAll() error {
	var firstErr error

	if err := cs.Reload(); err != nil {
		firstErr = err
		slog.Error("Failed to reload default certificate", "error", err)
	}

	cs.mu.RLock()
	entries := make([]SNICertEntry, len(cs.sniEntries))
	copy(entries, cs.sniEntries)
	cs.mu.RUnlock()

	for _, entry := range entries {
		cert, err := cryptotls.LoadX509KeyPair(entry.CertPath, entry.KeyPath)
		if err != nil {
			slog.Error("Failed to reload SNI certificate", "hostname", entry.Hostname, "error", err)
			if firstErr == nil {
				firstErr = fmt.Errorf("reloading SNI cert for %s: %w", entry.Hostname, err)
			}
			continue
		}
		cs.mu.Lock()
		cs.certs[entry.Hostname] = &cert
		cs.mu.Unlock()
	}

	return firstErr
}

// ParseSNIConfig reads SNI certificate entries from koanf configuration.
func ParseSNIConfig(k *koanf.Koanf) ([]SNICertEntry, error) {
	raw := k.Get("server.tls.sni")
	if raw == nil {
		return nil, nil
	}

	items, ok := raw.([]interface{})
	if !ok {
		return nil, nil
	}

	var entries []SNICertEntry
	for i, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("server.tls.sni[%d]: expected map, got %T", i, item)
		}
		hostname, _ := m["hostname"].(string)
		certPath, _ := m["cert"].(string)
		keyPath, _ := m["key"].(string)

		if hostname == "" {
			return nil, fmt.Errorf("server.tls.sni[%d]: hostname is required", i)
		}
		if certPath == "" {
			return nil, fmt.Errorf("server.tls.sni[%d]: cert is required", i)
		}
		if keyPath == "" {
			return nil, fmt.Errorf("server.tls.sni[%d]: key is required", i)
		}

		entries = append(entries, SNICertEntry{
			Hostname: hostname,
			CertPath: certPath,
			KeyPath:  keyPath,
		})
	}
	return entries, nil
}
