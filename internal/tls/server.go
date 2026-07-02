package tls

import (
	"crypto/rand"
	cryptotls "crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net"
	"os"

	"golang.org/x/crypto/acme/autocert"

	"github.com/knadh/koanf/v2"
)

// ConfigServer wraps the inner listener with TLS if configured.
// Returns the (possibly wrapped) listener and a CertStore if cert-based
// TLS is configured (nil for Let's Encrypt or no TLS). A TLS configuration
// that was requested but fails to load is returned as an error rather than
// silently falling back to a plaintext listener.
func ConfigServer(inner net.Listener, k *koanf.Koanf) (net.Listener, *CertStore, error) {
	tlsConf, store, err := GetServerConfig(k)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load server TLS config: %w", err)
	}

	if tlsConf != nil {
		return cryptotls.NewListener(inner, tlsConf), store, nil
	}
	return inner, nil, nil
}

// GetServerConfig reads server TLS configuration from koanf and returns a *tls.Config
// and optionally a CertStore for hot-reload support.
// CertStore is nil for Let's Encrypt (which manages its own certs) and when no TLS is configured.
func GetServerConfig(k *koanf.Koanf) (*cryptotls.Config, *CertStore, error) {
	var (
		tlsConf *cryptotls.Config
	)
	// Load server TLS config from cert files
	cert := k.String("server.tls.cert")
	key := k.String("server.tls.key")
	ca := k.String("server.tls.ca")
	useLetsencrypt := k.Bool("server.tls.letsencrypt.enable")

	opts, err := ParseTLSOptions(k, "server.tls")
	if err != nil {
		return nil, nil, fmt.Errorf("parsing server TLS options: %w", err)
	}

	var store *CertStore

	// Check for whether server.tls.letsencrypt.enable is true,
	// and load a LetsEncrypt cert if so.
	switch {
	case useLetsencrypt:
		slog.Debug("Enabling LetsEncrypt on Server connection")
		m := autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(k.String("server.tls.letsencrypt.domain")),
			Email:      k.String("server.tls.letsencrypt.email"),
			Cache:      autocert.DirCache(k.String("server.tls.letsencrypt.cachedir")),
		}
		minV := opts.MinVersion
		if minV == 0 {
			minV = cryptotls.VersionTLS12
		}
		tlsConf = &cryptotls.Config{GetCertificate: m.GetCertificate, MinVersion: minV}
		if opts.MaxVersion != 0 {
			tlsConf.MaxVersion = opts.MaxVersion
		}
		if opts.CipherSuites != nil {
			tlsConf.CipherSuites = opts.CipherSuites
		}
		if opts.NextProtos != nil {
			tlsConf.NextProtos = opts.NextProtos
		}
	case len(cert) > 0 || len(key) > 0:
		// Create a CertStore for hot-reload support
		store, err = NewCertStore(cert, key)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to load requested TLS config: %w", err)
		}

		// Parse and load SNI certificates
		sniEntries, sniErr := ParseSNIConfig(k)
		if sniErr != nil {
			return nil, nil, fmt.Errorf("failed to parse SNI config: %w", sniErr)
		}
		for _, e := range sniEntries {
			if addErr := store.AddSNICert(e.Hostname, e.CertPath, e.KeyPath); addErr != nil {
				return nil, nil, fmt.Errorf("failed to load SNI cert: %w", addErr)
			}
		}

		minV := opts.MinVersion
		if minV == 0 {
			minV = cryptotls.VersionTLS12
		}

		tlsConf = &cryptotls.Config{
			GetCertificate: store.GetCertificate,
			Rand:           rand.Reader,
			MinVersion:     minV,
		}
		if opts.MaxVersion != 0 {
			tlsConf.MaxVersion = opts.MaxVersion
		}
		if opts.CipherSuites != nil {
			tlsConf.CipherSuites = opts.CipherSuites
		}
		if opts.NextProtos != nil {
			tlsConf.NextProtos = opts.NextProtos
		}

		// Load CA pool for client cert verification if needed
		if ca != "" {
			caPool, caErr := loadCAPool(ca, false)
			if caErr != nil {
				return nil, nil, fmt.Errorf("failed to load CA: %w", caErr)
			}
			tlsConf.ClientCAs = caPool
			tlsConf.RootCAs = caPool
		}

		slog.Debug("Loaded Server TLS config", "cert", cert, "key", key, "ca", ca)
	default:
		slog.Warn("No server TLS config loaded")
		slog.Info("Proceeding with non-TLS server")
	}

	// Parse the other TLS options.
	//   'verify' overrides 'require'
	if v := k.Bool("server.tls.require"); v && tlsConf != nil {
		slog.Debug("Setting server.tls.require", "value", v)
		tlsConf.ClientAuth = cryptotls.RequireAnyClientCert
	}
	if v := k.Bool("server.tls.verify"); v && tlsConf != nil {
		slog.Debug("Setting server.tls.verify", "value", v)
		tlsConf.ClientAuth = cryptotls.RequireAndVerifyClientCert
	}

	return tlsConf, store, nil
}

// loadCAPool creates a CertPool from a CA file, optionally including system roots.
func loadCAPool(ca string, loadSystemRoots bool) (*x509.CertPool, error) {
	var caPool *x509.CertPool

	if loadSystemRoots {
		var err error
		caPool, err = SystemCAPool()
		if err != nil {
			return nil, err
		}
	} else {
		caPool = x509.NewCertPool()
	}

	if ca != "" {
		caPem, err := os.ReadFile(ca)
		if err != nil {
			return nil, err
		}
		if !caPool.AppendCertsFromPEM(caPem) {
			return nil, fmt.Errorf("failed to load CA file: %s", ca)
		}
	}

	return caPool, nil
}
