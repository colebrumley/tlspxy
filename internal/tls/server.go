package tls

import (
	cryptotls "crypto/tls"
	"fmt"
	"log/slog"
	"net"

	"golang.org/x/crypto/acme/autocert"

	"github.com/knadh/koanf/v2"
)

// ConfigServer wraps the inner listener with TLS if configured.
// If TLS configuration fails, it logs the error and returns the
// original listener without TLS.
func ConfigServer(inner net.Listener, k *koanf.Koanf) net.Listener {
	tlsConf, err := GetServerConfig(k)
	if err != nil {
		slog.Error(fmt.Sprintf("Failed to load server TLS config: %s", err.Error()))
		slog.Info("Proceeding with non-TLS server")
		return inner
	}

	if tlsConf != nil {
		return cryptotls.NewListener(inner, tlsConf)
	}
	return inner
}

// GetServerConfig reads server TLS configuration from koanf and returns a *tls.Config.
// It returns a non-nil error if the requested TLS certificate files cannot be loaded.
func GetServerConfig(k *koanf.Koanf) (*cryptotls.Config, error) {
	var (
		tlsConf *cryptotls.Config
		err     error
	)
	// Load server TLS config from cert files
	cert := k.String("server.tls.cert")
	key := k.String("server.tls.key")
	ca := k.String("server.tls.ca")
	useLetsencrypt := k.Bool("server.tls.letsencrypt.enable")

	// Check for whether server.tls.letsencrypt.enable is true,
	// and load a LetsEncrypt cert if so.
	if useLetsencrypt {
		slog.Debug("Enabling LetsEncrypt on Server connection")
		m := autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(k.String("server.tls.letsencrypt.domain")),
			Email:      k.String("server.tls.letsencrypt.email"),
			Cache:      autocert.DirCache(k.String("server.tls.letsencrypt.cachedir")),
		}
		tlsConf = &cryptotls.Config{GetCertificate: m.GetCertificate, MinVersion: cryptotls.VersionTLS12}
		// See if a cert or key was specified, load a TLS config from it if so
	} else if len(cert) > 0 || len(key) > 0 {
		if tlsConf, err = LoadConfigFromFiles(cert, key, ca, false); err != nil {
			return nil, fmt.Errorf("failed to load requested TLS config: %w", err)
		}
		slog.Debug("Loaded Server TLS config", "cert", cert, "key", key, "ca", ca)
		// Otherwise don't load a TLS config
	} else {
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

	return tlsConf, nil
}
